package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

type fakeApplier struct {
	upsertIndex string
	upsertDocs  []Document
	deleteIndex string
	deleteIDs   []string
	upsertErr   error
	deleteErr   error
}

func (f *fakeApplier) UpsertDocuments(_ context.Context, indexUID string, docs []Document) error {
	f.upsertIndex = indexUID
	f.upsertDocs = append(f.upsertDocs, docs...)
	return f.upsertErr
}

func (f *fakeApplier) DeleteDocuments(_ context.Context, indexUID string, ids []string) error {
	f.deleteIndex = indexUID
	f.deleteIDs = append(f.deleteIDs, ids...)
	return f.deleteErr
}

type fakeReader struct {
	object domain.Object
	err    error
	called bool
}

func (f *fakeReader) GetObjectForSearch(_ context.Context, _, _ string) (domain.Object, error) {
	f.called = true
	return f.object, f.err
}

func testObject(t *testing.T) domain.Object {
	t.Helper()
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	return domain.Object{
		ID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		TenantID:     tenantID,
		ObjectTypeID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Version:      3,
		Properties:   map[string]any{"name": "Widget", "color": "blue"},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func testOutboxEvent(eventType string) OutboxEvent {
	return OutboxEvent{
		ID:            "44444444-4444-4444-4444-444444444444",
		TenantID:      "11111111-1111-1111-1111-111111111111",
		AggregateType: "object",
		AggregateID:   "22222222-2222-2222-2222-222222222222",
		EventType:     eventType,
		CreatedAt:     time.Now().UTC(),
	}
}

func TestApplyEventUpsertsDocument(t *testing.T) {
	applier := &fakeApplier{}
	reader := &fakeReader{object: testObject(t)}
	consumer := NewConsumer(nil, applier, reader, Config{IndexVersion: "v1"}, nil, nil, nil)

	if err := consumer.ApplyEvent(context.Background(), testOutboxEvent(EventObjectUpdated)); err != nil {
		t.Fatalf("ApplyEvent error = %v", err)
	}
	if !reader.called {
		t.Fatal("reader was not called")
	}
	wantIndex := IndexName("v1", "11111111-1111-1111-1111-111111111111")
	if applier.upsertIndex != wantIndex {
		t.Fatalf("upsert index = %q, want %q", applier.upsertIndex, wantIndex)
	}
	if len(applier.upsertDocs) != 1 || applier.upsertDocs[0].ID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("upsert docs = %#v", applier.upsertDocs)
	}
	if applier.upsertDocs[0].DisplayName != "Widget" {
		t.Fatalf("display name = %q", applier.upsertDocs[0].DisplayName)
	}
}

func TestApplyEventNotFoundDeletesDocument(t *testing.T) {
	applier := &fakeApplier{}
	reader := &fakeReader{err: domain.ErrNotFound}
	consumer := NewConsumer(nil, applier, reader, Config{IndexVersion: "v1"}, nil, nil, nil)

	if err := consumer.ApplyEvent(context.Background(), testOutboxEvent(EventObjectDeleted)); err != nil {
		t.Fatalf("ApplyEvent error = %v", err)
	}
	if applier.deleteIndex != IndexName("v1", "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("delete index = %q", applier.deleteIndex)
	}
	if len(applier.deleteIDs) != 1 || applier.deleteIDs[0] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("delete ids = %#v", applier.deleteIDs)
	}
	if len(applier.upsertDocs) != 0 {
		t.Fatalf("unexpected upserts: %#v", applier.upsertDocs)
	}
}

func TestApplyEventReaderError(t *testing.T) {
	applier := &fakeApplier{}
	reader := &fakeReader{err: errors.New("database down")}
	consumer := NewConsumer(nil, applier, reader, Config{IndexVersion: "v1"}, nil, nil, nil)

	err := consumer.ApplyEvent(context.Background(), testOutboxEvent(EventObjectCreated))
	if err == nil || !stringsContains(err.Error(), "read object") {
		t.Fatalf("ApplyEvent error = %v, want read object error", err)
	}
}

func TestApplyEventUnknownTypeIsNoop(t *testing.T) {
	applier := &fakeApplier{}
	reader := &fakeReader{err: errors.New("must not be called")}
	consumer := NewConsumer(nil, applier, reader, Config{IndexVersion: "v1"}, nil, nil, nil)

	if err := consumer.ApplyEvent(context.Background(), testOutboxEvent("objects.object.mystery.v1")); err != nil {
		t.Fatalf("ApplyEvent unknown error = %v", err)
	}
	if reader.called {
		t.Fatal("reader called for unknown event")
	}
}

func TestApplyEventApplierErrorIsReturned(t *testing.T) {
	applier := &fakeApplier{upsertErr: errors.New("meili down")}
	reader := &fakeReader{object: testObject(t)}
	consumer := NewConsumer(nil, applier, reader, Config{IndexVersion: "v1"}, nil, nil, nil)

	err := consumer.ApplyEvent(context.Background(), testOutboxEvent(EventObjectCreated))
	if err == nil || !errors.Is(err, applier.upsertErr) {
		t.Fatalf("ApplyEvent error = %v, want applier error", err)
	}
}

func TestBackoffForAttempt(t *testing.T) {
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{0, time.Second},
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{6, 32 * time.Second},
		{20, 5 * time.Minute},
		{100, 5 * time.Minute},
	}
	for _, tt := range tests {
		if got := backoffForAttempt(tt.attempts); got != tt.want {
			t.Fatalf("backoffForAttempt(%d) = %s, want %s", tt.attempts, got, tt.want)
		}
	}
}

func TestClientApplierRequiresClient(t *testing.T) {
	applier := ClientApplier{}
	if err := applier.UpsertDocuments(context.Background(), "idx", nil); err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil client upsert error = %v, want ErrUnavailable", err)
	}
	if err := applier.DeleteDocuments(context.Background(), "idx", nil); err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil client delete error = %v, want ErrUnavailable", err)
	}
}

func TestReplayDeadRequiresPool(t *testing.T) {
	consumer := NewConsumer(nil, nil, nil, Config{}, nil, nil, nil)
	if _, err := consumer.ReplayDead(context.Background(), 10); err == nil {
		t.Fatal("ReplayDead with nil pool returned nil error")
	}
}

func TestEventConstants(t *testing.T) {
	for _, eventType := range []string{EventObjectCreated, EventObjectUpdated, EventObjectDeleted} {
		if !knownEventType(eventType) {
			t.Fatalf("knownEventType(%q) = false", eventType)
		}
	}
	if knownEventType("objects.object.mystery.v1") {
		t.Fatal("unknown event type reported known")
	}
}

func stringsContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
