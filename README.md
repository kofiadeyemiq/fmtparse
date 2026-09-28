# fmtparse

A Go port of Python's `parse` library: match a string against a format
pattern and pull typed, named values out of it, the reverse of `fmt.Sprintf`.

```go
p, err := fmtparse.Compile("{name} scored {score:d} points")
if err != nil {
    log.Fatal(err)
}
r, err := p.Parse("Alice scored 42 points")
if err != nil {
    log.Fatal(err)
}
fmt.Println(r.Named["name"], r.Named["score"]) // Alice 42
```

## Why

Go's `fmt.Sscanf` only takes positional arguments as compile-time pointers,
and its `%s` verb stops at the first whitespace — it cannot pull a
space-containing field out of a line, and there is no way to get named,
typed results back as a map. `regexp` can do the matching, but you write and
maintain the pattern yourself, and it hands back strings, not `int`s,
`float64`s or `time.Time`s. Nothing on pkg.go.dev is a port of Python's
`parse` (github.com/r1chardj0n3s/parse) — the field-format syntax
(`{name:d}`, alignment, width, the `t*` date types) is unique to it and
existing Go parsing libraries don't replicate it. `fmtparse` does the same
job: describe the shape of the string once, get named and typed fields back.

Precompiling the format once and reusing it, as the usage example above
does, avoids re-parsing the format string on every call:

Measured: `go test -run '^$' -bench . -benchtime=1s -count=3 ./...` (Apple M5 Pro)

```
BenchmarkParsePackageLevel-15    	  106206	     11177 ns/op
BenchmarkParsePrecompiled-15     	 1228341	      1011 ns/op
```

Calling the package-level `Parse` (which compiles the format on every call)
took about 11x longer than reusing a `Compile`d `*Parser`, across three runs.

## Installation

```sh
go get github.com/__GH_OWNER__/fmtparse
```

Requires Go 1.23, because `Parser.FindAll` returns an `iter.Seq[*Result]`
(the `range`-over-func iterator added in that release). Zero runtime
dependencies.

## Usage

### Parse a whole string

```go
r, err := fmtparse.Parse("{year:d}-{month:d}-{day:d}", "2024-03-15")
// r.Named["year"] == int64(2024)
```

`Parse` requires the format to match the entire input, like Python's
`parse.parse()`.

### Find a match anywhere in a string

```go
r, err := fmtparse.Search("user={}", "request from user=alice at 10:02")
// r.Fixed[0] == "alice"
```

### Iterate every match

```go
seq, err := fmtparse.FindAll("{:d}", "found 3 errors in 12 files")
for r := range seq {
    fmt.Println(r.Fixed[0]) // 3, then 12
}
```

### Reuse a compiled format

```go
p, err := fmtparse.Compile("{ip}: {status:d}")
for _, line := range logLines {
    if r, err := p.Parse(line); err == nil && r != nil {
        fmt.Println(r.Named["ip"], r.Named["status"])
    }
}
```

### Typed access

```go
r, _ := fmtparse.Parse("{name} is {age:d}", "Sam is 30")
age, err := r.Int("age")     // int64(30), nil
name, err := r.String("name") // "Sam", nil
```

### Custom field types

