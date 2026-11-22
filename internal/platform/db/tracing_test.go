package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

func TestQueryTracerRecordsBoundedSpanAndMetric(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	metrics := observability.NewMetrics(prometheus.NewRegistry())

	tracer := NewQueryTracer("ontoaxis", provider.Tracer("test"), metrics)
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM secret_table WHERE token='x'"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "postgres select" {
		t.Fatalf("unexpected spans: %#v", spans)
	}
	if len(spans[0].Attributes) == 0 {
		t.Fatal("expected DB attributes")
	}
	for _, a := range spans[0].Attributes {
		if string(a.Key) == "db.statement" {
			t.Fatal("SQL text must not be recorded")
		}
	}
	if got := testutil.CollectAndCount(metrics.DBQueryDuration, "ontoaxis_db_query_duration_seconds"); got != 1 {
		t.Fatalf("query metric families = %d", got)
	}
}
