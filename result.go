package fmtparse

import (
	"fmt"
	"time"
)

// Result holds the fields extracted by [Parse], [Search], or [FindAll].
//
// Fixed holds anonymous ({}) and positional field values in the order they
// appear in the format string. Named holds {name} field values, keyed by
// their top-level name; a dotted or indexed field ({a.b} or {a[b]}) produces
// a nested map[string]any under Named["a"].
type Result struct {
	// Fixed holds values for every anonymous ({}) field, in left-to-right order.
	Fixed []any
	// Named holds values for every named ({name}) field. Dotted and indexed
	// names ({a.b}, {a[b]}) are expanded into nested map[string]any values.
	Named map[string]any
	// Spans records the [start, end) byte offsets in the matched string for
	// each field. Named fields are keyed by their full flat name (e.g. "a.b"
	// or "a[b]", not expanded). Fixed fields are keyed by their decimal
	// index ("0", "1", ...).
	Spans map[string][2]int
}

func newResult() *Result {
	return &Result{
		Fixed: nil,
		Named: map[string]any{},
		Spans: map[string][2]int{},
	}
}

// Get returns the named field's raw value and whether it was present.
func (r *Result) Get(name string) (any, bool) {
	v, ok := r.Named[name]
	return v, ok
}

// Int returns the named field's value as an int64. It returns an error if
// the field is absent or its underlying value is not an integer.
func (r *Result) Int(name string) (int64, error) {
	v, ok := r.Named[name]
	if !ok {
		return 0, fmt.Errorf("fmtparse: no such field %q", name)
	}
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	}
	return 0, fmt.Errorf("fmtparse: field %q is %T, not an integer", name, v)
}

// Float returns the named field's value as a float64. It returns an error
// if the field is absent or its underlying value is not a float.
func (r *Result) Float(name string) (float64, error) {
	v, ok := r.Named[name]
	if !ok {
		return 0, fmt.Errorf("fmtparse: no such field %q", name)
	}
	switch n := v.(type) {
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case int:
		return float64(n), nil
	}
	return 0, fmt.Errorf("fmtparse: field %q is %T, not a float", name, v)
}

// String returns the named field's value as a string. It returns an error
// if the field is absent. Non-string values are formatted with fmt.Sprint.
func (r *Result) String(name string) (string, error) {
	v, ok := r.Named[name]
	if !ok {
		return "", fmt.Errorf("fmtparse: no such field %q", name)
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	return fmt.Sprint(v), nil
}

// Time returns the named field's value as a time.Time. It returns an error
// if the field is absent or its underlying value is not a time.Time. Fields
// parsed with the "tt" (time-only) type carry the zero date (year 1, month
// January, day 1); every other date/time type carries a full date.
func (r *Result) Time(name string) (time.Time, error) {
	v, ok := r.Named[name]
	if !ok {
		return time.Time{}, fmt.Errorf("fmtparse: no such field %q", name)
	}
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("fmtparse: field %q is %T, not a time.Time", name, v)
}
