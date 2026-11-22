package authz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/policies"
)

// Authorizer is the single authorization decision point. Business modules pass
// only an action and a trusted resource fact; roles and tenant come from the
// verified context.
type Authorizer struct {
	cfg     Config
	catalog *Catalog
	bundle  *Bundle
	cedar   *cedarEngine
	fga     FGAClient
	auditor *Auditor
	metrics *Metrics
	tracer  trace.Tracer
}

// Options assembles the authorizer. FGAClient, AuditWriter, and Metrics are
// injectable for tests; production callers pass only Config and let New build
// the OpenFGA adapter.
type Options struct {
	Config      Config
	FGAClient   FGAClient
	AuditWriter io.Writer
	Metrics     *Metrics
	Auditor     *Auditor
	Tracer      trace.Tracer
}

// New loads the embedded bundle, cross-checks the catalog/schema, verifies the
// OpenFGA store and model, and emits the policy-change audit event. It fails
// closed: any validation or dependency error aborts startup.
func New(ctx context.Context, opts Options) (*Authorizer, error) {
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	sourceCommit, err := resolveSourceCommit(opts.Config.SourceCommit)
	if err != nil {
		return nil, err
	}
	opts.Config.SourceCommit = sourceCommit
	bundleFS := fs.FS(policies.FS)
	if opts.Config.PolicyDir != "" {
		bundleFS = os.DirFS(opts.Config.PolicyDir)
	}
	bundle, err := LoadBundle(bundleFS)
	if err != nil {
		return nil, err
	}
	catalog, err := DefaultCatalog()
	if err != nil {
		return nil, err
	}
	if err := ValidateCatalogAgainstSchema(catalog, bundle); err != nil {
		return nil, err
	}
	cedarEngine, err := newCedarEngine(bundle, catalog)
	if err != nil {
		return nil, err
	}
	fga := opts.FGAClient
	if fga == nil {
		fga, err = NewOpenFGAClient(opts.Config)
		if err != nil {
			return nil, err
		}
	}
	startupCtx, cancel := context.WithTimeout(ctx, opts.Config.DecisionTimeout)
	defer cancel()
	if err := startupWithRetry(startupCtx, fga, 3); err != nil {
		return nil, fmt.Errorf("authorization startup check: %w", err)
	}
	auditor := opts.Auditor
	if auditor == nil {
		writer := opts.AuditWriter
		if writer == nil {
			writer = os.Stdout
		}
		auditor = NewAuditor(writer, opts.Config.OpenFGAModelID, bundle.ModelDigest, bundle.Digest, opts.Config.SourceCommit)
	}
	a := &Authorizer{
		cfg:     opts.Config,
		catalog: catalog,
		bundle:  bundle,
		cedar:   cedarEngine,
		fga:     fga,
		auditor: auditor,
		metrics: opts.Metrics,
		tracer:  opts.Tracer,
	}
	if err := auditor.EmitPolicyChange(ctx, ""); err != nil {
		return nil, fmt.Errorf("emit policy-change audit event: %w", err)
	}
	return a, nil
}

func startupWithRetry(ctx context.Context, fga FGAClient, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if err = fga.Startup(ctx); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		if attempt == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	return err
}

// Close releases the OpenFGA client.
func (a *Authorizer) Close() error {
	if a == nil || a.fga == nil {
		return nil
	}
	return a.fga.Close()
}

// BundleDigest returns the content digest recorded in audit.
func (a *Authorizer) BundleDigest() string {
	if a == nil || a.bundle == nil {
		return ""
	}
	return a.bundle.Digest
}

// resolveSourceCommit records the code revision in every audit event. An
// empty value is a startup error: the deployment must inject the commit or
// build with VCS metadata.
func resolveSourceCommit(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), nil
	}
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && strings.TrimSpace(setting.Value) != "" {
				return strings.TrimSpace(setting.Value), nil
			}
		}
	}
	return "", fmt.Errorf("%s is required (or build with VCS metadata)", EnvironmentSourceCommit)
}

// Ready verifies the authorization dependency and the empty store invariant.
// The app-readyz wiring should call it with a short timeout.
func (a *Authorizer) Ready(ctx context.Context) error {
	if a == nil || a.fga == nil {
		return &Error{Reason: ReasonInternalError, Engine: "readyz"}
	}
	readyCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := a.fga.Startup(readyCtx); err != nil {
		return &Error{Reason: ReasonEngineError, Engine: "openfga", Err: err}
	}
	return nil
}

