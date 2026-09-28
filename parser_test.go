package fmtparse

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseBasic(t *testing.T) {
	tests := []struct {
		format, input string
		wantFixed     []any
		wantNamed     map[string]any
	}{
		{"{}", "hello", []any{"hello"}, map[string]any{}},
		{"{} {}", "hello world", []any{"hello", "world"}, map[string]any{}},
		{"{name}", "Alice", nil, map[string]any{"name": "Alice"}},
		{"{name} scored {score:d}", "Bob scored 7", nil, map[string]any{"name": "Bob", "score": int64(7)}},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			r, err := Parse(tt.format, tt.input)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if r == nil {
				t.Fatalf("expected a match")
			}
			if tt.wantFixed != nil {
				if len(r.Fixed) != len(tt.wantFixed) {
					t.Fatalf("fixed len = %d, want %d", len(r.Fixed), len(tt.wantFixed))
				}
				for i := range tt.wantFixed {
					if r.Fixed[i] != tt.wantFixed[i] {
						t.Errorf("fixed[%d] = %v, want %v", i, r.Fixed[i], tt.wantFixed[i])
					}
				}
			}
			for k, v := range tt.wantNamed {
				if r.Named[k] != v {
					t.Errorf("named[%s] = %v, want %v", k, r.Named[k], v)
				}
			}
		})
	}
}

func TestParseNoMatch(t *testing.T) {
	r, err := Parse("{} scored {:d} points", "not a matching string")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if r != nil {
		t.Fatalf("expected nil result, got %+v", r)
	}
}

func TestSearchFindsFirstOccurrence(t *testing.T) {
	r, err := Search("<{}>", "before <inside> after")
	if err != nil || r == nil {
		t.Fatalf("Search failed: %v %v", r, err)
	}
	if got, _ := r.Get(""); got != nil {
		t.Fatalf("unexpected named field: %v", got)
	}
	if len(r.Fixed) != 1 || r.Fixed[0] != "inside" {
		t.Fatalf("Fixed = %v, want [inside]", r.Fixed)
	}
	wantSpan := [2]int{8, 14}
	if r.Spans["0"] != wantSpan {
		t.Errorf("span = %v, want %v", r.Spans["0"], wantSpan)
	}
}

func TestFindAllIteratesEveryMatch(t *testing.T) {
	p, err := Compile("{:d}")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var got []int64
	for r := range p.FindAll("1 and 22 and 333") {
		v, err := strconv.ParseInt(fmt.Sprint(r.Fixed[0]), 10, 64)
		if err != nil {
			t.Fatalf("unexpected fixed value %v: %v", r.Fixed[0], err)
		}
		got = append(got, v)
	}
	want := []int64{1, 22, 333}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFindAllStopsEarlyWhenYieldReturnsFalse(t *testing.T) {
	p, err := Compile("{:d}")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	count := 0
	for range p.FindAll("1 2 3 4 5") {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2 (iteration should have stopped)", count)
	}
}

func TestFindAllPackageLevel(t *testing.T) {
	seq, err := FindAll("{:d}", "1 2 3")
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	n := 0
	for range seq {
		n++
	}
	if n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}
}

func TestWithTypeCustomConverter(t *testing.T) {
	upper := WithType("upper", `[A-Z]+`, func(s string) (any, error) {
		return strings.ToLower(s), nil
	})
	r, err := Parse("code: {code:upper}", "code: ABC", upper)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r == nil {
		t.Fatalf("expected match")
	}
	if r.Named["code"] != "abc" {
		t.Errorf("code = %v, want abc", r.Named["code"])
	}

	// Custom patterns are subject to the same case-insensitive matching as
	// everything else by default (matching Python's with_pattern behaviour):
	// lowercase input still matches [A-Z]+ here.
	r2, err := Parse("code: {code:upper}", "code: abc", upper)
	if err != nil || r2 == nil {
		t.Fatalf("expected a case-insensitive match, got %+v, %v", r2, err)
	}

	// With CaseSensitive(), the custom pattern's case now matters.
	r3, err := Parse("code: {code:upper}", "code: abc", upper, CaseSensitive())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r3 != nil {
		t.Fatalf("expected no match under CaseSensitive(), got %+v", r3)
	}
}

func TestWithTypeConverterError(t *testing.T) {
	boom := WithType("boom", `\d+`, func(s string) (any, error) {
		return nil, errors.New("deliberate failure")
	})
	_, err := Parse("{n:boom}", "42", boom)
	if err == nil {
		t.Fatalf("expected an error from the custom converter")
	}
}

func TestCaseSensitiveOption(t *testing.T) {
	r, err := Parse("HELLO {}", "hello world")
	if err != nil || r == nil {
		t.Fatalf("case-insensitive default should match: %v %v", r, err)
	}

	r2, err := Parse("HELLO {}", "hello world", CaseSensitive())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r2 != nil {
		t.Fatalf("case-sensitive mode should not match differently-cased literal text")
	}

	r3, err := Parse("HELLO {}", "HELLO world", CaseSensitive())
	if err != nil || r3 == nil {
		t.Fatalf("case-sensitive mode should match identically-cased text: %v %v", r3, err)
	}
}

