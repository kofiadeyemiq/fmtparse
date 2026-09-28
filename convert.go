package fmtparse

import (
	"strconv"
	"strings"
)

const intConvertChars = "0123456789abcdefghijklmnopqrstuvwxyz"

// convertInt ports parse.py's int_convert class. base == 0 means
// "auto-detect from a 0x/0o/0b prefix, default 10", matching Python's
// int_convert(base=None) behaviour used for the "d" type.
func convertInt(base int) func(string) (any, error) {
	return func(s string) (any, error) {
		if s == "" {
			return nil, strconv.ErrSyntax
		}
		sign := int64(1)
		numberStart := 0
		switch s[0] {
		case '-':
			sign = -1
			numberStart = 1
		case '+':
			numberStart = 1
		}

		b := base
		if b == 0 {
			b = 10
			if numberStart < len(s) && s[numberStart] == '0' && len(s)-numberStart > 2 {
				switch s[numberStart+1] {
				case 'b', 'B':
					b = 2
				case 'o', 'O':
					b = 8
				case 'x', 'X':
					b = 16
				}
			}
		}

		chars := intConvertChars[:b]
		lower := strings.ToLower(s)
		var filtered strings.Builder
		for _, r := range lower {
			if strings.ContainsRune(chars, r) {
				filtered.WriteRune(r)
			}
		}
		v, err := strconv.ParseInt(filtered.String(), b, 64)
		if err != nil {
			return nil, err
		}
		return sign * v, nil
	}
}

// convertFloat parses a matched numeric substring (with a sign already
// consumed as part of the regex) as a float64. Go's strconv.ParseFloat
// already accepts "nan"/"inf"/"+inf"/"-inf" case-insensitively, which
// covers every literal the "f"/"e"/"g" regexes allow.
func convertFloat(s string) (any, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// convertPercentage ports parse.py's percentage(): strips the trailing '%'
// and divides by 100.
func convertPercentage(s string) (any, error) {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	if err != nil {
		return nil, err
	}
	return v / 100.0, nil
}
