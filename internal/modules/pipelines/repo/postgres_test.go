package repo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

// fakeRow implements rowScanner so scanRun's column contract can be tested
// without a database or a mocking dependency.
type fakeRow struct {
	values []any
	err    error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan destination count %d does not match column count %d", len(dest), len(r.values))
	}
	for index, target := range dest {
		dst := reflect.ValueOf(target)
		if dst.Kind() != reflect.Pointer || dst.IsNil() {
			return fmt.Errorf("scan destination %d is not a non-nil pointer", index)
		}
		dst = dst.Elem()
		value := r.values[index]
		if value == nil {
			dst.Set(reflect.Zero(dst.Type()))
			continue
		}
		source := reflect.ValueOf(value)
		if source.Type() != dst.Type() {
			if source.Kind() == reflect.String && dst.Kind() == reflect.String {
				source = source.Convert(dst.Type())
			} else {
				return fmt.Errorf("column %d: cannot assign %T to %s", index, value, dst.Type())
			}
		}
		dst.Set(source)
	}
	return nil
}

func TestScanRunReadsEveryRunColumn(t *testing.T) {
	started := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	finished := started.Add(5 * time.Minute)
	reconciled := finished.Add(time.Minute)
	created := started.Add(-time.Minute)
	updated := finished.Add(time.Minute)
	row := &fakeRow{values: []any{
		"run-1", "tenant-1", "pipeline-1", 4, "running", "manual",
		"exec-123", "invalid_sql_reference", 3, &reconciled,
		&started, &finished, "boom", created, updated,
	}}
	run, err := scanRun(row)
	if err != nil {
		t.Fatalf("scanRun: %v", err)
	}
	if run.ID != "run-1" || run.TenantID != "tenant-1" || run.PipelineID != "pipeline-1" || run.Version != 4 {
		t.Fatalf("identity columns not scanned: %+v", run)
	}
	if run.Status != domain.PipelineRunStatusRunning || run.TriggerType != domain.TriggerTypeManual {
		t.Fatalf("status columns not scanned: %+v", run)
	}
	if run.ExecutionID != "exec-123" || run.FailureCode != "invalid_sql_reference" || run.AttemptCount != 3 {
		t.Fatalf("execution columns not scanned: %+v", run)
	}
	if run.LastReconciledAt == nil || !run.LastReconciledAt.Equal(reconciled) {
		t.Fatalf("last_reconciled_at not scanned: %v", run.LastReconciledAt)
	}
	if run.StartedAt == nil || !run.StartedAt.Equal(started) || run.FinishedAt == nil || !run.FinishedAt.Equal(finished) {
		t.Fatalf("run timestamps not scanned: %v / %v", run.StartedAt, run.FinishedAt)
	}
	if run.Error != "boom" || !run.CreatedAt.Equal(created) || !run.UpdatedAt.Equal(updated) {
		t.Fatalf("remaining columns not scanned: %+v", run)
	}
}

func TestScanRunNormalisesNullableExecutionFields(t *testing.T) {
	// 00002 made execution_id/failure_code nullable; the SELECT uses
	// COALESCE(..., '') so nil never reaches the string dests. A nil
	// last_reconciled_at stays nil.
	row := &fakeRow{values: []any{
		"run-2", "tenant-1", "pipeline-1", 1, "queued", "manual",
		"", "", 0, nil, nil, nil, "", time.Now(), time.Now(),
	}}
	run, err := scanRun(row)
	if err != nil {
		t.Fatalf("scanRun: %v", err)
	}
	if run.ExecutionID != "" || run.FailureCode != "" || run.AttemptCount != 0 {
		t.Fatalf("nullable execution fields = %+v", run)
	}
	if run.LastReconciledAt != nil || run.StartedAt != nil || run.FinishedAt != nil {
		t.Fatalf("nil timestamps must stay nil: %+v", run)
	}
}

func TestScanRunPropagatesScanErrors(t *testing.T) {
	if _, err := scanRun(&fakeRow{err: pgx.ErrNoRows}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("scanRun error = %v, want pgx.ErrNoRows", err)
	}
}

func TestRunSelectAndInsertColumnsAgree(t *testing.T) {
	selected := splitTopLevelColumns(runColumns)
	if len(selected) != 15 {
		t.Fatalf("runColumns has %d columns, want 15: %v", len(selected), selected)
	}

	open := strings.Index(createRunSQL, "(")
	close := strings.Index(createRunSQL[open:], ")")
	if open < 0 || close < 0 {
		t.Fatalf("cannot parse INSERT column list: %q", createRunSQL)
	}
	inserted := splitTopLevelColumns(createRunSQL[open+1 : open+close])
	if len(inserted) != 15 {
		t.Fatalf("createRunSQL inserts %d columns, want 15: %v", len(inserted), inserted)
	}
	for index := range selected {
		want := baseColumnName(selected[index])
		if inserted[index] != want {
			t.Fatalf("column %d mismatch: SELECT %q vs INSERT %q (full SELECT: %v)", index, want, inserted[index], selected)
		}
	}
}

func TestCreateRunSQLBindsEveryColumn(t *testing.T) {
	assertPlaceholders(t, createRunSQL, 15)
}

