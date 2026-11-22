package authz

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/policies"
)

type matrixFailingWriter struct{}

func (matrixFailingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestNegativeMatrix24 is the reviewable negative matrix required by
// docs/architecture/authorization-boundary.md §11.3. Each row asserts the
// stable HTTP status and, for deny/error rows, that no allow decision leaked.
func TestNegativeMatrix24(t *testing.T) {
	baseTenant := uuid.New()
	baseUser := uuid.New()
	otherTenant := uuid.NewString()

	type row struct {
		name   string
		ctx    context.Context
		fga    *fakeFGA
		action Action
		res    ResourceFact
		status int
	}
	ctxFor := func(roles ...string) context.Context {
		return WithRequestMemo(authn.WithTenantContext(context.Background(), authn.TenantContext{
			TenantID: baseTenant, UserID: baseUser, Roles: roles, RequestID: "negative-matrix",
		}))
	}
	tenantCtx := ctxFor("member")
	viewerCtx := ctxFor("viewer")
	memberCtx := ctxFor("member")
	workspaceAdminCtx := ctxFor("workspace-admin")
	tenantAdminCtx := ctxFor("tenant-admin")
	unknownRoleCtx := ctxFor("group:platform")
	noTenantCtx := WithRequestMemo(authn.WithTenantContext(context.Background(), authn.TenantContext{UserID: baseUser, Roles: []string{"member"}}))

	dataset := func(tenant string) ResourceFact {
		return ResourceFact{Type: "dataset", ID: uuid.NewString(), TenantID: tenant}
	}
	pipeline := func(status string) ResourceFact {
		return ResourceFact{Type: "pipeline", ID: uuid.NewString(), TenantID: baseTenant.String(), Status: status}
	}
	rows := []row{
		{name: "无 JWT", ctx: context.Background(), fga: &fakeFGA{allowed: true}, action: "dataset:read", res: dataset(baseTenant.String()), status: 401},
		{name: "JWT 无 tenant_id", ctx: noTenantCtx, fga: &fakeFGA{allowed: true}, action: "dataset:read", res: dataset(baseTenant.String()), status: 403},
		{name: "租户头与 JWT 不一致 GET", ctx: tenantCtx, fga: &fakeFGA{allowed: true}, action: "dataset:read", res: dataset(otherTenant), status: 403},
		{name: "跨租户 PATCH", ctx: tenantCtx, fga: &fakeFGA{allowed: true}, action: "dataset:update", res: dataset(otherTenant), status: 403},
		{name: "跨租户 DELETE", ctx: tenantCtx, fga: &fakeFGA{allowed: true}, action: "dataset:delete", res: dataset(otherTenant), status: 403},
		{name: "viewer POST", ctx: viewerCtx, fga: &fakeFGA{allowed: true}, action: "dataset:create", res: ResourceFact{Type: "tenant", ID: baseTenant.String(), TenantID: baseTenant.String()}, status: 403},
		{name: "viewer PATCH", ctx: viewerCtx, fga: &fakeFGA{allowed: true}, action: "dataset:update", res: dataset(baseTenant.String()), status: 403},
		{name: "viewer DELETE", ctx: viewerCtx, fga: &fakeFGA{allowed: true}, action: "dataset:delete", res: dataset(baseTenant.String()), status: 403},
		{name: "member 组织管理", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "tenancy:organization:update", res: ResourceFact{Type: "organization", ID: uuid.NewString(), TenantID: baseTenant.String()}, status: 403},
		{name: "member 成员管理", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "tenancy:membership:add", res: ResourceFact{Type: "organization", ID: uuid.NewString(), TenantID: baseTenant.String()}, status: 403},
		{name: "workspace-admin 组织创建", ctx: workspaceAdminCtx, fga: &fakeFGA{allowed: true}, action: "tenancy:organization:create", res: ResourceFact{Type: "tenant", ID: baseTenant.String(), TenantID: baseTenant.String()}, status: 403},
		{name: "workspace-admin 组织更新", ctx: workspaceAdminCtx, fga: &fakeFGA{allowed: true}, action: "tenancy:organization:update", res: ResourceFact{Type: "organization", ID: uuid.NewString(), TenantID: baseTenant.String()}, status: 403},
		{name: "tenant-admin 审计读取", ctx: tenantAdminCtx, fga: &fakeFGA{allowed: true}, action: "audit:read", res: ResourceFact{Type: "tenant", ID: baseTenant.String(), TenantID: baseTenant.String()}, status: 200},
		{name: "workspace-admin 审计读取", ctx: workspaceAdminCtx, fga: &fakeFGA{allowed: true}, action: "audit:read", res: ResourceFact{Type: "tenant", ID: baseTenant.String(), TenantID: baseTenant.String()}, status: 403},
		{name: "非 owner 删除 SavedQuery", ctx: memberCtx, fga: &fakeFGA{allowed: false}, action: "saved_query:delete", res: ResourceFact{Type: "saved_query", ID: uuid.NewString(), TenantID: baseTenant.String(), OwnerID: uuid.NewString()}, status: 403},
		{name: "viewer owner 删除 SavedQuery", ctx: viewerCtx, fga: &fakeFGA{allowed: true}, action: "saved_query:delete", res: ResourceFact{Type: "saved_query", ID: uuid.NewString(), TenantID: baseTenant.String(), OwnerID: baseUser.String()}, status: 403},
		{name: "owner member 删除 SavedQuery", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "saved_query:delete", res: ResourceFact{Type: "saved_query", ID: uuid.NewString(), TenantID: baseTenant.String(), OwnerID: baseUser.String()}, status: 200},
		{name: "archived Pipeline 触发", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "pipeline:invoke", res: pipeline("archived"), status: 403},
		{name: "archived Pipeline 更新", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "pipeline:update", res: pipeline("archived"), status: 403},
		{name: "draft Pipeline 触发", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "pipeline:invoke", res: pipeline("draft"), status: 403},
		{name: "disabled ActionType 运行", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "action_type:run", res: ResourceFact{Type: "action_type", ID: uuid.NewString(), TenantID: baseTenant.String(), Status: "disabled"}, status: 403},
		{name: "paused Connector 同步", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "connector:sync", res: ResourceFact{Type: "connector", ID: uuid.NewString(), TenantID: baseTenant.String(), Status: "paused"}, status: 403},
		{name: "queued Execution 重试", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "action_execution:retry", res: ResourceFact{Type: "action_execution", ID: uuid.NewString(), TenantID: baseTenant.String(), Status: "queued"}, status: 403},
		{name: "succeeded Execution 取消", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "action_execution:cancel", res: ResourceFact{Type: "action_execution", ID: uuid.NewString(), TenantID: baseTenant.String(), Status: "succeeded"}, status: 403},
		{name: "组主体/未知角色", ctx: unknownRoleCtx, fga: &fakeFGA{allowed: true}, action: "dataset:read", res: dataset(baseTenant.String()), status: 403},
		{name: "未登记动作", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "dataset:unknown", res: dataset(baseTenant.String()), status: 403},
		{name: "OpenFGA 超时", ctx: memberCtx, fga: &fakeFGA{err: context.DeadlineExceeded}, action: "dataset:read", res: dataset(baseTenant.String()), status: 503},
		{name: "Cedar 错误", ctx: memberCtx, fga: &fakeFGA{allowed: true}, action: "dataset:read", res: ResourceFact{Type: "not_a_resource", ID: uuid.NewString(), TenantID: baseTenant.String()}, status: 503},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAuthorizer(t, tc.fga, nil)
			_, err := a.Authorize(tc.ctx, Request{Action: tc.action, Resource: tc.res})
			status := 200
			if err != nil {
				status = statusOf(err)
			}
			if status != tc.status {
				t.Fatalf("status = %d, want %d (err=%v)", status, tc.status, err)
			}
		})
	}

	t.Run("审计失败", func(t *testing.T) {
		a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
		a.auditor = NewAuditor(matrixFailingWriter{}, "model-test", a.bundle.ModelDigest, a.bundle.Digest, "test")
		_, err := a.Authorize(tenantCtx, Request{Action: "dataset:read", Resource: dataset(baseTenant.String())})
		if statusOf(err) != 503 {
			t.Fatalf("status = %d, want 503 (err=%v)", statusOf(err), err)
		}
	})

	t.Run("空策略集启动失败", func(t *testing.T) {
		dir := t.TempDir()
		copyPoliciesForTest(t, dir)
		if err := os.Remove(filepath.Join(dir, "platform", "role-matrix.cedar")); err != nil {
			t.Fatal(err)
		}
		_, err := New(context.Background(), Options{Config: Config{
			OpenFGAApiURL: "http://openfga.invalid", OpenFGAStoreID: "store", OpenFGAModelID: "model",
			PolicyDir: dir, OpenFGATimeout: 100000000, DecisionTimeout: 200000000, AuditSink: DefaultAuditSink, SourceCommit: "test",
		}, FGAClient: &fakeFGA{allowed: true}, AuditWriter: io.Discard})
		if err == nil {
			t.Fatal("expected startup failure for missing policy")
		}
	})

	t.Run("OpenFGA store 有元组启动失败", func(t *testing.T) {
		_, err := New(context.Background(), Options{Config: Config{
			OpenFGAApiURL: "http://openfga.invalid", OpenFGAStoreID: "store", OpenFGAModelID: "model",
			OpenFGATimeout: 100000000, DecisionTimeout: 200000000, AuditSink: DefaultAuditSink, SourceCommit: "test",
		}, FGAClient: &fakeFGA{allowed: true, startup: errors.New("store must be empty")}, AuditWriter: io.Discard})
		if err == nil {
			t.Fatal("expected startup failure for non-empty store")
		}
	})
}

func copyPoliciesForTest(t *testing.T, dir string) {
	t.Helper()
	err := fs.WalkDir(policies.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := policies.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func statusOf(err error) int {
	if err == nil {
		return 200
	}
	if errors.Is(err, authn.ErrUnauthenticated) {
		return 401
	}
	var authzErr *Error
	if errors.As(err, &authzErr) {
		return authzErr.HTTPStatus()
	}
	return 500
}
