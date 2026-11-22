package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
)

type fakeStore struct {
	item  domain.SavedQuery
	items []domain.SavedQuery
}

func (f *fakeStore) CreateSavedQuery(_ context.Context, tenantID uuid.UUID, name, sql string, ownerID uuid.UUID) (domain.SavedQuery, error) {
	f.item = domain.SavedQuery{ID: uuid.New(), TenantID: tenantID, Name: name, SQL: sql, OwnerID: ownerID}
	return f.item, nil
}
func (f *fakeStore) GetSavedQuery(context.Context, uuid.UUID, uuid.UUID) (domain.SavedQuery, error) {
	return f.item, nil
}
func (f *fakeStore) ListSavedQueries(context.Context, uuid.UUID, int, int) ([]domain.SavedQuery, int64, error) {
	return f.items, int64(len(f.items)), nil
}
func (f *fakeStore) DeleteSavedQuery(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type fakeExecutor struct {
	result domain.QueryResult
	plan   string
	err    error
}

func (f *fakeExecutor) Execute(context.Context, string, int) (domain.QueryResult, error) {
	return f.result, f.err
}
func (f *fakeExecutor) Explain(context.Context, string) (string, error) { return f.plan, f.err }

func TestExecuteRejectsNonSelect(t *testing.T) {
	svc := New(&fakeStore{}, &fakeExecutor{}, nil)
	if _, err := svc.Execute(context.Background(), "DELETE FROM t", 10); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("err = %v, want invalid input", err)
	}
}

func TestExecuteUsesExecutor(t *testing.T) {
	exec := &fakeExecutor{result: domain.QueryResult{Columns: []string{"x"}, Rows: [][]any{{1}}, RowCount: 1}}
	svc := New(&fakeStore{}, exec, nil)
	got, err := svc.Execute(context.Background(), "SELECT 1", 10)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.RowCount != 1 || len(got.Columns) != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestExecuteUnavailableWithoutExecutor(t *testing.T) {
	svc := New(&fakeStore{}, nil, nil)
	if _, err := svc.Execute(context.Background(), "SELECT 1", 10); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
}

func TestSaveQueryValidatesNameAndSQL(t *testing.T) {
	svc := New(&fakeStore{}, nil, nil)
	if _, err := svc.SaveQuery(context.Background(), uuid.New(), uuid.New(), " ", "SELECT 1"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("err = %v, want invalid input", err)
	}
	if _, err := svc.SaveQuery(context.Background(), uuid.New(), uuid.New(), "q", "CREATE TABLE x (id int)"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("err = %v, want invalid input", err)
	}
}

func TestListSavedQueriesClampsLimit(t *testing.T) {
	svc := New(&fakeStore{items: []domain.SavedQuery{{Name: "q"}}}, nil, nil)
	items, total, err := svc.ListSavedQueries(context.Background(), uuid.New(), 999, -1)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("items=%v total=%d err=%v", items, total, err)
	}
}
