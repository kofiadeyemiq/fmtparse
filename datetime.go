package fmtparse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Regex fragments ported verbatim from parse.py's DAYS_PAT, MONTHS_PAT,
// ALL_MONTHS_PAT, TIME_PAT, AM_PAT and TZ_PAT.
const (
	daysPat   = `(Mon|Tue|Wed|Thu|Fri|Sat|Sun)`
	monthsPat = `(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`
	timePat   = `(\d{1,2}:\d{1,2}(:\d{1,2}(\.\d+)?)?)`
	amPat     = `(\s+[AP]M)`
	tzPat     = `(\s+[-+]\d\d?:?\d\d)`
)

// allMonthsPat mirrors ALL_MONTHS_PAT: alternation over MONTHS_MAP's keys in
// their original (insertion) order, both short and long month names.
var allMonthsPat = "(" + strings.Join([]string{
	"Jan", "January", "Feb", "February", "Mar", "March", "Apr", "April",
	"May", "Jun", "June", "Jul", "July", "Aug", "August", "Sep", "September",
	"Oct", "October", "Nov", "November", "Dec", "December",
}, "|") + ")"

var monthsMap = map[string]int{
	"Jan": 1, "January": 1, "Feb": 2, "February": 2, "Mar": 3, "March": 3,
	"Apr": 4, "April": 4, "May": 5, "Jun": 6, "June": 6, "Jul": 7, "July": 7,
	"Aug": 8, "August": 8, "Sep": 9, "September": 9, "Oct": 10, "October": 10,
	"Nov": 11, "November": 11, "Dec": 12, "December": 12,
}

var dateSplitRe = regexp.MustCompile(`[-/\s]`)

const noGroup = -1

// dateConvertOpts records, as 0-based Python-groups()-style indices into the
// match's group slice, which capture groups hold which date/time component.
// It mirrors the keyword arguments of parse.py's date_convert().
type dateConvertOpts struct {
	ymd, mdy, dmy    int
	dmyD, dmyM, dmyY int // the "tc" (ctime) type's separately-captured d/m/y groups
	hms, am, tz      int
}

func newDateConvertOpts() dateConvertOpts {
	return dateConvertOpts{ymd: noGroup, mdy: noGroup, dmy: noGroup, dmyD: noGroup, dmyM: noGroup, dmyY: noGroup, hms: noGroup, am: noGroup, tz: noGroup}
}

// buildDateTimeFragment returns the regex fragment, a converter and the
// number of capturing groups the fragment adds, for one of the built-in
// date/time field types (ti, tg, ta, te, th, tc, tt). n is the 0-based
// group index in effect immediately before this fragment's own groups are
// added — it mirrors parse.py's `n = self._group_index` captured at the
// same point.
func buildDateTimeFragment(typ string, n int) (string, converter, int, error) {
	switch typ {
	case "ti":
		frag := `(\d{4}-\d\d-\d\d)((\s+|T)` + timePat + `)?(Z|\s*[-+]\d\d:?\d\d)?`
		o := newDateConvertOpts()
		o.ymd, o.hms, o.tz = n+1, n+4, n+7
		return frag, dateConverter(o), 7, nil
	case "tg":
		frag := fmt.Sprintf(`(\d{1,2}[-/](\d{1,2}|%s)[-/]\d{4})(\s+%s)?%s?%s?`, allMonthsPat, timePat, amPat, tzPat)
		o := newDateConvertOpts()
		o.dmy, o.hms, o.am, o.tz = n+1, n+5, n+8, n+9
		return frag, dateConverter(o), 9, nil
	case "ta":
		frag := fmt.Sprintf(`((\d{1,2}|%s)[-/]\d{1,2}[-/]\d{4})(\s+%s)?%s?%s?`, allMonthsPat, timePat, amPat, tzPat)
		o := newDateConvertOpts()
		o.mdy, o.hms, o.am, o.tz = n+1, n+5, n+8, n+9
		return frag, dateConverter(o), 9, nil
	case "te":
		frag := fmt.Sprintf(`(%s,\s+)?(\d{1,2}\s+%s\s+\d{4})\s+%s%s`, daysPat, monthsPat, timePat, tzPat)
		o := newDateConvertOpts()
		o.dmy, o.hms, o.tz = n+3, n+5, n+8
		return frag, dateConverter(o), 8, nil
	case "th":
		frag := fmt.Sprintf(`(\d{1,2}[-/]%s[-/]\d{4}):%s%s`, monthsPat, timePat, tzPat)
		o := newDateConvertOpts()
		o.dmy, o.hms, o.tz = n+1, n+3, n+6
		return frag, dateConverter(o), 6, nil
	case "tc":
		frag := fmt.Sprintf(`(%s)\s+%s\s+(\d{1,2})\s+%s\s+(\d{4})`, daysPat, monthsPat, timePat)
		o := newDateConvertOpts()
		o.dmyD, o.dmyM, o.dmyY, o.hms = n+4, n+3, n+8, n+5
		return frag, dateConverter(o), 8, nil
	case "tt":
		frag := fmt.Sprintf(`%s?%s?%s?`, timePat, amPat, tzPat)
		o := newDateConvertOpts()
		o.hms, o.am, o.tz = n+1, n+4, n+5
		return frag, dateConverter(o), 5, nil
	}
	return "", nil, 0, fmt.Errorf("fmtparse: unsupported date/time type %q", typ)
}

