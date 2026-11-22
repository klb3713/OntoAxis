package authn

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Authenticator is the platform identity contract. Implementations must be
// fail-closed: no credentials means no TenantContext.
type Authenticator interface {
	Authenticate(ctx context.Context, rawToken string) (TenantContext, error)
	Middleware(next http.Handler) http.Handler
}

// Config configures the edge authenticator.
type Config struct {
	// TrustGateway selects gateway-trust mode. When true, Middleware reads only
	// x-ontoaxis-* internal headers and rejects Authorization.
	TrustGateway bool
	Issuer       string
	Audience     string
}

// EdgeAuthenticator implements Authenticator for both direct JWT validation
// and gateway-trust deployments.
type EdgeAuthenticator struct {
	cfg      Config
	verifier Verifier
}

// NewEdgeAuthenticator constructs an EdgeAuthenticator. A nil verifier is
// accepted at construction time but every authentication attempt fails closed.
func NewEdgeAuthenticator(cfg Config, verifier Verifier) *EdgeAuthenticator {
	if cfg.Issuer == "" {
		cfg.Issuer = DefaultIssuer
	}
	if cfg.Audience == "" {
		cfg.Audience = DefaultAudience
	}
	return &EdgeAuthenticator{cfg: cfg, verifier: verifier}
}

// Authenticate validates a raw bearer token and maps its claims to a tenant
// context. It implements the direct JWT path; gateway-trust mode is handled by
// Middleware because it consumes request headers.
func (a *EdgeAuthenticator) Authenticate(ctx context.Context, rawToken string) (TenantContext, error) {
	if a == nil || a.verifier == nil {
		return TenantContext{}, fmt.Errorf("%w: verifier is not configured", ErrUnauthenticated)
	}
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return TenantContext{}, fmt.Errorf("%w: bearer token is empty", ErrUnauthenticated)
	}
	claims, err := a.verifier.Verify(ctx, token)
	if err != nil {
		// Signature, issuer, audience, and expiry failures are always
		// authentication failures, never permission failures.
		return TenantContext{}, fmt.Errorf("%w: token verification failed", ErrUnauthenticated)
	}
	return claimsToTenantContext(claims, requestIDFromContext(ctx))
}

// Middleware authenticates a request and injects TenantContext into the
// request context. It is intended for protected routes only; public route
// allowlisting is owned by the edge route table.
func (a *EdgeAuthenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a == nil {
			writeAuthError(w, r, http.StatusUnauthorized, CodeUnauthenticated, "authentication failed")
			return
		}
		r = requestWithResolvedID(r)

		var (
			tc  TenantContext
			err error
		)
		if a.cfg.TrustGateway {
			tc, err = a.authenticateGateway(r)
		} else {
			tc, err = a.authenticateDirect(r)
		}
		if err != nil {
			writeAuthenticatorError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(withTenantContext(r.Context(), tc)))
	})
}

func (a *EdgeAuthenticator) authenticateDirect(r *http.Request) (TenantContext, error) {
	// A direct connection must never accept gateway identity headers: they are
	// client-forgeable outside the gateway trust boundary.
	if headerPresent(r, internalHeaderNames...) {
		return TenantContext{}, permissionError("internal gateway headers are not allowed in direct mode")
	}
	if headerPresent(r, directForbiddenClientHeaders...) {
		return TenantContext{}, permissionError("client identity headers are not allowed")
	}

	token, ok := bearerToken(r)
	if !ok {
		return TenantContext{}, fmt.Errorf("%w: bearer token is required", ErrUnauthenticated)
	}
	tc, err := a.Authenticate(r.Context(), token)
	if err != nil {
		return TenantContext{}, err
	}
	return applyTenantSelection(r, tc)
}

