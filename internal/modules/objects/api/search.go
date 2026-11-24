package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

// Search query limits. They mirror the platform search client and the
// object-search specification (offset window is maxTotalHits-1 = 9999).
const (
	defaultSearchLimit   = 20
	minSearchLimit       = 1
	maxSearchLimit       = 100
	maxSearchOffset      = 9999
	maxSearchQueryLength = 512
	maxSearchObjectTypes = 10
)

// searchAction is the read action checked for the object search endpoint.
const searchAction authz.Action = "object:read"

// SearchRequest is the narrow, transport-independent request the objects API
// hands to the search backend. TenantID always comes from the authenticated
// tenant context, never from a request header or query parameter.
type SearchRequest struct {
	TenantID      uuid.UUID
	Query         string
	ObjectTypeIDs []uuid.UUID
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time
	Limit         int
	Offset        int
	// Sort is a canonical sort expression: "", "updated_at:asc",
	// "updated_at:desc", "created_at:asc" or "created_at:desc".
	Sort string
}

// SearchHit is one indexed object returned by the search backend. It never
// carries tenant identifiers or property values.
type SearchHit struct {
	ID           string
	ObjectTypeID string
	DisplayName  string
	Version      int64
	UpdatedAt    time.Time
}

// SearchResponse is the backend result for one search request.
type SearchResponse struct {
	Items     []SearchHit
	Total     int64
	Limit     int
	Offset    int
	Truncated bool
}

// SearchBackend is the narrow search dependency of the objects API. The
// platform search client is adapted to it during module wiring; the API package
// deliberately does not depend on the concrete client.
type SearchBackend interface {
	Search(ctx context.Context, req SearchRequest) (SearchResponse, error)
	Healthy(ctx context.Context) error
}

// WithSearch configures the object search route. When enabled is false the
// route is not registered at all. When enabled is true a nil backend or guard
// fails closed: the route answers 503 and SearchStartupError reports the
// misconfiguration.
func WithSearch(backend SearchBackend, guard authz.DecisionGuard, enabled bool) Option {
	return func(h *Handler) {
		h.search = backend
		h.searchGuard = guard
		h.searchEnabled = enabled
	}
}

// SearchStartupError reports a fail-closed search misconfiguration. It returns
// nil when search is disabled or fully configured.
func (h *Handler) SearchStartupError() error {
	if h == nil || !h.searchEnabled {
		return nil
	}
	if h.search == nil {
		return errors.New("objects: search is enabled but no search backend is configured")
	}
	if h.searchGuard == nil {
		return errors.New("objects: search is enabled but no authorization guard is configured")
	}
	return nil
}

type searchHTTPItem struct {
	ID           string `json:"id"`
	ObjectTypeID string `json:"object_type_id"`
	DisplayName  string `json:"display_name"`
	Version      int64  `json:"version"`
	UpdatedAt    string `json:"updated_at"`
}

type searchHTTPResponse struct {
	Items     []searchHTTPItem `json:"items"`
	Total     int64            `json:"total"`
	Limit     int              `json:"limit"`
	Offset    int              `json:"offset"`
	Truncated bool             `json:"truncated"`
	RequestID string           `json:"request_id"`
}