func isAllUpperLetters(s string) bool {
	cased := false
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			cased = true
		}
	}
	return cased
}

func parseYMD(yStr, mStr, dStr string) (y, m, d int, err error) {
	y, err = strconv.Atoi(yStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("bad year %q: %w", yStr, err)
	}
	if mi, err2 := strconv.Atoi(mStr); err2 == nil {
		m = mi
	} else {
		mi, ok := monthsMap[mStr]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown month %q", mStr)
		}
		m = mi
	}
	d, err = strconv.Atoi(dStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("bad day %q: %w", dStr, err)
	}
	return y, m, d, nil
}

// dateConverter ports parse.py's date_convert(), building a converter
// closure that reaches into the full groups slice (not just its own raw
// match) to assemble a time.Time.
func dateConverter(o dateConvertOpts) converter {
	return func(_ string, groups []string) (any, error) {
		var y, m, d int
		timeOnly := true

		switch {
		case o.ymd != noGroup:
			parts := dateSplitRe.Split(groups[o.ymd], -1)
			if len(parts) != 3 {
				return nil, fmt.Errorf("fmtparse: unparseable date %q", groups[o.ymd])
			}
			var err error
			y, m, d, err = parseYMD(parts[0], parts[1], parts[2])
			if err != nil {
				return nil, err
			}
			timeOnly = false
		case o.mdy != noGroup:
			parts := dateSplitRe.Split(groups[o.mdy], -1)
			if len(parts) != 3 {
				return nil, fmt.Errorf("fmtparse: unparseable date %q", groups[o.mdy])
			}
			var err error
			y, m, d, err = parseYMD(parts[2], parts[0], parts[1])
			if err != nil {
				return nil, err
			}
			timeOnly = false
		case o.dmy != noGroup:
			parts := dateSplitRe.Split(groups[o.dmy], -1)
			if len(parts) != 3 {
				return nil, fmt.Errorf("fmtparse: unparseable date %q", groups[o.dmy])
			}
			var err error
			y, m, d, err = parseYMD(parts[2], parts[1], parts[0])
			if err != nil {
				return nil, err
			}
			timeOnly = false
		case o.dmyD != noGroup:
			var err error
			y, m, d, err = parseYMD(groups[o.dmyY], groups[o.dmyM], groups[o.dmyD])
			if err != nil {
				return nil, err
			}
			timeOnly = false
		}

		H, M, S, us := 0, 0, 0, 0
		if o.hms != noGroup && groups[o.hms] != "" {
			parts := strings.Split(groups[o.hms], ":")
			var err error
			H, err = strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("fmtparse: bad hour: %w", err)
			}
			M, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("fmtparse: bad minute: %w", err)
			}
			if len(parts) >= 3 {
				secStr := parts[2]
				if idx := strings.IndexByte(secStr, '.'); idx != -1 {
					S, err = strconv.Atoi(secStr[:idx])
					if err != nil {
						return nil, fmt.Errorf("fmtparse: bad second: %w", err)
					}
					frac := secStr[idx+1:]
					for len(frac) < 6 {
						frac += "0"
					}
					us, err = strconv.Atoi(frac[:6])
					if err != nil {
						return nil, fmt.Errorf("fmtparse: bad fraction: %w", err)
					}
				} else {
					S, err = strconv.Atoi(secStr)
					if err != nil {
						return nil, fmt.Errorf("fmtparse: bad second: %w", err)
					}
				}
			}
		}

		if o.am != noGroup && groups[o.am] != "" {
			am := strings.TrimSpace(groups[o.am])
			switch {
			case am == "AM" && H == 12:
				H -= 12
			case am == "PM" && H == 12:
				// midday, unchanged
			case am == "PM":
				H += 12
			}
		}

		loc := time.UTC
		if o.tz != noGroup && groups[o.tz] != "" {
			tz := strings.TrimSpace(groups[o.tz])
			switch {
			case tz == "Z":
				loc = time.UTC
			case isAllUpperLetters(tz):
				// named zone abbreviation: not resolved, same as upstream's TODO.
			default:
				sign := tz[0]
				var tzh, tzm string
				switch {
				case strings.Contains(tz, ":"):
					parts := strings.SplitN(tz[1:], ":", 2)
					tzh, tzm = parts[0], parts[1]
				case len(tz) == 4:
					tzh, tzm = tz[1:2], tz[2:4]
				default:
					tzh, tzm = tz[1:3], tz[3:5]
				}
				hh, errH := strconv.Atoi(tzh)
				mm, errM := strconv.Atoi(tzm)
				if errH != nil || errM != nil {
					return nil, fmt.Errorf("fmtparse: bad timezone offset %q", tz)
				}
				offset := mm + hh*60
				if sign == '-' {
					offset = -offset
				}
				loc = time.FixedZone(tz, offset*60)
			}
		}

		if timeOnly {
			return time.Date(1, 1, 1, H, M, S, us*1000, loc), nil
		}
		return time.Date(y, time.Month(m), d, H, M, S, us*1000, loc), nil
	}
}
