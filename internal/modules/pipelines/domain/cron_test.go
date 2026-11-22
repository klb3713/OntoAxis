package domain_test

import (
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

func TestParseCronAcceptsStandardExpressions(t *testing.T) {
	expressions := []string{
		"* * * * *",
		"0 * * * *",
		"*/15 9-17 * * MON-FRI",
		"0 0 1 JAN *",
		"5,35 0-23/2 * * *",
		"0 0 * * 7",
		"0 0 ? * MON",
	}
	for _, expression := range expressions {
		if _, err := domain.ParseCron(expression); err != nil {
			t.Fatalf("ParseCron(%q) returned error: %v", expression, err)
		}
	}
}

func TestParseCronRejectsInvalidExpressions(t *testing.T) {
	expressions := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * * 8",
		"*/0 * * * *",
		"5-1 * * * *",
		"MON * * * *",
		"* * ? * ?",
	}
	for _, expression := range expressions {
		if _, err := domain.ParseCron(expression); err == nil {
			t.Fatalf("ParseCron(%q) unexpectedly succeeded", expression)
		}
	}
}

func TestCronNextMinute(t *testing.T) {
	schedule, err := domain.ParseCron("0 * * * *")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	after := time.Date(2026, 1, 2, 10, 30, 15, 0, time.UTC)
	next, err := schedule.Next(after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Next = %s, want %s", next, want)
	}
}

func TestCronSundaySevenNormalizesToSunday(t *testing.T) {
	schedule, err := domain.ParseCron("0 0 * * 7")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // Thursday
	next, err := schedule.Next(after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if next.Weekday() != time.Sunday {
		t.Fatalf("Next weekday = %s, want Sunday", next.Weekday())
	}
	want := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Next = %s, want %s", next, want)
	}
}

func TestCronDayOfMonthAndDayOfWeekUseOR(t *testing.T) {
	schedule, err := domain.ParseCron("0 0 1 * MON")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	after := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	next, err := schedule.Next(after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	// 2026-01-01 is Thursday, so the next match is Monday 2026-01-05 rather
	// than waiting for 2026-02-01.
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Next = %s, want %s", next, want)
	}
}

func TestComputeNextRunUsesTimezone(t *testing.T) {
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, err := domain.ComputeNextRun("0 9 * * *", "Asia/Shanghai", after)
	if err != nil {
		t.Fatalf("ComputeNextRun: %v", err)
	}
	want := time.Date(2026, 1, 1, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
	if next.Location().String() != "Asia/Shanghai" {
		t.Fatalf("location = %q, want Asia/Shanghai", next.Location())
	}
}

func TestCronNextLeapDayWithinEightYears(t *testing.T) {
	schedule, err := domain.ParseCron("0 0 29 2 *")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	next, err := schedule.Next(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Next = %s, want %s", next, want)
	}
}

func TestComputeNextRunRejectsInvalidTimezone(t *testing.T) {
	if _, err := domain.ComputeNextRun("* * * * *", "Mars/Olympus", time.Now()); err == nil {
		t.Fatal("expected invalid timezone error")
	}
}