func (a *EdgeAuthenticator) authenticateGateway(r *http.Request) (TenantContext, error) {
	if headerPresent(r, "Authorization") {
		return TenantContext{}, permissionError("Authorization is not accepted in gateway trust mode")
	}
	// Client identity headers must have been stripped by the gateway. Seeing
	// them at the core means the gateway contract was bypassed.
	if headerPresent(r, clientIdentityHeaderNames...) {
		return TenantContext{}, permissionError("client identity headers must be stripped by the gateway")
	}
	if !headerPresent(r, internalTenantHeader) {
		return TenantContext{}, permissionError("missing tenant context")
	}

	tenantRaw := strings.TrimSpace(r.Header.Get(internalTenantHeader))
	userRaw := strings.TrimSpace(r.Header.Get(internalUserHeader))
	if tenantRaw == "" {
		return TenantContext{}, permissionError("missing tenant context")
	}
	if userRaw == "" {
		return TenantContext{}, permissionError("missing user context")
	}
	tenantID, err := uuid.Parse(tenantRaw)
	if err != nil {
		return TenantContext{}, permissionError("tenant context is invalid")
	}
	userID, err := uuid.Parse(userRaw)
	if err != nil {
		return TenantContext{}, permissionError("user context is invalid")
	}

	orgIDs, err := parseUUIDList("x-ontoaxis-org-ids", splitHeaderList(r.Header.Get(internalOrgIDsHeader)))
	if err != nil {
		return TenantContext{}, permissionError("org context is invalid")
	}
	workspaceIDs, err := parseUUIDList("x-ontoaxis-workspace-ids", splitHeaderList(r.Header.Get(internalWorkspaceIDsHeader)))
	if err != nil {
		return TenantContext{}, permissionError("workspace context is invalid")
	}

	requestID := strings.TrimSpace(r.Header.Get(internalRequestIDHeader))
	if requestID == "" {
		requestID = requestIDFromRequest(r)
	}

	return TenantContext{
		TenantID:     tenantID,
		UserID:       userID,
		OrgIDs:       orgIDs,
		WorkspaceIDs: workspaceIDs,
		Roles:        normalizeRoles(splitHeaderList(r.Header.Get(internalRolesHeader))),
		RequestID:    requestID,
	}, nil
}

func applyTenantSelection(r *http.Request, tc TenantContext) (TenantContext, error) {
	raw := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if raw == "" {
		return tc, nil
	}
	target, err := uuid.Parse(raw)
	if err != nil {
		return TenantContext{}, permissionError("X-Tenant-Id is not a valid tenant UUID")
	}
	if target == tc.TenantID {
		return tc, nil
	}
	if !hasRole(tc.Roles, "platform-admin") {
		return TenantContext{}, permissionError("tenant selection is not permitted")
	}
	for _, orgID := range tc.OrgIDs {
		if orgID == target {
			tc.TenantID = target
			return tc, nil
		}
	}
	return TenantContext{}, permissionError("tenant selection is outside the allowed organizations")
}

func writeAuthenticatorError(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil && errorsIsPermission(err) {
		writeAuthError(w, r, http.StatusForbidden, CodePermissionDenied, "permission denied")
		return
	}
	writeAuthError(w, r, http.StatusUnauthorized, CodeUnauthenticated, "authentication failed")
}

func permissionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrPermissionDenied, fmt.Sprintf(format, args...))
}

// EdgeAuthHandler is the forward-auth endpoint consumed by non-Envoy gateways
// (Traefik). It authenticates a direct Bearer token and returns the trusted
// x-ontoaxis-* identity headers in the response. Gateways MUST strip all
// x-ontoaxis-* request headers before calling this endpoint.
func (a *EdgeAuthenticator) EdgeAuthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = requestWithResolvedID(r)
		tc, err := a.authenticateDirect(r)
		if err != nil {
			writeAuthenticatorError(w, r, err)
			return
		}
		h := w.Header()
		h.Set(internalTenantHeader, tc.TenantID.String())
		h.Set(internalUserHeader, tc.UserID.String())
		if len(tc.OrgIDs) > 0 {
			h.Set(internalOrgIDsHeader, joinUUIDs(tc.OrgIDs))
		}
		if len(tc.WorkspaceIDs) > 0 {
			h.Set(internalWorkspaceIDsHeader, joinUUIDs(tc.WorkspaceIDs))
		}
		if len(tc.Roles) > 0 {
			h.Set(internalRolesHeader, strings.Join(tc.Roles, ","))
		}
		if tc.RequestID != "" {
			h.Set(internalRequestIDHeader, tc.RequestID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func joinUUIDs(ids []uuid.UUID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id.String())
	}
	return strings.Join(parts, ",")
}
