package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("query not found")
	ErrInvalidInput = errors.New("invalid query input")
	ErrConflict     = errors.New("query conflict")
	ErrUnavailable  = errors.New("query backend unavailable")
)

// QueryResult is the normalized tabular result returned by the query engine.
type QueryResult struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	RowCount int64    `json:"row_count"`
}

// SavedQuery is a tenant-scoped named SQL statement.
type SavedQuery struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	SQL       string    `json:"sql"`
	OwnerID   uuid.UUID `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValidateSQL accepts read-only statements only. Stage 1 deliberately rejects
// DDL/DML so a query page cannot mutate platform state.
func ValidateSQL(sql string) error {
	trimmed := trimSQL(sql)
	if trimmed == "" {
		return ErrInvalidInput
	}
	verb := firstWord(trimmed)
	switch verb {
	case "SELECT", "WITH", "SHOW", "DESCRIBE", "EXPLAIN", "VALUES":
		return nil
	default:
		return ErrInvalidInput
	}
}

func trimSQL(sql string) string {
	start, end := 0, len(sql)
	for start < end {
		switch sql[start] {
		case ' ', '\n', '\r', '\t', ';':
			start++
		default:
			goto leftDone
		}
	}
leftDone:
	for end > start {
		switch sql[end-1] {
		case ' ', '\n', '\r', '\t', ';':
			end--
		default:
			goto rightDone
		}
	}
rightDone:
	return sql[start:end]
}

func firstWord(sql string) string {
	for i := 0; i < len(sql); i++ {
		switch sql[i] {
		case ' ', '\n', '\r', '\t', '(':
			return upper(sql[:i])
		}
	}
	return upper(sql)
}

func upper(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
