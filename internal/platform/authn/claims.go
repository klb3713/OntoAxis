package authn

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultIssuer is the Keycloak realm issuer fixed by ARCHITECTURE.md 6.3.1.
	DefaultIssuer = "https://auth.ontoaxis.local/realms/ontoaxis"
	// DefaultAudience is the API audience required by ARCHITECTURE.md 6.3.1.
	DefaultAudience = "ontoaxis-api"
)

// Claims is the verified JWT claim set consumed by the identity edge. Field
// names follow the JWT claim names in ARCHITECTURE.md 6.3.1.
type Claims struct {
	Issuer       string
	Audience     []string
	Subject      string
	TenantID     string
	OrgIDs       []string
	WorkspaceIDs []string
	Roles        []string
	Email        string
	Name         string
	ExpiresAt    time.Time
	IssuedAt     time.Time
	AuthTime     time.Time
}

// Known roles from ARCHITECTURE.md 6.3.1. Unknown roles are dropped so a
// malformed or future token cannot accidentally gain privileges.
var knownRoles = map[string]struct{}{
	"platform-admin":  {},
	"tenant-admin":    {},
	"workspace-admin": {},
	"member":          {},
	"viewer":          {},
}

func normalizeRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	seen := make(map[string]struct{}, len(roles))
	for _, raw := range roles {
		role := strings.TrimSpace(raw)
		if role == "" {
			continue
		}
		if _, known := knownRoles[role]; !known {
			continue
		}
		if _, dup := seen[role]; dup {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func parseUUIDList(field string, values []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("claim %s contains an invalid UUID: %w", field, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// claimsToTenantContext maps verified claims to the trusted tenant context.
// Missing or invalid tenant identity is a permission problem (403); malformed
// subject or list claims are authentication problems (401).
func claimsToTenantContext(claims Claims, requestID string) (TenantContext, error) {
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return TenantContext{}, fmt.Errorf("%w: sub claim is required", ErrUnauthenticated)
	}
	userID, err := uuid.Parse(subject)
	if err != nil {
		return TenantContext{}, fmt.Errorf("%w: sub claim is not a valid UUID", ErrUnauthenticated)
	}

	tenant := strings.TrimSpace(claims.TenantID)
	if tenant == "" {
		return TenantContext{}, fmt.Errorf("%w: tenant_id claim is required", ErrPermissionDenied)
	}
	tenantID, err := uuid.Parse(tenant)
	if err != nil {
		return TenantContext{}, fmt.Errorf("%w: tenant_id claim is not a valid UUID", ErrPermissionDenied)
	}

	orgIDs, err := parseUUIDList("org_ids", claims.OrgIDs)
	if err != nil {
		return TenantContext{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	workspaceIDs, err := parseUUIDList("workspace_ids", claims.WorkspaceIDs)
	if err != nil {
		return TenantContext{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	if strings.TrimSpace(claims.Email) == "" {
		return TenantContext{}, fmt.Errorf("%w: email claim is required", ErrUnauthenticated)
	}

	return TenantContext{
		TenantID:     tenantID,
		UserID:       userID,
		OrgIDs:       orgIDs,
		WorkspaceIDs: workspaceIDs,
		Roles:        normalizeRoles(claims.Roles),
		RequestID:    requestID,
	}, nil
}
