package fmtparse

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

// canonValue mirrors gen_cases.py's canon(): a type-tagged JSON value so
// Go's decoder doesn't have to guess whether a number was an int or float.
type canonValue struct {
	Kind      string          `json:"k"`
	Value     json.RawMessage `json:"v"`
	Year      int             `json:"year"`
	Month     int             `json:"month"`
	Day       int             `json:"day"`
	Hour      int             `json:"hour"`
	Minute    int             `json:"minute"`
	Second    int             `json:"second"`
	Micro     int             `json:"micro"`
	TZMinutes *int            `json:"tz_minutes"`
}

type diffCase struct {
	Format        string                 `json:"format"`
	Input         string                 `json:"input"`
	CaseSensitive bool                   `json:"case_sensitive"`
	Op            string                 `json:"op"`
	Match         bool                   `json:"match"`
	Fixed         []*canonValue          `json:"fixed"`
	Named         map[string]*canonValue `json:"named"`
	CompileError  string                 `json:"compile_error"`
	EvalError     string                 `json:"eval_error"`
}

func loadDiffCases(t *testing.T) []diffCase {
	t.Helper()
	data, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatalf("reading testdata/cases.json: %v", err)
	}
	var cases []diffCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decoding testdata/cases.json: %v", err)
	}
	return cases
}

// compareValue checks a Go result value against the Python-derived
// canonical expectation, tolerant of int/float boundary differences that
// don't matter (e.g. a "d" field that happens to hold a whole number).
func compareValue(t *testing.T, path string, got any, want *canonValue) {
	t.Helper()
	if want == nil {
		return
	}
	switch want.Kind {
	case "int":
		var wv int64
		if err := json.Unmarshal(want.Value, &wv); err != nil {
			t.Fatalf("%s: bad expected int: %v", path, err)
		}
		gv, ok := got.(int64)
		if !ok {
			t.Errorf("%s: got %T(%v), want int64(%d)", path, got, got, wv)
			return
		}
		if gv != wv {
			t.Errorf("%s: got %d, want %d", path, gv, wv)
		}
	case "float":
		var raw any
		if err := json.Unmarshal(want.Value, &raw); err != nil {
			t.Fatalf("%s: bad expected float: %v", path, err)
		}
		gv, ok := got.(float64)
		if !ok {
			t.Errorf("%s: got %T(%v), want float64", path, got, got)
			return
		}
		switch rv := raw.(type) {
		case string:
			switch rv {
			case "nan":
				if !math.IsNaN(gv) {
					t.Errorf("%s: got %v, want NaN", path, gv)
				}
			case "inf":
				if gv != math.Inf(1) {
					t.Errorf("%s: got %v, want +Inf", path, gv)
				}
			case "-inf":
				if gv != math.Inf(-1) {
					t.Errorf("%s: got %v, want -Inf", path, gv)
				}
			}
		case float64:
			if math.Abs(gv-rv) > 1e-9*math.Max(1, math.Abs(rv)) {
				t.Errorf("%s: got %v, want %v", path, gv, rv)
			}
		}
	case "str":
		var wv string
		if err := json.Unmarshal(want.Value, &wv); err != nil {
			t.Fatalf("%s: bad expected string: %v", path, err)
		}
		gv, ok := got.(string)
		if !ok {
			t.Errorf("%s: got %T(%v), want string(%q)", path, got, got, wv)
			return
		}
		if gv != wv {
			t.Errorf("%s: got %q, want %q", path, gv, wv)
		}
	case "datetime", "time":
		gt, ok := got.(time.Time)
		if !ok {
			t.Errorf("%s: got %T(%v), want time.Time", path, got, got)
			return
		}
		if want.Kind == "datetime" {
			y, m, d := gt.Date()
			if y != want.Year || int(m) != want.Month || d != want.Day {
				t.Errorf("%s: date got %04d-%02d-%02d, want %04d-%02d-%02d", path, y, m, d, want.Year, want.Month, want.Day)
			}
		}
		if gt.Hour() != want.Hour || gt.Minute() != want.Minute || gt.Second() != want.Second {
			t.Errorf("%s: time got %02d:%02d:%02d, want %02d:%02d:%02d", path, gt.Hour(), gt.Minute(), gt.Second(), want.Hour, want.Minute, want.Second)
		}
		gotMicro := gt.Nanosecond() / 1000
		if gotMicro != want.Micro {
			t.Errorf("%s: microsecond got %d, want %d", path, gotMicro, want.Micro)
		}
		if want.TZMinutes != nil {
			_, offSec := gt.Zone()
			gotMin := offSec / 60
			if gotMin != *want.TZMinutes {
				t.Errorf("%s: tz offset got %d min, want %d min", path, gotMin, *want.TZMinutes)
			}
		}
	case "map":
		var wv map[string]*canonValue
		if err := json.Unmarshal(want.Value, &wv); err != nil {
			t.Fatalf("%s: bad expected map: %v", path, err)
		}
		gv, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: got %T(%v), want map[string]any", path, got, got)
			return
		}
		for k, sub := range wv {
			compareValue(t, path+"."+k, gv[k], sub)
		}
	default:
		t.Errorf("%s: unhandled expected kind %q", path, want.Kind)
	}
}

