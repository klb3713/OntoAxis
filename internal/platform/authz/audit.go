package authz

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"go.opentelemetry.io/otel/trace"
)

// AuditMeta carries handler facts that are safe to audit. It is set by the
// guard, never by a client.
type AuditMeta struct {
	Method string
	Route  string
}

type auditMetaKey struct{}

// WithAuditMeta stores the HTTP method and route template for audit. Raw URLs,
// query strings, headers, and bodies are not recorded.
func WithAuditMeta(ctx context.Context, method, route string) context.Context {
	return context.WithValue(ctx, auditMetaKey{}, AuditMeta{Method: method, Route: route})
}

func auditMetaFromContext(ctx context.Context) AuditMeta {
	if ctx == nil {
		return AuditMeta{}
	}
	meta, _ := ctx.Value(auditMetaKey{}).(AuditMeta)
	return meta
}

// Auditor writes append-only structured audit events. It is synchronous and
// returns write errors so callers can fail closed.
type Auditor struct {
	w            io.Writer
	modelID      string
	modelDigest  string
	bundleDigest string
	sourceCommit string
	now          func() time.Time
}

// NewAuditor creates the v1 stdout audit sink. The sink must be append-only in
// deployment; the process does not rotate or query it.
func NewAuditor(w io.Writer, modelID, modelDigest, bundleDigest, sourceCommit string) *Auditor {
	if w == nil {
		w = os.Stdout
	}
	return &Auditor{w: w, modelID: modelID, modelDigest: modelDigest, bundleDigest: bundleDigest, sourceCommit: sourceCommit, now: time.Now}
}

type auditEnvelope struct {
	Authz auditEvent `json:"authz"`
}

type auditEvent struct {
	SchemaVersion    int            `json:"schema_version"`
	EventID          string         `json:"event_id"`
	EventType        string         `json:"event_type"`
	OccurredAt       string         `json:"occurred_at"`
	TenantID         string         `json:"tenant_id,omitempty"`
	RequestID        string         `json:"request_id,omitempty"`
	TraceID          string         `json:"trace_id,omitempty"`
	Actor            *auditActor    `json:"actor,omitempty"`
	Action           string         `json:"action,omitempty"`
	Resource         *auditResource `json:"resource,omitempty"`
	Decision         string         `json:"decision,omitempty"`
	ReasonCode       string         `json:"reason_code,omitempty"`
	DecisionID       string         `json:"decision_id,omitempty"`
	Policy           *auditPolicy   `json:"policy,omitempty"`
	HTTP             *auditHTTP     `json:"http,omitempty"`
	LatencyMS        *auditLatency  `json:"latency_ms,omitempty"`
	Error            *auditError    `json:"error,omitempty"`
	ModelID          string         `json:"model_id,omitempty"`
	ModelDigest      string         `json:"model_digest,omitempty"`
	BundleDigest     string         `json:"bundle_digest,omitempty"`
	SourceCommit     string         `json:"source_commit,omitempty"`
	PreviousDigest   string         `json:"previous_digest,omitempty"`
	CurrentDigest    string         `json:"current_digest,omitempty"`
	Result           string         `json:"result,omitempty"`
	MatchedRelations []string       `json:"matched_relations,omitempty"`
	MatchedPolicyIDs []string       `json:"matched_policy_ids,omitempty"`
}

type auditActor struct {
	Type  string   `json:"type"`
	ID    string   `json:"id"`
	Roles []string `json:"roles,omitempty"`
}

type auditResource struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	TenantID string `json:"tenant_id,omitempty"`
}

type auditPolicy struct {
	FGAModelID     string   `json:"fga_model_id,omitempty"`
	FGARelation    string   `json:"fga_relation,omitempty"`
	FGADecision    string   `json:"fga_decision,omitempty"`
	CedarPolicyIDs []string `json:"cedar_policy_ids,omitempty"`
	CedarDecision  string   `json:"cedar_decision,omitempty"`
	BundleDigest   string   `json:"bundle_digest,omitempty"`
}

type auditHTTP struct {
	Method string `json:"method,omitempty"`
	Route  string `json:"route,omitempty"`
}

type auditLatency struct {
	FGA   int64 `json:"fga_ms"`
	Cedar int64 `json:"cedar_ms"`
	Total int64 `json:"total_ms"`
}

