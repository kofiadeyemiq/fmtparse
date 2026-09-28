// Package fmtparse parses strings against a format specification, the
// reverse of fmt.Sprintf. It is a Go port of Python's parse library
// (github.com/r1chardj0n3s/parse) — see the README for exactly what is and
// is not ported.
package fmtparse

import (
	"fmt"
	"iter"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// converter turns a matched field's raw text into a typed Go value. raw is
// the field's own captured substring; groups is the full 0-based
// Python-groups()-style slice of every captured substring in the match,
// which date/time converters need to reach sibling groups.
type converter func(raw string, groups []string) (any, error)

// RepeatedFieldError reports that a name was used for two fields with
// different format specs, which parse.py treats as an error (its
// RepeatedNameError).
type RepeatedFieldError struct {
	Name     string
	Format   string
	Previous string
}

func (e *RepeatedFieldError) Error() string {
	return fmt.Sprintf("fmtparse: field type %q for field %q does not match previously seen type %q", e.Format, e.Name, e.Previous)
}

// parseTokenRe ports parse.py's PARSE_RE: it tokenizes a format string into
// "{{" / "}}" escapes and brace-delimited fields, splitting on the other
// text implicitly (see generateExpression).
var parseTokenRe = regexp.MustCompile(`\{\{|\}\}|\{[\w-]*(?:\.[\w-]+|\[[^\]]+\])*(?::[^}]+)?\}`)

const allowedSingleTypes = "nboxX%fFeEgGwWdDsSl"

var allowedDateTypes = map[string]bool{
	"ti": true, "te": true, "tg": true, "ta": true, "th": true, "tc": true, "tt": true,
}

func isAllowedType(typ string, extraTypes map[string]customType) bool {
	if len(typ) == 1 && strings.ContainsRune(allowedSingleTypes, rune(typ[0])) {
		return true
	}
	if allowedDateTypes[typ] {
		return true
	}
	_, ok := extraTypes[typ]
	return ok
}

// fieldFormat is the parsed [[fill]align][sign][0][width][grouping][.precision][type]
// format spec, ported from parse.py's extract_format().
type fieldFormat struct {
	fill         string
	align        string
	width        string
	grouping     string
	precisionSet bool
	precision    string
	typ          string
}

func extractFormat(spec string, extraTypes map[string]customType) (fieldFormat, error) {
	var f fieldFormat
	r := []rune(spec)

	if len(r) > 0 && strings.ContainsRune("<>=^", r[0]) {
		f.align = string(r[0])
		r = r[1:]
	} else if len(r) > 1 && strings.ContainsRune("<>=^", r[1]) {
		f.fill = string(r[0])
		f.align = string(r[1])
		r = r[2:]
	}

	if len(r) > 0 && (r[0] == '+' || r[0] == '-' || r[0] == ' ') {
		r = r[1:]
	}

	if len(r) > 0 && r[0] == '0' {
		// The zero-pad flag is consumed but not wired into fill, matching
		// parse.py exactly: it never reads `zero` back out.
		r = r[1:]
	}

	var width []rune
	for len(r) > 0 && unicode.IsDigit(r[0]) {
		width = append(width, r[0])
		r = r[1:]
	}
	f.width = string(width)

	if len(r) > 0 && r[0] == ',' {
		r = r[1:]
		f.grouping = ","
	} else if len(r) > 0 && r[0] == '_' {
		r = r[1:]
		f.grouping = "_"
	}

	if len(r) > 0 && r[0] == '.' {
		r = r[1:]
		f.precisionSet = true
		var precision []rune
		for len(r) > 0 && unicode.IsDigit(r[0]) {
			precision = append(precision, r[0])
			r = r[1:]
		}
		f.precision = string(precision)
	}

	f.typ = string(r)
	if f.typ != "" && !isAllowedType(f.typ, extraTypes) {
		return fieldFormat{}, fmt.Errorf("fmtparse: format spec %q not recognised", f.typ)
	}
	return f, nil
}

// parserBuilder accumulates state while translating a format string into a
// regular expression; it mirrors the instance state of parse.py's Parser.
type parserBuilder struct {
	format      string
	extraTypes  map[string]customType
	groupToName map[string]string // regex group name -> flat field name
	nameToGroup map[string]string // flat field name -> its first regex group name
	nameFormat  map[string]string // flat field name -> the format spec first seen for it

	namedFields  []string             // group names, first-seen order (drives Result.Named)
	fixedFields  []int                // 0-based python-groups()-style index per fixed field, in order
	conversions  map[string]converter // keyed by group name (named) or "#<index>" (fixed)
	repeatGroups map[string][]string  // flat field name -> every group name sharing it
	groupIndex   int
}

func fixedKey(idx int) string { return "#" + strconv.Itoa(idx) }

func (b *parserBuilder) toGroupName(field string) string {
	group := strings.NewReplacer(".", "_", "[", "_", "]", "_", "-", "_").Replace(field)
	n := 1
	for {
		if _, collide := b.groupToName[group]; !collide {
			break
		}
		n++
		switch {
		case strings.Contains(field, "."):
			group = strings.ReplaceAll(field, ".", strings.Repeat("_", n))
		case strings.Contains(field, "_"):
			group = strings.ReplaceAll(field, "_", strings.Repeat("_", n))
		case strings.Contains(field, "-"):
			group = strings.ReplaceAll(field, "-", strings.Repeat("_", n))
		default:
			// parse.py raises KeyError here; this is truly unreachable given
			// how the group string above is built from field's own characters.
			group = fmt.Sprintf("%s_%d", group, n)
		}
	}
	b.groupToName[group] = field
	b.nameToGroup[field] = group
	return group
}

func isNamedField(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsLetter(r)
}

func (b *parserBuilder) generateExpression() (string, error) {
	var sb strings.Builder
	format := b.format
	last := 0
	for _, loc := range parseTokenRe.FindAllStringIndex(format, -1) {
		start, end := loc[0], loc[1]
		if start > last {
			sb.WriteString(regexp.QuoteMeta(format[last:start]))
		}
		token := format[start:end]
		switch token {
		case "{{":
			sb.WriteString(`\{`)
		case "}}":
			sb.WriteString(`\}`)
		default:
			frag, err := b.handleField(token)
			if err != nil {
				return "", err
			}
			sb.WriteString(frag)
		}
		last = end
	}
	if last < len(format) {
		sb.WriteString(regexp.QuoteMeta(format[last:]))
	}
	return sb.String(), nil
}

func (b *parserBuilder) handleField(token string) (string, error) {
	field := token[1 : len(token)-1]
	name := field
	formatSpec := ""
	if idx := strings.IndexByte(field, ':'); idx != -1 {
		name, formatSpec = field[:idx], field[idx+1:]
	}

	if isNamedField(name) {
		if _, seen := b.nameToGroup[name]; seen {
			if b.nameFormat[name] != formatSpec {
				return "", &RepeatedFieldError{Name: name, Format: formatSpec, Previous: b.nameFormat[name]}
			}
			groupName := fmt.Sprintf("%s__r%d", b.nameToGroup[name], len(b.repeatGroups[name])+1)
			// Go's RE2 has no backreferences, so a repeated {name} becomes a
			// second independent capturing group with the same pattern
			// instead of parse.py's `(?P=group)`; equality between the two
			// captures is enforced after matching (see repeatsSatisfied).
			frag, _, err := b.buildFieldBody(groupName, formatSpec, false)
			if err != nil {
				return "", err
			}
			b.repeatGroups[name] = append(b.repeatGroups[name], groupName)
			return frag, nil
		}
		group := b.toGroupName(name)
		b.nameFormat[name] = formatSpec
		b.namedFields = append(b.namedFields, group)
		b.repeatGroups[name] = []string{group}
		frag, conv, err := b.buildFieldBody(group, formatSpec, false)
		if err != nil {
			return "", err
		}
		if conv != nil {
			b.conversions[group] = conv
		}
		return frag, nil
	}

	idx := b.groupIndex
	b.fixedFields = append(b.fixedFields, idx)
	frag, conv, err := b.buildFieldBody("", formatSpec, true)
	if err != nil {
		return "", err
	}
	if conv != nil {
		b.conversions[fixedKey(idx)] = conv
	}
	return frag, nil
}

// buildFieldBody ports the tail of parse.py's _handle_field: turning a
// format spec into a regex fragment, wrapped in a capturing group, with
// fill/alignment applied around it.
func (b *parserBuilder) buildFieldBody(group, formatSpec string, fixed bool) (string, converter, error) {
	wrap := func(s string) string {
		if fixed {
			return "(" + s + ")"
		}
		return "(?P<" + group + ">" + s + ")"
	}

	if formatSpec == "" {
		frag := wrap(".+?")
		b.groupIndex++
		return frag, nil, nil
	}

	f, err := extractFormat(formatSpec, b.extraTypes)
	if err != nil {
		return "", nil, err
	}

	typ := f.typ
	isNumeric := len(typ) == 1 && strings.ContainsRune("n%fFegdobxEGX", rune(typ[0]))
	n := b.groupIndex

	var s string
	var conv converter

	switch {
	case typ != "" && b.extraTypes != nil && isCustomType(typ, b.extraTypes):
		ct := b.extraTypes[typ]
		pattern := ct.pattern
		if pattern == "" {
			pattern = ".+?"
		}
		b.groupIndex += ct.groupCount
		userConvert := ct.convert
		conv = func(raw string, _ []string) (any, error) { return userConvert(raw) }
		s = pattern
	case typ == "n":
		s = `\d{1,3}([,.]\d{3})*`
		b.groupIndex++
		conv = wrapSimple(convertInt(10))
	case typ == "b":
		s = `(0[bB])?[01]+`
		conv = wrapSimple(convertInt(2))
		b.groupIndex++
	case typ == "o":
		s = `(0[oO])?[0-7]+`
		conv = wrapSimple(convertInt(8))
		b.groupIndex++
	case typ == "x" || typ == "X":
		s = `(0[xX])?[0-9a-fA-F]+`
		conv = wrapSimple(convertInt(16))
		b.groupIndex++
	case typ == "%":
		s = `\d+(\.\d+)?%`
		b.groupIndex++
		conv = wrapSimple(convertPercentage)
	case typ == "f" || typ == "F":
		if f.precisionSet && f.precision == "0" {
			s = `(?:\d+|nan|NAN|inf|INF)`
		} else {
			s = `(?:\d*\.\d+|nan|NAN|inf|INF)`
		}
		conv = wrapSimple(convertFloat)
	case typ == "e" || typ == "E":
		s = `\d*\.\d+[eE][-+]?\d+|nan|NAN|[-+]?inf|[-+]?INF`
		conv = wrapSimple(convertFloat)
	case typ == "g" || typ == "G":
		s = `\d+(\.\d+)?([eE][-+]?\d+)?|nan|NAN|[-+]?inf|[-+]?INF`
		b.groupIndex += 2
		conv = wrapSimple(convertFloat)
	case typ == "d":
		width := "+"
		if f.width != "" {
			wv, err := strconv.Atoi(f.width)
			if err != nil {
				return "", nil, fmt.Errorf("fmtparse: bad width %q: %w", f.width, err)
			}
			width = fmt.Sprintf("{1,%d}", wv)
		}
		g := f.grouping
		s = fmt.Sprintf(`[-+ ]?[0-9%s]%s|[-+ ]?0[xX][0-9a-fA-F%s]%s|[-+ ]?0[bB][01%s]%s|[-+ ]?0[oO][0-7%s]%s`,
			g, width, g, width, g, width, g, width)
		conv = wrapSimple(convertInt(0))
	case allowedDateTypes[typ]:
		frag, cv, consumed, err := buildDateTimeFragment(typ, n)
		if err != nil {
			return "", nil, err
		}
		s = frag
		conv = cv
		b.groupIndex += consumed
	case typ == "l":
		s = `[A-Za-z]+`
	case typ != "":
		s = `\` + typ + `+`
	case f.precisionSet:
		if f.width != "" {
			s = fmt.Sprintf(`.{%s,%s}?`, f.width, f.precision)
		} else {
			s = fmt.Sprintf(`.{1,%s}?`, f.precision)
		}
	case f.width != "":
		s = fmt.Sprintf(`.{%s,}?`, f.width)
	default:
		s = `.+?`
	}

	align := f.align
	fill := f.fill

	if isNumeric {
		if align == "=" {
			if fill == "" {
				fill = "0"
			}
			s = fill + "*" + s
		}
		s = `[-+ ]?` + s
	}

	if fill == "" {
		fill = " "
	}

	s = wrap(s)
	b.groupIndex++

	if f.width != "" && align == "" {
		align = ">"
	}

	if strings.ContainsAny(fill, `.\+?*[](){}^$`) {
		fill = `\` + fill
	}

	switch align {
	case "<":
		s = s + fill + "*"
	case ">":
		s = fill + "*" + s
	case "^":
		s = fill + "*" + s + fill + "*"
	}

	return s, conv, nil
}

func isCustomType(typ string, extraTypes map[string]customType) bool {
	_, ok := extraTypes[typ]
	return ok
}

func wrapSimple(f func(string) (any, error)) converter {
	return func(raw string, _ []string) (any, error) { return f(raw) }
}

// Parser is a compiled format string, produced by [Compile]. It can be
// reused to parse many strings without re-parsing the format.
type Parser struct {
	format        string
	caseSensitive bool
	expression    string
	matchRe       *regexp.Regexp
	searchRe      *regexp.Regexp

	namedFields  []string
	groupToName  map[string]string
	fixedFields  []int
	conversions  map[string]converter
	repeatGroups map[string][]string
	groupIndexOf map[string]int
}

// Format returns the format string the Parser was compiled from.
func (p *Parser) Format() string { return p.format }

func compileParser(format string, cfg *config) (*Parser, error) {
	b := &parserBuilder{
		format:       format,
		extraTypes:   cfg.extraTypes,
		groupToName:  map[string]string{},
		nameToGroup:  map[string]string{},
		nameFormat:   map[string]string{},
		conversions:  map[string]converter{},
		repeatGroups: map[string][]string{},
	}
	expr, err := b.generateExpression()
	if err != nil {
		return nil, err
	}

	flags := "(?s)"
	if !cfg.caseSensitive {
		flags = "(?is)"
	}

	matchRe, err := regexp.Compile(flags + `\A(?:` + expr + `)\z`)
	if err != nil {
		return nil, fmt.Errorf("fmtparse: could not compile format %q: %w", format, err)
	}
	searchRe, err := regexp.Compile(flags + expr)
	if err != nil {
		return nil, fmt.Errorf("fmtparse: could not compile format %q: %w", format, err)
	}

	groupIndexOf := map[string]int{}
	for i, name := range searchRe.SubexpNames() {
		if name != "" {
			groupIndexOf[name] = i - 1
		}
	}

	return &Parser{
		format:        format,
		caseSensitive: cfg.caseSensitive,
		expression:    expr,
		matchRe:       matchRe,
		searchRe:      searchRe,
		namedFields:   b.namedFields,
		groupToName:   b.groupToName,
		fixedFields:   b.fixedFields,
		conversions:   b.conversions,
		repeatGroups:  b.repeatGroups,
		groupIndexOf:  groupIndexOf,
	}, nil
}

func shiftLoc(loc []int, offset int) []int {
	out := make([]int, len(loc))
	for i, v := range loc {
		if v == -1 {
			out[i] = -1
		} else {
			out[i] = v + offset
		}
	}
	return out
}

// repeatsSatisfied checks that every occurrence of a repeated {name} field
// captured the same text (case-insensitively unless the Parser is
// case-sensitive), which is what a true regex backreference would enforce.
func (p *Parser) repeatsSatisfied(s string, loc []int) bool {
	for _, groupNames := range p.repeatGroups {
		if len(groupNames) < 2 {
			continue
		}
		var first string
		haveFirst := false
		for _, gn := range groupNames {
			idx, ok := p.groupIndexOf[gn]
			if !ok {
				continue
			}
			a, b := loc[2*(idx+1)], loc[2*(idx+1)+1]
			var val string
			if a != -1 {
				val = s[a:b]
			}
			if !haveFirst {
				first, haveFirst = val, true
				continue
			}
			if p.caseSensitive {
				if val != first {
					return false
				}
			} else if !strings.EqualFold(val, first) {
				return false
			}
		}
	}
	return true
}

func (p *Parser) evaluate(s string, loc []int) (*Result, error) {
	numGroups := len(loc)/2 - 1
	groups := make([]string, numGroups)
	for i := 0; i < numGroups; i++ {
		a, bIdx := loc[2*(i+1)], loc[2*(i+1)+1]
		if a != -1 {
			groups[i] = s[a:bIdx]
		}
	}

	res := newResult()

	fixed := make([]any, len(p.fixedFields))
	for pos, groupIdx := range p.fixedFields {
		raw := groups[groupIdx]
		var val any = raw
		if conv, ok := p.conversions[fixedKey(groupIdx)]; ok {
			v, err := conv(raw, groups)
			if err != nil {
				return nil, fmt.Errorf("fmtparse: converting fixed field %d: %w", pos, err)
			}
			val = v
		}
		fixed[pos] = val
		a, bIdx := loc[2*(groupIdx+1)], loc[2*(groupIdx+1)+1]
		res.Spans[strconv.Itoa(pos)] = [2]int{a, bIdx}
	}
	res.Fixed = fixed

	flatNamed := map[string]any{}
	for _, group := range p.namedFields {
		flatName := p.groupToName[group]
		groupIdx, ok := p.groupIndexOf[group]
		if !ok {
			continue
		}
		raw := groups[groupIdx]
		var val any = raw
		if conv, ok := p.conversions[group]; ok {
			v, err := conv(raw, groups)
			if err != nil {
				return nil, fmt.Errorf("fmtparse: converting field %q: %w", flatName, err)
			}
			val = v
		}
		flatNamed[flatName] = val
		a, bIdx := loc[2*(groupIdx+1)], loc[2*(groupIdx+1)+1]
		res.Spans[flatName] = [2]int{a, bIdx}
	}
	res.Named = expandNamedFields(flatNamed)

	return res, nil
}

// expandNamedFields ports parse.py's _expand_named_fields exactly,
// including its real (if surprising) behaviour: only bracket-indexed names
// ({a[b]}) nest into maps. Dotted names ({a.b}) stay a single flat key
// "a.b" — verified against the upstream library, not merely assumed.
func expandNamedFields(named map[string]any) map[string]any {
	result := map[string]any{}
	indexRe := regexp.MustCompile(`\[[^\]]+\]`)
	for field, value := range named {
		basename, subkeys := field, ""
		if n := strings.IndexByte(field, '['); n != -1 {
			basename, subkeys = field[:n], field[n:]
		}
		d := result
		k := basename
		if subkeys != "" {
			for _, sk := range indexRe.FindAllString(subkeys, -1) {
				next, ok := d[k].(map[string]any)
				if !ok {
					next = map[string]any{}
					d[k] = next
				}
				d = next
				k = sk[1 : len(sk)-1]
			}
		}
		d[k] = value
	}
	return result
}

// Parse matches format against the whole of s. It returns (nil, nil) if s
// does not match format, and a non-nil error only when format itself is
// invalid or a matched value fails type conversion.
func (p *Parser) Parse(s string) (*Result, error) {
	loc := p.matchRe.FindStringSubmatchIndex(s)
	if loc == nil {
		return nil, nil
	}
	if !p.repeatsSatisfied(s, loc) {
		return nil, nil
	}
	return p.evaluate(s, loc)
}

// Search finds the first occurrence of format anywhere in s. It returns
// (nil, nil) if there is no occurrence.
func (p *Parser) Search(s string) (*Result, error) {
	pos := 0
	for pos <= len(s) {
		loc := p.searchRe.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			return nil, nil
		}
		shifted := shiftLoc(loc, pos)
		if p.repeatsSatisfied(s, shifted) {
			return p.evaluate(s, shifted)
		}
		start := shifted[0]
		_, size := utf8.DecodeRuneInString(s[start:])
		if size == 0 {
			return nil, nil
		}
		pos = start + size
	}
	return nil, nil
}

// FindAll returns an iterator over every non-overlapping occurrence of
// format in s, left to right.
func (p *Parser) FindAll(s string) iter.Seq[*Result] {
	return func(yield func(*Result) bool) {
		pos := 0
		for pos <= len(s) {
			loc := p.searchRe.FindStringSubmatchIndex(s[pos:])
			if loc == nil {
				return
			}
			shifted := shiftLoc(loc, pos)
			if !p.repeatsSatisfied(s, shifted) {
				start := shifted[0]
				_, size := utf8.DecodeRuneInString(s[start:])
				if size == 0 {
					return
				}
				pos = start + size
				continue
			}
			res, err := p.evaluate(s, shifted)
			if err != nil {
				return
			}
			if !yield(res) {
				return
			}
			if shifted[1] == shifted[0] {
				_, size := utf8.DecodeRuneInString(s[shifted[1]:])
				if size == 0 {
					return
				}
				pos = shifted[1] + size
			} else {
				pos = shifted[1]
			}
		}
	}
}

// Compile parses format once into a reusable Parser.
func Compile(format string, opts ...Option) (*Parser, error) {
	return compileParser(format, newConfig(opts))
}

// Parse matches format against the whole of s using the same rules as
// [Parser.Parse]. See [CaseSensitive] and [WithType] for opts.
func Parse(format, s string, opts ...Option) (*Result, error) {
	p, err := Compile(format, opts...)
	if err != nil {
		return nil, err
	}
	return p.Parse(s)
}

// Search finds the first occurrence of format anywhere in s using the same
// rules as [Parser.Search].
func Search(format, s string, opts ...Option) (*Result, error) {
	p, err := Compile(format, opts...)
	if err != nil {
		return nil, err
	}
	return p.Search(s)
}

// FindAll returns an iterator over every occurrence of format in s. Unlike
// [Parser.FindAll], this package-level form must compile format first, so
// it can fail; that is why its signature carries an error return that the
// method form does not need.
func FindAll(format, s string, opts ...Option) (iter.Seq[*Result], error) {
	p, err := Compile(format, opts...)
	if err != nil {
		return nil, err
	}
	return p.FindAll(s), nil
}
