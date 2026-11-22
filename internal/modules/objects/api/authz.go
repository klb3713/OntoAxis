package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// Resource type names used when building authorization facts. They must match
// the resource types registered in the platform authorization schema.
const (
	resourceTypeTenant     = "tenant"
	resourceTypeObjectType = "object_type"
	resourceTypeObject     = "object"
)

// ObjectTypeFact is the tenant-scoped data the authorization layer needs about
// an object type. The caller loads it from the object types owned by the
// request tenant; request input alone must never be trusted as the tenant edge.
type ObjectTypeFact struct {
	ID       string
	TenantID string
}

// ObjectTypeReader loads an object type for the given tenant. Implementations
// must scope the lookup by tenant and return an error satisfying
// errors.Is(err, domain.ErrNotFound) when the object type does not exist for
// that tenant. The reader is required whenever a DecisionGuard is configured.
type ObjectTypeReader interface {
	GetObjectTypeFact(ctx context.Context, tenantID, objectTypeID uuid.UUID) (ObjectTypeFact, error)
}

// LinkFact is the tenant-scoped data the authorization layer needs about a
// link. SourceID and TargetID identify the two linked object instances.
type LinkFact struct {
	ID       string
	TenantID string
	SourceID string
	TargetID string
}

// LinkReader loads a link for the given tenant. Implementations must scope the
// lookup by tenant and return an error satisfying
// errors.Is(err, domain.ErrNotFound) when the link does not exist for that
// tenant. The reader is required whenever a DecisionGuard is configured.
type LinkReader interface {
	GetLinkFact(ctx context.Context, tenantID, linkID uuid.UUID) (LinkFact, error)
}

// authorize delegates one decision to the platform guard. A nil guard allows
// the request, preserving the pre-authorization behavior for callers that have
// not wired a guard yet.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, h.guard, action, resource)
}

// authorizeObjectType loads the object type tenant-scoped and authorizes the
// action against the resulting ObjectType fact. A missing (or cross-tenant)
// object type is a 404 that happens before authorization, as required by the
// authorization boundary contract.
func (h *Handler) authorizeObjectType(w http.ResponseWriter, r *http.Request, tenantID, objectTypeID uuid.UUID, action authz.Action) bool {
	if h.guard == nil {
		return true
	}
	if h.objectTypes == nil {
		writeError(w, r, httperr.Unavailable("authorization object type reader is not configured"))
		return false
	}
	fact, err := h.objectTypes.GetObjectTypeFact(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeAuthorizationLoadError(w, r, err, "object type not found")
		return false
	}
	return h.authorize(w, r, action, authz.ResourceFact{
		Type:     resourceTypeObjectType,
		ID:       fact.ID,
		TenantID: fact.TenantID,
	})
}

// authorizeTenant authorizes an action against the request tenant, used by
// tenant-level object lists that have no parent resource filter.
func (h *Handler) authorizeTenant(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, action authz.Action) bool {
	if h.guard == nil {
		return true
	}
	value := tenantID.String()
	return h.authorize(w, r, action, authz.ResourceFact{Type: resourceTypeTenant, ID: value, TenantID: value})
}

// loadObjectForAuthorization loads an object row scoped to the request tenant.
// Both a missing row and a row whose stored tenant does not match the request
// tenant are reported as 404 so that cross-tenant objects stay invisible and
// never reach the authorization layer.
func (h *Handler) loadObjectForAuthorization(w http.ResponseWriter, r *http.Request, tenantID, objectID uuid.UUID) (domain.Object, bool) {
	object, err := h.svc.GetObject(r.Context(), tenantID, objectID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return domain.Object{}, false
	}
	if object.TenantID != tenantID {
		writeError(w, r, httperr.NotFound("object not found"))
		return domain.Object{}, false
	}
	return object, true
}

// authorizeLinkEndpoints loads both link endpoints tenant-scoped, requires the
// endpoints to belong to the same tenant, and authorizes the action against the
// source object fact. Any missing or cross-tenant endpoint is a 404 before
// authorization.
func (h *Handler) authorizeLinkEndpoints(w http.ResponseWriter, r *http.Request, tenantID, sourceID, targetID uuid.UUID, action authz.Action) bool {
	if h.guard == nil {
		return true
	}
	source, ok := h.loadObjectForAuthorization(w, r, tenantID, sourceID)
	if !ok {
		return false
	}
	target, ok := h.loadObjectForAuthorization(w, r, tenantID, targetID)
	if !ok {
		return false
	}
	if source.TenantID != target.TenantID {
		writeError(w, r, httperr.NotFound("object not found"))
		return false
	}
	return h.authorize(w, r, action, objectFact(source))
}

// authorizeLink loads the link tenant-scoped and authorizes an action against
// the source object fact. The link row supplies the endpoint IDs; both endpoint
// objects are then loaded tenant-scoped. A missing link is a 404 before
// authorization.
func (h *Handler) authorizeLink(w http.ResponseWriter, r *http.Request, tenantID, linkID uuid.UUID, action authz.Action) bool {
	if h.guard == nil {
		return true
	}
	if h.links == nil {
		// v1 fallback: without a tenant-scoped link loader, authorize at
		// tenant scope (same effective access as Stage 1) and let the
		// tenant-scoped DeleteLink decide existence. A future objects change
		// can add GetLink and narrow this to the source object.
		return h.authorize(w, r, action, authz.ResourceFact{Type: "tenant", ID: tenantID.String(), TenantID: tenantID.String()})
	}
	link, err := h.links.GetLinkFact(r.Context(), tenantID, linkID)
	if err != nil {
		writeAuthorizationLoadError(w, r, err, "object link not found")
		return false
	}
	sourceID, err := uuid.Parse(link.SourceID)
	if err != nil {
		writeError(w, r, httperr.Internal())
		return false
	}
	targetID, err := uuid.Parse(link.TargetID)
	if err != nil {
		writeError(w, r, httperr.Internal())
		return false
	}
	return h.authorizeLinkEndpoints(w, r, tenantID, sourceID, targetID, action)
}

// objectFact builds the authorization fact for an object instance that was
// loaded tenant-scoped. The object type ID from the loaded row is the ancestor
// edge used by the authorization model.
func objectFact(object domain.Object) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     resourceTypeObject,
		ID:       object.ID.String(),
		TenantID: object.TenantID.String(),
		Ancestors: []authz.Ancestor{
			{Type: resourceTypeObjectType, ID: object.ObjectTypeID.String()},
		},
	}
}

// writeAuthorizationLoadError translates a tenant-scoped authorization loader
// error into an HTTP response. Missing rows stay 404; every other failure is
// fail-closed as an internal error.
func writeAuthorizationLoadError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, r, httperr.NotFound(notFoundMessage))
		return
	}
	writeError(w, r, httperr.Internal())
}