// searchObjects serves GET /api/v1/objects/search.
func (h *Handler) searchObjects(w http.ResponseWriter, r *http.Request) {
	if h.search == nil || h.searchGuard == nil {
		writeError(w, r, httperr.Unavailable("object search is not configured"))
		return
	}

	// Tenant identity comes only from the authenticated context. The legacy
	// X-Tenant-Id fallback used by older object routes is intentionally not
	// accepted here.
	tc, err := authn.RequireTenant(r.Context())
	if err != nil || tc.TenantID == uuid.Nil {
		writeError(w, r, httperr.Unauthorized("tenant context is required"))
		return
	}

	req, message := parseSearchRequest(r)
	if message != "" {
		writeError(w, r, httperr.BadRequest(message))
		return
	}
	req.TenantID = tc.TenantID

	if len(req.ObjectTypeIDs) == 0 {
		if !authz.AuthorizeHTTP(w, r, h.searchGuard, searchAction, authz.ResourceFact{
			Type:     resourceTypeTenant,
			ID:       tc.TenantID.String(),
			TenantID: tc.TenantID.String(),
		}) {
			return
		}
	} else {
		// Resolve every scope first so a mixed-tenant filter cannot produce a
		// partial authorization result or a decision header for an allowed type.
		facts := make([]authz.ResourceFact, 0, len(req.ObjectTypeIDs))
		for _, objectTypeID := range req.ObjectTypeIDs {
			fact, ok := h.loadSearchObjectTypeFact(w, r, tc.TenantID, objectTypeID)
			if !ok {
				return
			}
			facts = append(facts, fact)
		}
		for _, fact := range facts {
			if !authz.AuthorizeHTTP(w, r, h.searchGuard, searchAction, fact) {
				return
			}
		}
	}

	response, err := h.search.Search(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, search.ErrUnavailable):
			writeSearchError(w, r, httperr.Error{Code: httperr.CodeUnavailable, Message: "object search is unavailable", Retryable: true})
		case errors.Is(err, search.ErrInvalid):
			writeSearchError(w, r, httperr.BadRequest("object search rejected the query"))
		default:
			writeSearchError(w, r, httperr.Internal())
		}
		return
	}

	items := make([]searchHTTPItem, 0, len(response.Items))
	for _, hit := range response.Items {
		items = append(items, searchHTTPItem{
			ID:           hit.ID,
			ObjectTypeID: hit.ObjectTypeID,
			DisplayName:  hit.DisplayName,
			Version:      hit.Version,
			UpdatedAt:    hit.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, searchHTTPResponse{
		Items:     items,
		Total:     response.Total,
		Limit:     req.Limit,
		Offset:    req.Offset,
		Truncated: response.Truncated,
		RequestID: searchRequestID(r),
	})
}

// loadSearchObjectTypeFact loads the trusted tenant edge for the shared guard.
// A type missing from this tenant and a cross-tenant type are intentionally
// indistinguishable to the caller.
func (h *Handler) loadSearchObjectTypeFact(w http.ResponseWriter, r *http.Request, tenantID, objectTypeID uuid.UUID) (authz.ResourceFact, bool) {
	if h.objectTypes == nil {
		authz.WriteHTTPError(w, r, &authz.Error{Reason: authz.ReasonInternalError})
		return authz.ResourceFact{}, false
	}
	fact, err := h.objectTypes.GetObjectTypeFact(r.Context(), tenantID, objectTypeID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		authz.WriteHTTPError(w, r, &authz.Error{Reason: authz.ReasonNoRelationship})
		return authz.ResourceFact{}, false
	case err != nil || fact.ID != objectTypeID.String() || fact.TenantID == "":
		authz.WriteHTTPError(w, r, &authz.Error{Reason: authz.ReasonInternalError})
		return authz.ResourceFact{}, false
	case fact.TenantID != tenantID.String():
		authz.WriteHTTPError(w, r, &authz.Error{Reason: authz.ReasonNoRelationship})
		return authz.ResourceFact{}, false
	}
	return authz.ResourceFact{
		Type:     resourceTypeObjectType,
		ID:       fact.ID,
		TenantID: fact.TenantID,
	}, true
}

// parseSearchRequest validates and normalizes the search query parameters. It
// returns a non-empty message when the request is invalid; that message is
// exposed as a 400 bad_request response.
func parseSearchRequest(r *http.Request) (SearchRequest, string) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > maxSearchQueryLength {
		return SearchRequest{}, "q must be at most 512 characters"
	}

	rawTypes := r.URL.Query()["object_type_id"]
	if len(rawTypes) > maxSearchObjectTypes {
		return SearchRequest{}, "object_type_id may be repeated at most 10 times"
	}
	objectTypeIDs := make([]uuid.UUID, 0, len(rawTypes))
	for _, raw := range rawTypes {
		objectTypeID, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || objectTypeID == uuid.Nil {
			return SearchRequest{}, "object_type_id must be a valid UUID"
		}
		objectTypeIDs = append(objectTypeIDs, objectTypeID)
	}
	if query == "" && len(objectTypeIDs) == 0 {
		return SearchRequest{}, "q or object_type_id is required"
	}

	var updatedAfter, updatedBefore *time.Time
	for _, filter := range []struct {
		name        string
		destination **time.Time
	}{
		{"updated_after", &updatedAfter},
		{"updated_before", &updatedBefore},
	} {
		raw := strings.TrimSpace(r.URL.Query().Get(filter.name))
		if raw == "" {
			continue
		}
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return SearchRequest{}, filter.name + " must be an RFC3339 timestamp"
		}
		utc := value.UTC()
		*filter.destination = &utc
	}

	limit := defaultSearchLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < minSearchLimit || value > maxSearchLimit {
			return SearchRequest{}, "limit must be an integer between 1 and 100"
		}
		limit = value
	}

	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > maxSearchOffset {
			return SearchRequest{}, "offset must be an integer between 0 and 9999"
		}
		offset = value
	}

	sort, ok := parseSearchSort(strings.TrimSpace(r.URL.Query().Get("sort")))
	if !ok {
		return SearchRequest{}, "sort must be updated_at or created_at with an optional :asc or :desc direction"
	}

	return SearchRequest{
		Query:         query,
		ObjectTypeIDs: objectTypeIDs,
		UpdatedAfter:  updatedAfter,
		UpdatedBefore: updatedBefore,
		Limit:         limit,
		Offset:        offset,
		Sort:          sort,
	}, ""
}

// parseSearchSort normalizes the documented sort grammar: the field
// (updated_at or created_at) optionally followed by ":asc"/":desc", or a
// leading "-" meaning descending. No direction means ascending. The canonical
// backend form is "<field>:<direction>".
func parseSearchSort(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	direction := "asc"
	field := raw
	if strings.HasPrefix(field, "-") {
		direction = "desc"
		field = strings.TrimPrefix(field, "-")
	} else if name, value, found := strings.Cut(field, ":"); found {
		field = name
		switch value {
		case "asc", "desc":
			direction = value
		default:
			return "", false
		}
	}
	switch field {
	case "updated_at", "created_at":
		return field + ":" + direction, true
	default:
		return "", false
	}
}

// searchRequestID returns the request id propagated through the request
// context, falling back to the incoming X-Request-ID header.
func searchRequestID(r *http.Request) string {
	if id := observability.RequestID(r.Context()); id != "" {
		return id
	}
	return strings.TrimSpace(r.Header.Get("X-Request-ID"))
}

// writeSearchError writes an error envelope enriched with the request id when
// the context does not carry one.
func writeSearchError(w http.ResponseWriter, r *http.Request, e httperr.Error) {
	if e.RequestID == "" {
		e.RequestID = searchRequestID(r)
	}
	writeError(w, r, e)
}