func TestUpdateRunConditionalSQLIsGuardedAndMonotonic(t *testing.T) {
	for _, fragment := range []string{
		"UPDATE pipeline.runs",
		"WHERE tenant_id = $1::uuid AND id = $2::uuid",
		"status IN ('queued', 'running')",
		"RETURNING " + runColumns,
		"execution_id = COALESCE(NULLIF($4, ''), execution_id)",
		"failure_code = COALESCE(NULLIF($5, ''), failure_code)",
		"attempt_count = GREATEST(attempt_count, $6)",
		"last_reconciled_at = COALESCE($7, last_reconciled_at)",
		"started_at = COALESCE(started_at, $8)",
		"finished_at = COALESCE(finished_at, $9)",
		"error = COALESCE(NULLIF($10, ''), error)",
	} {
		if !strings.Contains(updateRunConditionalSQL, fragment) {
			t.Errorf("updateRunConditionalSQL missing %q", fragment)
		}
	}
	assertPlaceholders(t, updateRunConditionalSQL, 11)
	// The CAS must not overwrite started_at/finished_at unconditionally: a
	// replayed reconciliation cannot move timestamps once set.
	if strings.Contains(updateRunConditionalSQL, "started_at = $8") || strings.Contains(updateRunConditionalSQL, "finished_at = $9") {
		t.Error("started_at/finished_at must merge with COALESCE so terminal timestamps are write-once")
	}
}

func TestRunLineageSQLIsIdempotent(t *testing.T) {
	assertPlaceholders(t, insertRunInputSQL, 4)
	for _, fragment := range []string{
		"INSERT INTO pipeline.run_inputs",
		"ON CONFLICT (run_id, dataset_id) DO NOTHING",
	} {
		if !strings.Contains(insertRunInputSQL, fragment) {
			t.Errorf("insertRunInputSQL missing %q", fragment)
		}
	}

	assertPlaceholders(t, insertRunOutputSQL, 8)
	for _, fragment := range []string{
		"INSERT INTO pipeline.run_outputs",
		"relation_catalog", "relation_schema", "relation_table",
		"ON CONFLICT (run_id, dataset_id) DO NOTHING",
	} {
		if !strings.Contains(insertRunOutputSQL, fragment) {
			t.Errorf("insertRunOutputSQL missing %q", fragment)
		}
	}
}

func TestMapPostgresError(t *testing.T) {
	noRows := fmt.Errorf("scan: %w", pgx.ErrNoRows)
	tests := []struct {
		name   string
		err    error
		target error
	}{
		{name: "nil", err: nil, target: nil},
		{name: "no rows", err: noRows, target: domain.ErrNotFound},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505", Message: "duplicate"}, target: domain.ErrAlreadyExists},
		{name: "foreign key violation", err: &pgconn.PgError{Code: "23503", Message: "fk"}, target: domain.ErrInvalidArgument},
		{name: "check violation", err: &pgconn.PgError{Code: "23514", Message: "check"}, target: domain.ErrInvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapPostgresError(test.err)
			if test.target == nil {
				if mapped != nil {
					t.Fatalf("mapPostgresError(nil) = %v", mapped)
				}
				return
			}
			if !errors.Is(mapped, test.target) {
				t.Fatalf("mapPostgresError(%v) = %v, want %v", test.err, mapped, test.target)
			}
		})
	}

	plain := errors.New("connection reset")
	if mapped := mapPostgresError(plain); !errors.Is(mapped, plain) {
		t.Fatalf("unknown errors must be preserved, got %v", mapped)
	}
}

func TestPostgresStoreNilPoolFailsClosed(t *testing.T) {
	if err := NewPostgresStore(nil).Ping(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("nil pool Ping = %v, want ErrUnavailable", err)
	}
}

func splitTopLevelColumns(expression string) []string {
	var columns []string
	var current strings.Builder
	depth := 0
	var quote byte
	for i := 0; i < len(expression); i++ {
		ch := expression[i]
		if quote != 0 {
			current.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'':
			quote = ch
			current.WriteByte(ch)
		case '(':
			depth++
			current.WriteByte(ch)
		case ')':
			depth--
			current.WriteByte(ch)
		case ',':
			if depth == 0 {
				columns = append(columns, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			current.WriteByte(ch)
		default:
			current.WriteByte(ch)
		}
	}
	if remaining := strings.TrimSpace(current.String()); remaining != "" {
		columns = append(columns, remaining)
	}
	return columns
}

func baseColumnName(expression string) string {
	trimmed := strings.TrimSpace(expression)
	if cast := strings.LastIndex(trimmed, "::"); cast >= 0 {
		trimmed = strings.TrimSpace(trimmed[:cast])
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "COALESCE(") {
		return trimmed
	}
	inner := trimmed[len("COALESCE("):]
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return strings.TrimSpace(inner[:i])
			}
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[:i])
			}
		}
	}
	return strings.TrimSpace(inner)
}

func assertPlaceholders(t *testing.T, statement string, want int) {
	t.Helper()
	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(statement, -1)
	seen := make(map[int]bool, len(matches))
	for _, match := range matches {
		number, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("bad placeholder %q: %v", match[1], err)
		}
		seen[number] = true
	}
	for number := 1; number <= want; number++ {
		if !seen[number] {
			t.Errorf("statement is missing placeholder $%d", number)
		}
	}
	if len(seen) != want {
		t.Errorf("statement has %d distinct placeholders, want %d", len(seen), want)
	}
}