func TestRepeatedFieldNameDifferentTypeIsAnError(t *testing.T) {
	_, err := Compile("{a:d} {a:w}")
	if err == nil {
		t.Fatalf("expected an error for a repeated field name with a different type")
	}
	var target *RepeatedFieldError
	if !errors.As(err, &target) {
		t.Errorf("expected a *RepeatedFieldError, got %T: %v", err, err)
	}
}

func TestInvalidFormatSpecIsAnError(t *testing.T) {
	_, err := Compile("{:zzz}")
	if err == nil {
		t.Fatalf("expected an error for an unrecognised type")
	}
}

func TestDottedNameStaysFlatBracketNameNests(t *testing.T) {
	// Verified against upstream: {a.b} keeps the literal dotted key "a.b";
	// only {a[b]} nests into a map. See expandNamedFields's doc comment.
	r, err := Parse("{a.b} {a[c]}", "1 2")
	if err != nil || r == nil {
		t.Fatalf("Parse: %v %v", r, err)
	}
	if r.Named["a.b"] != "1" {
		t.Errorf(`Named["a.b"] = %v, want "1"`, r.Named["a.b"])
	}
	nested, ok := r.Named["a"].(map[string]any)
	if !ok {
		t.Fatalf(`Named["a"] is %T, want map[string]any`, r.Named["a"])
	}
	if nested["c"] != "2" {
		t.Errorf(`Named["a"]["c"] = %v, want "2"`, nested["c"])
	}
}

func TestRepeatedFieldMustCaptureEqualText(t *testing.T) {
	r, err := Parse("{a}-{a}", "x-x")
	if err != nil || r == nil {
		t.Fatalf("expected equal repeats to match: %v %v", r, err)
	}
	if r.Named["a"] != "x" {
		t.Errorf("a = %v, want x", r.Named["a"])
	}

	r2, err := Parse("{a}-{a}", "x-y")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r2 != nil {
		t.Fatalf("expected no match when repeated field captures differ, got %+v", r2)
	}
}

func TestResultTypedGetters(t *testing.T) {
	r, err := Parse("{name} is {age:d} and {gpa:f}", "Sam is 30 and 3.5")
	if err != nil || r == nil {
		t.Fatalf("Parse: %v %v", r, err)
	}

	name, err := r.String("name")
	if err != nil || name != "Sam" {
		t.Errorf("String(name) = %q, %v", name, err)
	}
	age, err := r.Int("age")
	if err != nil || age != 30 {
		t.Errorf("Int(age) = %d, %v", age, err)
	}
	gpa, err := r.Float("gpa")
	if err != nil || gpa != 3.5 {
		t.Errorf("Float(gpa) = %v, %v", gpa, err)
	}

	if _, err := r.Int("name"); err == nil {
		t.Errorf("Int(name) should fail: name is a string, not an int")
	}
	if _, err := r.String("missing"); err == nil {
		t.Errorf("String(missing) should fail: no such field")
	}
}

func TestResultTime(t *testing.T) {
	r, err := Parse("{when:ti}", "2013-01-01T12:30:45Z")
	if err != nil || r == nil {
		t.Fatalf("Parse: %v %v", r, err)
	}
	when, err := r.Time("when")
	if err != nil {
		t.Fatalf("Time: %v", err)
	}
	want := time.Date(2013, 1, 1, 12, 30, 45, 0, time.UTC)
	if !when.Equal(want) {
		t.Errorf("when = %v, want %v", when, want)
	}
}

func TestParserFormatAccessor(t *testing.T) {
	p, err := Compile("{}: {}")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if p.Format() != "{}: {}" {
		t.Errorf("Format() = %q", p.Format())
	}
}

func TestCompileErrorPropagatesFromPackageFuncs(t *testing.T) {
	if _, err := Parse("{:zzz}", "x"); err == nil {
		t.Errorf("Parse should surface a compile error")
	}
	if _, err := Search("{:zzz}", "x"); err == nil {
		t.Errorf("Search should surface a compile error")
	}
	if _, err := FindAll("{:zzz}", "x"); err == nil {
		t.Errorf("FindAll should surface a compile error")
	}
}

// TestLowercaseMonthNameReturnsErrorNotPanic exercises a real upstream
// defect: Python's date_convert looks month names up in a dict keyed by
// capitalized names, so a case-insensitively-matched lowercase month name
// (e.g. "nov") raises an uncaught KeyError out of parse.parse() itself,
// confirmed against the installed library (see
// scratchpad/fmtparse-work/gen_cases.py's comment on this). fmtparse
// reaches the same "unknown month" situation but returns it as a normal
// Go error instead of crashing the caller.
func TestLowercaseMonthNameReturnsErrorNotPanic(t *testing.T) {
	_, err := Parse("{:tg}", "21-nov-2016")
	if err == nil {
		t.Fatalf("expected an error for a lowercase month name, matching upstream's own failure on this input")
	}
}

func TestReusedParserAcrossManyStrings(t *testing.T) {
	p, err := Compile("{name},{score:d}")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	inputs := []string{"a,1", "b,2", "c,3"}
	for i, in := range inputs {
		r, err := p.Parse(in)
		if err != nil || r == nil {
			t.Fatalf("Parse(%q): %v %v", in, r, err)
		}
		if r.Named["score"] != int64(i+1) {
			t.Errorf("score = %v, want %d", r.Named["score"], i+1)
		}
	}
}
