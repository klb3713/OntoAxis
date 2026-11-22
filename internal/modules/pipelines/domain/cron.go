package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule is a parsed five-field cron expression. Supported syntax:
// numbers, names (JAN-DEC, SUN-SAT), ranges (a-b), steps (*/n, a-b/n, a/n)
// and comma-separated lists. Sunday may be 0 or 7.
type CronSchedule struct {
	minute uint64
	hour   uint64
	dom    uint64
	month  uint64
	dow    uint64
	domAny bool
	dowAny bool
}

var monthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var dayNames = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
}

// ParseCron parses a five-field cron expression. Six- or seven-field Quartz
// expressions are rejected because this module only promises standard cron.
func ParseCron(expression string) (*CronSchedule, error) {
	fields := strings.Fields(strings.TrimSpace(expression))
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: cron must have exactly five fields", ErrInvalidArgument)
	}
	if fields[2] == "?" && fields[4] == "?" {
		return nil, fmt.Errorf("%w: cron cannot use '?' for both day fields", ErrInvalidArgument)
	}
	minute, _, err := parseCronField(fields[0], 0, 59, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron minute: %v", ErrInvalidArgument, err)
	}
	hour, _, err := parseCronField(fields[1], 0, 23, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron hour: %v", ErrInvalidArgument, err)
	}
	dom, domAny, err := parseCronField(fields[2], 1, 31, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron day of month: %v", ErrInvalidArgument, err)
	}
	month, _, err := parseCronField(fields[3], 1, 12, monthNames)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron month: %v", ErrInvalidArgument, err)
	}
	dow, dowAny, err := parseCronField(fields[4], 0, 7, dayNames)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron day of week: %v", ErrInvalidArgument, err)
	}
	// Normalize Sunday 7 to 0.
	if dow&(1<<7) != 0 {
		dow = (dow &^ (1 << 7)) | 1<<0
	}
	return &CronSchedule{
		minute: minute,
		hour:   hour,
		dom:    dom,
		month:  month,
		dow:    dow,
		domAny: domAny,
		dowAny: dowAny,
	}, nil
}

// Next returns the first matching minute strictly after the provided instant.
func (c *CronSchedule) Next(after time.Time, loc *time.Location) (time.Time, error) {
	if c == nil {
		return time.Time{}, fmt.Errorf("%w: cron schedule is nil", ErrInvalidArgument)
	}
	if loc == nil {
		loc = time.UTC
	}
	start := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	// Eight years covers the longest Gregorian gap between leap days
	// (2096-02-29 to 2104-02-29), so valid schedules such as "0 0 29 2 *"
	// are never rejected for having no nearby occurrence.
	limit := start.AddDate(8, 0, 0)
	for candidate := start; !candidate.After(limit); candidate = candidate.Add(time.Minute) {
		if c.matches(candidate.In(loc)) {
			return candidate.In(loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: cron has no matching time within eight years", ErrInvalidArgument)
}

func (c *CronSchedule) matches(t time.Time) bool {
	if c.minute&(1<<uint(t.Minute())) == 0 {
		return false
	}
	if c.hour&(1<<uint(t.Hour())) == 0 {
		return false
	}
	if c.month&(1<<uint(int(t.Month()))) == 0 {
		return false
	}
	domMatch := c.dom&(1<<uint(t.Day())) != 0
	dowMatch := c.dow&(1<<uint(int(t.Weekday()))) != 0
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dowMatch
	case c.dowAny:
		return domMatch
	default:
		// Standard Vixie behavior: when both are restricted, either may match.
		return domMatch || dowMatch
	}
}

func parseCronField(raw string, minValue, maxValue int, names map[string]int) (uint64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, fmt.Errorf("empty field")
	}
	if raw == "?" && (maxValue == 31 || maxValue == 7) {
		return fieldMask(minValue, maxValue, 1), true, nil
	}
	var bits uint64
	anyValue := raw == "*" || strings.HasPrefix(raw, "*/")
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0, false, fmt.Errorf("empty list item")
		}
		rangePart := part
		step := 1
		if slash := strings.IndexByte(part, '/'); slash >= 0 {
			rangePart = part[:slash]
			parsedStep, err := strconv.Atoi(part[slash+1:])
			if err != nil || parsedStep <= 0 {
				return 0, false, fmt.Errorf("invalid step %q", part[slash+1:])
			}
			step = parsedStep
		}
		start, end := minValue, maxValue
		if rangePart != "*" {
			if dash := strings.IndexByte(rangePart, '-'); dash >= 0 {
				var err error
				start, err = parseCronValue(rangePart[:dash], names)
				if err != nil {
					return 0, false, err
				}
				end, err = parseCronValue(rangePart[dash+1:], names)
				if err != nil {
					return 0, false, err
				}
			} else {
				var err error
				start, err = parseCronValue(rangePart, names)
				if err != nil {
					return 0, false, err
				}
				if strings.IndexByte(part, '/') >= 0 {
					end = maxValue
				} else {
					end = start
				}
			}
		}
		if start < minValue || start > maxValue || end < minValue || end > maxValue {
			return 0, false, fmt.Errorf("value out of range %d-%d", minValue, maxValue)
		}
		if start > end {
			return 0, false, fmt.Errorf("range start %d exceeds end %d", start, end)
		}
		for value := start; value <= end; value += step {
			bits |= 1 << uint(value)
		}
	}
	if bits == 0 {
		return 0, false, fmt.Errorf("field matches no values")
	}
	return bits, anyValue, nil
}

func parseCronValue(raw string, names map[string]int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty value")
	}
	if names != nil {
		if value, ok := names[strings.ToUpper(raw)]; ok {
			return value, nil
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", raw)
	}
	return value, nil
}

func fieldMask(minValue, maxValue, step int) uint64 {
	var bits uint64
	for value := minValue; value <= maxValue; value += step {
		bits |= 1 << uint(value)
	}
	return bits
}

// ComputeNextRun validates the timezone and expression and returns the next
// scheduled occurrence strictly after the given instant.
func ComputeNextRun(expression, timezone string, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid timezone %q", ErrInvalidArgument, timezone)
	}
	schedule, err := ParseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(after, loc)
}