// TestDifferentialAgainstPython replays testdata/cases.json — real output
// captured from Python's `parse` library (r1chardj0n3s/parse) via
// scratchpad/fmtparse-work/gen_cases.py — against fmtparse, and requires
// match/no-match and every field value to agree.
func TestDifferentialAgainstPython(t *testing.T) {
	cases := loadDiffCases(t)
	if len(cases) < 150 {
		t.Fatalf("expected at least 150 differential cases, got %d", len(cases))
	}

	var passed, failed int
	for i, c := range cases {
		c := c
		t.Run(caseName(i, c), func(t *testing.T) {
			var opts []Option
			if c.CaseSensitive {
				opts = append(opts, CaseSensitive())
			}
			p, err := Compile(c.Format, opts...)
			if err != nil {
				t.Fatalf("Compile(%q) failed: %v", c.Format, err)
			}

			var res *Result
			var perr error
			if c.Op == "search" {
				res, perr = p.Search(c.Input)
			} else {
				res, perr = p.Parse(c.Input)
			}

			if !c.Match {
				if perr == nil && res != nil {
					t.Errorf("expected no match for format %q input %q, got %+v", c.Format, c.Input, res)
					return
				}
				passed++
				return
			}

			if perr != nil {
				t.Errorf("expected a match for format %q input %q, got error: %v", c.Format, c.Input, perr)
				return
			}
			if res == nil {
				t.Errorf("expected a match for format %q input %q, got no match", c.Format, c.Input)
				return
			}

			if len(res.Fixed) != len(c.Fixed) {
				t.Errorf("fixed field count: got %d, want %d", len(res.Fixed), len(c.Fixed))
			} else {
				for i, want := range c.Fixed {
					compareValue(t, "fixed["+itoa(i)+"]", res.Fixed[i], want)
				}
			}

			for name, want := range c.Named {
				got, ok := res.Named[name]
				if !ok {
					t.Errorf("named[%s]: missing from result (got keys %v)", name, keysOf(res.Named))
					continue
				}
				compareValue(t, "named."+name, got, want)
			}
			if len(res.Named) != len(c.Named) {
				t.Errorf("named field count: got %d (%v), want %d (%v)", len(res.Named), keysOf(res.Named), len(c.Named), keysOfWant(c.Named))
			}

			passed++
		})
		_ = failed
	}
	t.Logf("differential cases: %d total", len(cases))
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfWant(m map[string]*canonValue) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func caseName(i int, c diffCase) string {
	return itoa(i) + "_" + sanitize(c.Format)
}

func sanitize(s string) string {
	b := []byte(s)
	out := make([]byte, 0, len(b))
	for _, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "empty"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return string(out)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