type auditError struct {
	Engine  string `json:"engine,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type Latency struct {
	FGA   time.Duration
	Cedar time.Duration
	Total time.Duration
}

func (a *Auditor) baseEvent(ctx context.Context, eventType string) auditEvent {
	event := auditEvent{
		SchemaVersion: 1,
		EventID:       newUUIDv7(),
		EventType:     eventType,
		OccurredAt:    a.now().UTC().Format(time.RFC3339Nano),
		RequestID:     observability.RequestID(ctx),
		TraceID:       traceID(ctx),
	}
	if tc, ok := authn.FromContext(ctx); ok {
		event.TenantID = tc.TenantID.String()
		event.Actor = &auditActor{Type: "user", ID: tc.UserID.String(), Roles: append([]string(nil), tc.Roles...)}
	}
	if meta := auditMetaFromContext(ctx); meta.Method != "" || meta.Route != "" {
		event.HTTP = &auditHTTP{Method: meta.Method, Route: meta.Route}
	}
	return event
}

// EmitDecision writes one decision event before the handler has a side effect.
// A nil decision is allowed only for dependency failures.
func (a *Auditor) EmitDecision(ctx context.Context, decision *Decision, reason Reason, latency Latency, engineErr error) error {
	event := a.baseEvent(ctx, "authz.decision")
	event.DecisionID = newUUIDv7()
	if decision != nil {
		event.Action = string(decision.Action)
		event.Resource = &auditResource{Type: decision.Resource.Type, ID: decision.Resource.ID, TenantID: decision.Resource.TenantID}
		event.Decision = "deny"
		if decision.Allow {
			event.Decision = "allow"
		}
		event.DecisionID = decision.DecisionID
		event.Policy = &auditPolicy{
			FGAModelID:     decision.Policy.FGAModelID,
			FGARelation:    decision.Policy.FGARelation,
			FGADecision:    decision.Policy.FGADecision,
			CedarPolicyIDs: decision.Policy.CedarPolicyIDs,
			CedarDecision:  decision.Policy.CedarDecision,
			BundleDigest:   decision.Policy.BundleDigest,
		}
	}
	event.ReasonCode = string(reason)
	event.LatencyMS = &auditLatency{FGA: latency.FGA.Milliseconds(), Cedar: latency.Cedar.Milliseconds(), Total: latency.Total.Milliseconds()}
	if engineErr != nil {
		event.Decision = "error"
		event.Error = &auditError{Engine: "authorization", Code: string(reason), Message: "authorization dependency failed"}
	}
	return a.write(event)
}

// EmitPolicyChange records a successful startup or reload of the immutable
// policy bundle. It is written before readiness.
func (a *Auditor) EmitPolicyChange(ctx context.Context, previousDigest string) error {
	event := a.baseEvent(ctx, "authz.policy_change")
	event.ModelID = a.modelID
	event.ModelDigest = a.modelDigest
	event.BundleDigest = a.bundleDigest
	event.SourceCommit = a.sourceCommit
	event.PreviousDigest = previousDigest
	event.CurrentDigest = a.bundleDigest
	event.Result = "loaded"
	return a.write(event)
}

// EmitExplain records an admin Explain request and its matched relations and
// policies.
func (a *Auditor) EmitExplain(ctx context.Context, decision Decision, matchedRelations []string) error {
	event := a.baseEvent(ctx, "authz.explain")
	event.Action = string(decision.Action)
	event.Resource = &auditResource{Type: decision.Resource.Type, ID: decision.Resource.ID, TenantID: decision.Resource.TenantID}
	event.DecisionID = decision.DecisionID
	event.MatchedRelations = append([]string(nil), matchedRelations...)
	event.MatchedPolicyIDs = append([]string(nil), decision.Policy.CedarPolicyIDs...)
	return a.write(event)
}

func (a *Auditor) write(event auditEvent) error {
	if a == nil {
		return nil
	}
	encoder := json.NewEncoder(a.w)
	if err := encoder.Encode(auditEnvelope{Authz: event}); err != nil {
		return err
	}
	if f, ok := a.w.(*os.File); ok {
		if info, statErr := f.Stat(); statErr == nil && !info.Mode().IsRegular() {
			return nil
		}
	}
	if syncer, ok := a.w.(interface{ Sync() error }); ok {
		return syncer.Sync()
	}
	return nil
}

func newUUIDv7() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func traceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return ""
	}
	return span.TraceID().String()
}
