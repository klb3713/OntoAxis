package domain

import (
	"errors"
	"testing"
	"time"
)

func TestParseDatasetFormat(t *testing.T) {
	for _, raw := range []string{"iceberg", " Iceberg ", "parquet"} {
		format, err := ParseDatasetFormat(raw)
		if err != nil {
			t.Fatalf("ParseDatasetFormat(%q) error = %v", raw, err)
		}
		if !format.Valid() {
			t.Fatalf("ParseDatasetFormat(%q) = %q, not valid", raw, format)
		}
	}
	for _, raw := range []string{"", "csv", "ICEBERG2"} {
		if _, err := ParseDatasetFormat(raw); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("ParseDatasetFormat(%q) error = %v, want ErrInvalidArgument", raw, err)
		}
	}
}

func TestParseTransactionType(t *testing.T) {
	transactionType, err := ParseTransactionType("")
	if err != nil || transactionType != TransactionTypeSnapshot {
		t.Fatalf("ParseTransactionType(\"\") = %q, %v", transactionType, err)
	}
	for _, raw := range []string{"snapshot", "append", "update", "delete"} {
		got, err := ParseTransactionType(raw)
		if err != nil || string(got) != raw {
			t.Fatalf("ParseTransactionType(%q) = %q, %v", raw, got, err)
		}
	}
	if _, err := ParseTransactionType("merge"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ParseTransactionType(merge) error = %v, want ErrInvalidArgument", err)
	}
}

func TestTransactionStateMachine(t *testing.T) {
	now := time.Now().UTC()
	transaction := Transaction{ID: "tx-1", Status: TransactionStatusOpen}
	if err := transaction.Commit(now); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if transaction.Status != TransactionStatusCommitted || transaction.CommittedAt == nil {
		t.Fatalf("committed transaction = %+v", transaction)
	}
	if err := transaction.Commit(now.Add(time.Minute)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second Commit() error = %v, want ErrInvalidState", err)
	}
	if err := transaction.Abort(now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Abort() committed error = %v, want ErrInvalidState", err)
	}

	open := Transaction{ID: "tx-2", Status: TransactionStatusOpen}
	if err := open.Abort(now); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if open.Status != TransactionStatusAborted {
		t.Fatalf("aborted status = %q", open.Status)
	}
}

func TestDatasetRID(t *testing.T) {
	const id = "0190a1b2-c3d4-7e5f-8a90-b1c2d3e4f567"
	if got := DatasetRID(id); got != "ri.ontoaxis.main.dataset."+id {
		t.Fatalf("DatasetRID() = %q", got)
	}
}