// Authorize evaluates one request and writes exactly one decision audit event
// before returning. A dependency or audit failure returns a non-nil error and
// the caller must not perform the business side effect.
func (a *Authorizer) Authorize(ctx context.Context, req Request) (decision Decision, err error) {
	start := time.Now()
	tc, ok := authn.FromContext(ctx)
	if !ok {
		return Decision{}, authn.ErrUnauthenticated
	}
	ctx, span := startAuthzSpan(a.tracer, ctx, "authz.check", attribute.String("authz.action", string(req.Action)))
	defer func() {
		if span == nil {
			return
		}
		span.SetAttributes(
			attribute.String("authz.decision", decisionLabel(decision)),
			attribute.String("authz.reason", string(decision.Reason)),
			attribute.String("authz.decision_id", decision.DecisionID),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, string(decision.Reason))
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()
	decisionID := newUUIDv7()
	var followUp evaluationDetails
	memo := memoFromContext(ctx)
	if memo != nil {
		if cached, ok := memo[memoKey(tc, req)]; ok && cached.Allow {
			return cached, nil
		}
	}
	decision, followUp, err = a.evaluate(ctx, tc, req, decisionID, start)
	latency := Latency{FGA: followUp.fga, Cedar: followUp.cedar, Total: time.Since(start)}
	if err != nil {
		a.observe(req.Action, decision, followUp.reason, latency, err)
		if auditErr := a.auditor.EmitDecision(ctx, &decision, followUp.reason, latency, err); auditErr != nil {
			return decision, &Error{Reason: ReasonAuditUnavailable, Engine: "audit", Decision: decision, Err: auditErr}
		}
		return decision, err
	}
	if err := a.auditor.EmitDecision(ctx, &decision, decision.Reason, latency, nil); err != nil {
		return decision, &Error{Reason: ReasonAuditUnavailable, Engine: "audit", Decision: decision, Err: err}
	}
	a.observe(req.Action, decision, decision.Reason, latency, nil)
	if memo != nil && decision.Allow {
		memo[memoKey(tc, req)] = decision
	}
	return decision, nil
}

type evaluationDetails struct {
	reason Reason
	fga    time.Duration
	cedar  time.Duration
}

func (a *Authorizer) evaluate(ctx context.Context, tc authn.TenantContext, req Request, decisionID string, start time.Time) (Decision, evaluationDetails, error) {
	decision := Decision{DecisionID: decisionID, Action: req.Action, Resource: req.Resource}
	decision.Policy = PolicyFacts{BundleDigest: a.bundle.Digest, FGAModelID: a.cfg.OpenFGAModelID}
	details := evaluationDetails{}
	spec, ok := a.catalog.Lookup(req.Action)
	if !ok {
		decision.Reason = ReasonUnknownAction
		details.reason = ReasonUnknownAction
		return decision, details, &Error{Reason: ReasonUnknownAction, Engine: "catalog", Decision: decision}
	}
	if req.Resource.Type == "" || req.Resource.ID == "" || req.Resource.TenantID == "" {
		decision.Reason = ReasonAttributeMissing
		details.reason = ReasonAttributeMissing
		return decision, details, &Error{Reason: ReasonAttributeMissing, Engine: "resource", Decision: decision}
	}
	if req.Resource.TenantID != tc.TenantID.String() {
		decision.Reason = ReasonNoRelationship
		details.reason = ReasonNoRelationship
		return decision, details, &Error{Reason: ReasonNoRelationship, Engine: "tenant", Decision: decision}
	}
	tuples, err := BuildContextualTuples(tc, req.Resource)
	if err != nil {
		decision.Reason = ReasonAttributeMissing
		details.reason = ReasonAttributeMissing
		return decision, details, &Error{Reason: ReasonAttributeMissing, Engine: "tuples", Decision: decision, Err: err}
	}
	decision.Policy.FGARelation = spec.Relation
	fgaCtx, fgaSpan := startAuthzSpan(a.tracer, ctx, "authz.fga_check", attribute.String("authz.engine", "openfga"))
	defer func() {
		if fgaSpan != nil {
			fgaSpan.End()
		}
	}()
	fgaCtx, cancel := context.WithTimeout(fgaCtx, a.cfg.OpenFGATimeout)
	fgaStart := time.Now()
	check, checkErr := a.fga.Check(fgaCtx, CheckInput{
		User:             "user:" + tc.UserID.String(),
		Relation:         spec.Relation,
		Object:           objectRef(req.Resource.Type, req.Resource.ID),
		ContextualTuples: tuples,
	})
	cancel()
	fgaElapsed := time.Since(fgaStart)
	details.fga = fgaElapsed
	if checkErr != nil {
		reason := ReasonEngineError
		if errors.Is(checkErr, context.DeadlineExceeded) || errors.Is(checkErr, context.Canceled) {
			reason = ReasonEngineTimeout
		}
		decision.Reason = reason
		decision.Policy.FGADecision = "error"
		details.reason = reason
		return decision, details, &Error{Reason: reason, Engine: "openfga", Decision: decision, Err: checkErr}
	}
	if !check.Allowed {
		decision.Reason = ReasonNoRelationship
		decision.Policy.FGADecision = "deny"
		details.reason = ReasonNoRelationship
		return decision, details, &Error{Reason: ReasonNoRelationship, Engine: "openfga", Decision: decision}
	}
	decision.Policy.FGADecision = "allow"
	cedarCtx, cedarSpan := startAuthzSpan(a.tracer, ctx, "authz.cedar_eval", attribute.String("authz.engine", "cedar"))
	_ = cedarCtx
	defer func() {
		if cedarSpan != nil {
			cedarSpan.End()
		}
	}()
	cedarStart := time.Now()
	allowed, reason, matched, cedarErr := a.cedar.Evaluate(tc, spec, req.Resource)
	cedarElapsed := time.Since(cedarStart)
	details.cedar = cedarElapsed
	if cedarErr != nil {
		decision.Reason = ReasonEngineError
		decision.Policy.CedarDecision = "error"
		details.reason = ReasonEngineError
		return decision, details, &Error{Reason: ReasonEngineError, Engine: "cedar", Decision: decision, Err: cedarErr}
	}
	if elapsed := time.Since(start); elapsed > a.cfg.DecisionTimeout {
		decision.Reason = ReasonEngineTimeout
		decision.Policy.CedarDecision = "timeout"
		details.reason = ReasonEngineTimeout
		return decision, details, &Error{Reason: ReasonEngineTimeout, Engine: "decision", Decision: decision}
	}
	decision.Policy.CedarPolicyIDs = matched
	if allowed {
		decision.Allow = true
		decision.Reason = ReasonAllow
		decision.Policy.CedarDecision = "allow"
		return decision, details, nil
	}
	decision.Reason = reason
	decision.Policy.CedarDecision = "deny"
	details.reason = reason
	return decision, details, &Error{Reason: reason, Engine: "cedar", Decision: decision}
}

func (a *Authorizer) observe(action Action, decision Decision, reason Reason, latency Latency, err error) {
	if a.metrics == nil {
		return
	}
	a.metrics.observeDecision(action, decision.Allow, reason)
	a.metrics.observeDuration("fga", latency.FGA.Seconds())
	a.metrics.observeDuration("cedar", latency.Cedar.Seconds())
	if err != nil {
		var authzErr *Error
		if errors.As(err, &authzErr) && IsDependencyFailure(authzErr.Reason) {
			a.metrics.observeError(authzErr.Engine, authzErr.Reason)
		}
	}
}

// Explain is restricted to tenant-admin/platform-admin. It evaluates the same
// relationship and Cedar path and returns matched relation/policy identifiers;
// it never exposes policy text or subject attributes.
func (a *Authorizer) Explain(ctx context.Context, req Request) (Decision, error) {
	tc, ok := authn.FromContext(ctx)
	if !ok {
		return Decision{}, authn.ErrUnauthenticated
	}
	if !hasAdminRole(tc.Roles) {
		decision := Decision{DecisionID: newUUIDv7(), Action: req.Action, Resource: req.Resource, Reason: ReasonAdminRequired}
		return decision, &Error{Reason: ReasonAdminRequired, Engine: "explain", Decision: decision}
	}
	decision, _, err := a.evaluate(ctx, tc, req, newUUIDv7(), time.Now())
	if explainErr := a.auditor.EmitExplain(ctx, decision, nil); explainErr != nil {
		return decision, &Error{Reason: ReasonAuditUnavailable, Engine: "audit", Decision: decision, Err: explainErr}
	}
	return decision, err
}

func hasAdminRole(roles []string) bool {
	for _, role := range roles {
		if role == "tenant-admin" || role == "platform-admin" {
			return true
		}
	}
	return false
}

type memoKeyType struct{}

type requestMemo map[string]Decision

// WithRequestMemo enables per-request decision memoization. It must be set on
// the request context by the guard; there is no cross-request allow cache.
func WithRequestMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, memoKeyType{}, requestMemo{})
}

func memoFromContext(ctx context.Context) requestMemo {
	if ctx == nil {
		return nil
	}
	memo, _ := ctx.Value(memoKeyType{}).(requestMemo)
	return memo
}

func memoKey(tc authn.TenantContext, req Request) string {
	return strings.Join([]string{
		tc.TenantID.String(), tc.UserID.String(), string(req.Action),
		req.Resource.Type, req.Resource.ID, req.Resource.TenantID,
	}, "\x00")
}

func startAuthzSpan(t trace.Tracer, ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if t == nil {
		return ctx, nil
	}
	return t.Start(ctx, name, trace.WithAttributes(attrs...))
}

func decisionLabel(decision Decision) string {
	if decision.Allow {
		return "allow"
	}
	if decision.Reason == "" {
		return "deny"
	}
	return "deny"
}
