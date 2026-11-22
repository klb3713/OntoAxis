package db

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

type queryTracer struct {
	database string
	tracer   trace.Tracer
	metrics  *observability.Metrics
}

type queryState struct {
	span  trace.Span
	op    string
	start time.Time
}

type queryStateKey struct{}

// NewQueryTracer returns a pgx QueryTracer that records bounded, SQL-free
// spans and the shared DB query latency histogram.
func NewQueryTracer(database string, tracer trace.Tracer, metrics *observability.Metrics) pgx.QueryTracer {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("ontoaxis")
	}
	return &queryTracer{database: database, tracer: tracer, metrics: metrics}
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op := sqlOperation(data.SQL)
	ctx, span := t.tracer.Start(ctx, "postgres "+op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.namespace", t.database),
			attribute.String("db.operation", op),
		),
	)
	return context.WithValue(ctx, queryStateKey{}, &queryState{span: span, op: op, start: time.Now()})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, _ := ctx.Value(queryStateKey{}).(*queryState)
	if state == nil {
		return
	}
	if t.metrics != nil {
		t.metrics.DBQueryDuration.WithLabelValues(state.op).Observe(time.Since(state.start).Seconds())
	}
	if data.Err != nil {
		state.span.SetStatus(codes.Error, "query failed")
	}
	state.span.End()
}

// sqlOperation extracts the first SQL keyword so neither SQL text nor bound
// arguments are ever placed on spans or metric labels.
func sqlOperation(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "query"
	}
	op := strings.ToLower(fields[0])
	for _, r := range op {
		if r < 'a' || r > 'z' {
			return "query"
		}
	}
	return op
}
