package authn

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeRoles(t *testing.T) {
	got := normalizeRoles([]string{"viewer", "platform-admin", "unknown", "viewer", " member ", ""})
	want := []string{"member", "platform-admin", "viewer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeRoles = %v, want %v", got, want)
	}
}

func validClaims() Claims {
	return Claims{
		Issuer:       DefaultIssuer,
		Audience:     []string{DefaultAudience},
		Subject:      "22222222-2222-2222-2222-222222222222",
		TenantID:     "11111111-1111-1111-1111-111111111111",
		OrgIDs:       []string{"33333333-3333-3333-3333-333333333333"},
		WorkspaceIDs: []string{"44444444-4444-4444-4444-444444444444"},
		Roles:        []string{"viewer", "member", "unknown"},
		Email:        "user@example.com",
	}
}

func TestClaimsToTenantContext(t *testing.T) {
	claims := validClaims()
	tc, err := claimsToTenantContext(claims, "req-1")
	if err != nil {
		t.Fatalf("claimsToTenantContext error: %v", err)
	}
	if tc.TenantID.String() != claims.TenantID {
		t.Fatalf("TenantID = %s, want %s", tc.TenantID, claims.TenantID)
	}
	if tc.UserID.String() != claims.Subject {
		t.Fatalf("UserID = %s, want %s", tc.UserID, claims.Subject)
	}
	if len(tc.OrgIDs) != 1 || len(tc.WorkspaceIDs) != 1 {
		t.Fatalf("org/workspace ids not mapped: %+v", tc)
	}
	if want := []string{"member", "viewer"}; !reflect.DeepEqual(tc.Roles, want) {
		t.Fatalf("Roles = %v, want %v", tc.Roles, want)
	}
	if tc.RequestID != "req-1" {
		t.Fatalf("RequestID = %q, want req-1", tc.RequestID)
	}
}

func TestClaimsToTenantContextMissingTenant(t *testing.T) {
	claims := validClaims()
	claims.TenantID = ""
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestClaimsToTenantContextInvalidTenant(t *testing.T) {
	claims := validClaims()
	claims.TenantID = "not-a-uuid"
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestClaimsToTenantContextInvalidSubject(t *testing.T) {
	claims := validClaims()
	claims.Subject = "not-a-uuid"
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestClaimsToTenantContextMissingSubject(t *testing.T) {
	claims := validClaims()
	claims.Subject = " "
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestClaimsToTenantContextInvalidWorkspaceID(t *testing.T) {
	claims := validClaims()
	claims.WorkspaceIDs = []string{"not-a-uuid"}
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestClaimsToTenantContextInvalidOrgID(t *testing.T) {
	claims := validClaims()
	claims.OrgIDs = []string{"not-a-uuid"}
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestClaimsToTenantContextMissingEmail(t *testing.T) {
	claims := validClaims()
	claims.Email = " "
	_, err := claimsToTenantContext(claims, "")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}