```go
hexColor := fmtparse.WithType("hex", `#[0-9a-fA-F]{6}`, func(s string) (any, error) {
    return s, nil
})
r, _ := fmtparse.Parse("color: {c:hex}", "color: #FF00FF", hexColor)
```

### Case-sensitive matching

```go
r, _ := fmtparse.Parse("HELLO {}", "hello world")               // matches (default)
r, _ = fmtparse.Parse("HELLO {}", "hello world", fmtparse.CaseSensitive()) // nil
```

## Reference

### Functions

| Function | Signature |
| --- | --- |
| `Parse` | `func(format, s string, opts ...Option) (*Result, error)` — whole-string match |
| `Search` | `func(format, s string, opts ...Option) (*Result, error)` — first match anywhere |
| `FindAll` | `func(format, s string, opts ...Option) (iter.Seq[*Result], error)` — every match |
| `Compile` | `func(format string, opts ...Option) (*Parser, error)` — reusable compiled format |

`FindAll` carries an error return that its `iter.Seq[*Result]` result type
alone can't: the format must be compiled before an iterator can exist, and
that compilation can fail. `*Parser` has the same three methods
(`Parse`, `Search`, `FindAll`) without the `format` argument or the error
return, since a `Parser` is already known-good.

For every function above, `(nil, nil)` means "no match" — an error is
returned only when the format string itself is invalid, or a matched value
fails a type conversion (a registered custom type's converter, or a `t*`
date type whose captured text can't become a valid date; see Limitations).

### Options

| Option | Effect |
| --- | --- |
| `CaseSensitive()` | Match case-sensitively. Default is case-insensitive, matching Python's `parse` default. |
| `WithType(name, pattern, convert)` | Register `{field:name}` as a custom type. `pattern` is an RE2 regular expression with no capturing groups (use `(?:...)`); `convert` turns the matched text into a value or an error. |

### Field syntax

| Syntax | Meaning |
| --- | --- |
| `{}` | Anonymous field, appended to `Result.Fixed` in order |
| `{name}` | Named field, in `Result.Named["name"]` |
| `{name:type}` | Named field with a type conversion |
| `{a.b}` | Flat named field literally keyed `"a.b"` (see below — not nested) |
| `{a[b]}`, `{a[b][c]}` | Nested field: `Result.Named["a"].(map[string]any)["b"]` |
| `{{`, `}}` | Literal `{` and `}` |
| `{:<10}`, `{:>10}`, `{:^10}`, `{:*^10}` | Left/right/center alignment, with an optional fill character before the alignment character |
| `{:10}`, `{:.5}`, `{:10.5}` | Width and precision (precision caps how many characters a plain field consumes) |

### Types

| Type | Go value | Notes |
| --- | --- | --- |
| `d` | `int64` | Decimal, or `0x`/`0o`/`0b`-prefixed |
| `n` | `int64` | Decimal with `,` or `.` thousands separators |
| `x`, `X` | `int64` | Hex, with optional `0x`/`0X` prefix |
| `o` | `int64` | Octal, with optional `0o`/`0O` prefix |
| `b` | `int64` | Binary, with optional `0b`/`0B` prefix |
| `f`, `F` | `float64` | Python's `F` returns `Decimal`; fmtparse returns `float64` for both (see Limitations) |
| `e`, `E` | `float64` | Scientific notation |
| `g`, `G` | `float64` | General float, with or without exponent |
| `%` | `float64` | Percentage, divided by 100 |
| `w` | `string` | `\w+`: word characters |
| `W` | `string` | `\W+`: non-word characters |
| `D` | `string` | `\D+`: non-digits |
| `s` | `string` | `\s+`: **whitespace**, not "any string" — see below |
| `S` | `string` | `\S+`: non-whitespace |
| `l` | `string` | Letters only (`[A-Za-z]+`) |
| `ti` | `time.Time` | ISO 8601 |
| `te` | `time.Time` | RFC 2822 |
| `tg`, `ta` | `time.Time` | `dd-mm-yyyy` / `mm-dd-yyyy`, month name or number, optional time/AM-PM/timezone |
| `th` | `time.Time` | Apache/HTTP combined log format |
| `tc` | `time.Time` | C `ctime()` format |
| `tt` | `time.Time` | Time only; the returned value carries the placeholder date year 1, January 1 |
| any other single character | `string` | Matches `\<char>+` literally, e.g. `{:z}` matches one or more literal `z` characters, ported as-is from upstream |

`s` matching whitespace is not a typo: it is upstream's actual behavior
(`{:s}` falls through to a generic `\<type>+` regex, and `\s` means
whitespace), confirmed by running it, not assumed from the type letter's
name. A bare `{}` or `{name}` with no type — the common case for "a string"
— matches non-greedily and is unaffected by this.

### Result

```go
type Result struct {
    Fixed []any
    Named map[string]any
    Spans map[string][2]int
}
```

`Spans` gives byte offsets into the matched string, keyed by decimal index
(`"0"`, `"1"`, ...) for fixed fields and by flat field name for named
fields. Typed getters: `Int(name)`, `Float(name)`, `String(name)`,
`Time(name)`, and `Get(name) (any, bool)` for the raw value.

## How it works

A format string is translated into a single regular expression: literal
text is escaped, `{{`/`}}` become literal braces, and each `{...}` field
becomes a capturing group whose inner pattern is chosen by its type letter
— almost entirely mirroring parse.py's own field-to-regex translation,
including its exact group-index bookkeeping for the multi-group date types,
so the two implementations build structurally equivalent expressions
field-by-field. `Parse` anchors the expression with `\A...\z`; `Search` and
`FindAll` leave it unanchored. Repeated field names (`{a}...{a}`) are the
one place the translation must diverge: Go's `regexp` (RE2) has no
backreferences, so a repeated name becomes a second independent capturing
group, and its captured text is checked for equality against the first
occurrence after matching, instead of during matching.

## Limitations

- **Repeated field names use post-match equality, not a real
  backreference.** Python's engine backtracks to find any split of the
  string that satisfies a backreference; RE2 can't backtrack, so fmtparse
  takes the match RE2's linear-time engine finds and only then checks the
  repeated captures for equality. `Parse` and `Search`/`FindAll` all handle
  this correctly for realistic inputs (`Search`/`FindAll` retry from
  successive start positions on a constraint failure), but a pattern that
  needs a specific non-greedy split found only by backtracking to satisfy
  the repeat could still fail to match where Python would succeed. Every
  differential test with a repeated field passes; this is a theoretical
  edge documented for honesty, not a failure observed in testing.
- **`F` returns `float64`, not an arbitrary-precision decimal.** Python's
  `F` type returns `decimal.Decimal`; Go's standard library has no decimal
  type, and adding one would break the zero-dependency goal. Values beyond
  `float64`'s ~15-17 significant digits lose precision.
- **No arbitrary `{:%Y-%m-%d}`-style strftime passthrough type.** Upstream
  lets any format spec containing a `%`-code be used directly as a date
  pattern. Go's `time.Parse` uses reference-time layouts, not strftime
  codes, and several codes (`%f` mid-string, `%j`, `%U`, `%W`) don't map
  onto it cleanly. Use one of the eight named date types below, or a custom
  type via `WithType`.
- **The `ts` date type (month + day, no year) is not implemented.** It is
  the one built-in date type omitted from the required set; `{:ts}` returns
  a compile error rather than silently mismatching.
- **Integers are `int64`, not arbitrary precision.** Python's `int` has no
  bound; very large `{:d}` values overflow.
- **A lowercase month name can fail with an error, not silently mismatch.**
  This is not fmtparse's own choice: it reproduces a real bug in upstream,
  confirmed by running it — `parse.parse("{:tg}", "21-nov-2016")` raises an
  uncaught `KeyError` from Python itself, because month names are matched
  case-insensitively but then looked up in a dict keyed only by capitalized
  names. fmtparse hits the same "unknown month" situation but returns it as
  a normal Go `error` instead of crashing the caller.
- **Dotted names do not nest — verified, not assumed.** The task this
  package was built from expected `{a.b}` to nest like `{a[b]}` does; it
  doesn't, in upstream either. `parse.parse("{a.b} {a[c]}", "1 2").named`
  is `{'a.b': '1', 'a': {'c': '2'}}` in Python 1.22.2 — only bracket
  indexing nests. fmtparse matches this actual behavior.
- **No `logging` / debug hook.** Python's module logs the generated regex
  at debug level; fmtparse has no equivalent (`Parser.Format()` returns the
  source format string, which is the closest analogue).

## Compatibility

Built and tested with Go 1.27.1 on macOS/arm64 (darwin). CI (see
`.github/workflows/test.yml`) runs the same checks on Go 1.23 on
`ubuntu-latest`, which is exercised by GitHub Actions, not locally, by this
package's author.

## Development

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l .   # must print nothing
```

`testdata/cases.json` holds 173 differential test cases: real output
captured by running Python's `parse` 1.22.2 against a wide spread of
formats and inputs (every type, alignment/fill/width, escapes, dotted and
bracketed names, repeated names, case sensitivity, `Parse` and `Search`,
and non-matching inputs), via the generator script kept alongside this
package's build notes. `differential_test.go` replays every case through
fmtparse and requires match/no-match and every field value to agree.

## License

MIT. See [LICENSE](LICENSE).
