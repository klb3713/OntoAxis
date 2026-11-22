package authn

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestFromContextMissing(t *testing.T) {
	tc, ok := FromContext(context.Background())
	if ok {
		t.Fatalf("FromContext returned ok=true for empty context")
	}
	if !reflect.DeepEqual(tc, TenantContext{}) {
		t.Fatalf("FromContext returned non-zero context: %+v", tc)
	}
}

func TestFromContextNil(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatalf("FromContext(context.Background()) returned ok=true")
	}
}

func TestRequireTenantMissing(t *testing.T) {
	_, err := RequireTenant(context.Background())
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("RequireTenant error = %v, want ErrUnauthenticated", err)
	}
}

func TestFromContextInjected(t *testing.T) {
	want := TenantContext{
		TenantID:  uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Roles:     []string{"member"},
		RequestID: "req-1",
	}
	ctx := withTenantContext(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatalf("FromContext returned ok=false for injected context")
	}
	if got.TenantID != want.TenantID || got.UserID != want.UserID || got.RequestID != want.RequestID {
		t.Fatalf("FromContext = %+v, want %+v", got, want)
	}
}
