"""Generate cases.json, the differential test fixture for fmtparse, by
running each (format, input) pair through the real Python `parse` library
(github.com/r1chardj0n3s/parse) and recording its canonicalized output.

Run from this directory with parse 1.22.2 installed in a local venv:

    python3 -m venv venv
    ./venv/bin/pip install parse==1.22.2
    ./venv/bin/python gen_cases.py

Each case: {"format", "input", "case_sensitive", "op", "match", "fixed": [...], "named": {...}}
op is "parse" or "search". Values are canonicalized so the Go side can
compare without caring whether Python produced an int/float/Decimal/datetime;
see canon() below and differential_test.go's compareValue.
"""
import json
import datetime
import decimal

import parse


def canon(v):
    if v is None:
        return None
    if isinstance(v, bool):
        return {"k": "bool", "v": v}
    if isinstance(v, int):
        return {"k": "int", "v": v}
    if isinstance(v, decimal.Decimal):
        v = float(v)
    if isinstance(v, float):
        if v != v:
            return {"k": "float", "v": "nan"}
        if v == float("inf"):
            return {"k": "float", "v": "inf"}
        if v == float("-inf"):
            return {"k": "float", "v": "-inf"}
        return {"k": "float", "v": v}
    if isinstance(v, str):
        return {"k": "str", "v": v}
    if isinstance(v, datetime.datetime):
        off = v.utcoffset()
        return {
            "k": "datetime",
            "year": v.year, "month": v.month, "day": v.day,
            "hour": v.hour, "minute": v.minute, "second": v.second,
            "micro": v.microsecond,
            "tz_minutes": int(off.total_seconds() // 60) if off is not None else None,
        }
    if isinstance(v, datetime.time):
        off = v.utcoffset()
        return {
            "k": "time",
            "hour": v.hour, "minute": v.minute, "second": v.second,
            "micro": v.microsecond,
            "tz_minutes": int(off.total_seconds() // 60) if off is not None else None,
        }
    if isinstance(v, dict):
        return {"k": "map", "v": {kk: canon(vv) for kk, vv in v.items()}}
    return {"k": "other", "v": str(v)}


def make_case(fmt, s, case_sensitive=False, op="parse"):
    try:
        p = parse.Parser(fmt, case_sensitive=case_sensitive)
    except Exception as e:
        return {"format": fmt, "input": s, "case_sensitive": case_sensitive, "op": op,
                "compile_error": str(type(e).__name__)}
    try:
        if op == "parse":
            r = p.parse(s)
        else:
            r = p.search(s)
    except Exception as e:
        return {"format": fmt, "input": s, "case_sensitive": case_sensitive, "op": op,
                "eval_error": str(type(e).__name__)}
    if r is None:
        return {"format": fmt, "input": s, "case_sensitive": case_sensitive, "op": op, "match": False}
    return {
        "format": fmt, "input": s, "case_sensitive": case_sensitive, "op": op, "match": True,
        "fixed": [canon(x) for x in r.fixed],
        "named": {k: canon(v) for k, v in r.named.items()},
    }


cases = []

def add(fmt, s, **kw):
    cases.append(make_case(fmt, s, **kw))

# --- basic fixed / named fields ---
add("{}", "hello")
add("{}", "")
add("{} {}", "hello world")
add("{name}", "Alice")
add("{name} scored {score:d} points", "Alice scored 42 points")
add("{name} scored {score:d} points", "Alice scored forty points")
add("{}-{}-{}", "2020-01-15")
add("Hello, {}!", "Hello, World!")
add("Hello, {}!", "Hello World!")  # missing comma -> no match

# --- escapes ---
add("{{literal}}", "{literal}")
add("{{{}}}", "{x}")
add("a{{b}}c", "a{b}c")

# --- dotted / indexed names ---
add("{a.b} {a.c}", "1 2")
add("{a[b]} {a[c]}", "1 2")
add("{a[b][c]}", "42")
add("{a.b.c}", "hi")
add("{x.y} and {a[k]}", "foo and bar")

# --- repeated names ---
add("{a}-{a}", "x-x")
add("{a}-{a}", "x-y")
add("{a:d}-{a:d}", "5-5")
add("{a:d}-{a:d}", "5-6")

# --- alignment / fill / width ---
for spec, s in [
    (">10d", "        42"),
    ("<10d", "42        "),
    ("^10d", "    42    "),
    ("*>10d", "********42"),
    ("*<10d", "42********"),
    ("*^10d", "****42****"),
    ("0>5d", "00042"),
    ("05d", "00042"),
    (">5d", "   42"),
    (".5s", "hello"),
    (".3s", "hello"),
    ("10.3s", "       hel"),
    ("5s", None),
]:
    if s is None:
        continue
    add("{:%s}" % spec, s)
add("{:.5}", "hello world")
add("{:10}", "hello     ")

# --- integers: d, n, x/X, o, b ---
for s in ["42", "-42", "+42", "0", "007", "0x1A", "0X1a", "-0x1A", "0o17", "0b101", "  42", "1a2b"]:
    add("{:d}", s)
add("{:n}", "1,234,567")
add("{:n}", "1.234.567")
add("{:n}", "1234567")
add("{:n}", "-1,234")
for s in ["1a", "ff", "FF", "0xff", "-1a", "xyz"]:
    add("{:x}", s)
    add("{:X}", s)
for s in ["17", "0o17", "8", "0"]:
    add("{:o}", s)
for s in ["101", "0b101", "2"]:
    add("{:b}", s)

# --- floats: f, F, e, E, g, G, % ---
for s in ["3.14", "-3.14", "0.0", "nan", "NAN", "inf", "INF", "-inf", "3", "3."]:
    add("{:f}", s)
    add("{:F}", s)
for s in ["1.5e10", "1.5E-10", "-2.0e+5", "nan", "inf"]:
    add("{:e}", s)
for s in ["1.5e10", "42", "42.5", "nan", "inf"]:
    add("{:g}", s)
for s in ["50%", "3.5%", "100%"]:
    add("{:%}", s)
add("{:.0f}", "20")
add("{:.0f}", "20.0")

# --- w, W, l, s, S ---
add("{:w}", "hello_world123")
add("{:w}", "hello world")
add("{:W}", "!!!")
add("{:W}", "abc")
add("{:l}", "HelloWorld")
add("{:l}", "Hello World")
add("{:s}", "   ")
add("{:s}", "hi")
add("{:S}", "hi")
add("{:S}", "  ")

# --- case sensitivity ---
add("HELLO {}", "hello world", case_sensitive=True)
add("HELLO {}", "HELLO world", case_sensitive=True)
add("HELLO {}", "hello world", case_sensitive=False)
add("{:l}", "hello", case_sensitive=True)

# --- datetime types ---
add("{:ti}", "2013-01-01 12:30:45")
add("{:ti}", "2013-01-01T12:30:45Z")
add("{:ti}", "2013-01-01T12:30:45.123456+05:30")
add("{:ti}", "2013-01-01")
add("{:te}", "Mon, 21 Nov 2016 13:45:28 +0000")
add("{:te}", "21 Nov 2016 13:45:28 +0000")
add("{:tg}", "21-11-2016 13:45 +0100")
add("{:tg}", "21-Nov-2016")
add("{:tg}", "5/6/2013 12:15 AM")
add("{:ta}", "11-21-2016 1:45 PM")
add("{:ta}", "Nov-21-2016")
add("{:th}", "10/Oct/2000:13:55:36 -0700")
add("{:tc}", "Sun Jun 20 23:21:05 1993")
add("{:tt}", "12:15 AM")
add("{:tt}", "12:15 PM")
add("{:tt}", "23:59:59")
add("{:tt}", "")

# --- search / findall style (op=search) ---
add("hello {}!", "say hello world!", op="search")
add("{:d}", "abc 42 def", op="search")
add("{:d}", "no digits here", op="search")

# --- non-matches / negative controls ---
add("{}-{}-{}", "2020-01")
add("{:d}", "not a number")
add("exact {} match", "exact xyz matches")
add("{a}-{b}", "x")
add("{:5d}", "123456")

# --- sign handling ---
for spec, s in [("+d", "+42"), ("+d", "-42"), ("+d", "42"), (" d", " 42"), (" d", "-42")]:
    add("{:%s}" % spec, s)

# --- more width/precision combinations ---
add("{:8.2f}", "    3.14")
add("{:<8.2f}x", "3.14    x")
add("{:0=8d}", "00000042")
add("{:0=8d}", "-0000042")

# --- more string/char-class edge cases ---
add("{:D}", "abc")
add("{:D}", "123")
add("{a-b}", "1")
add("{a_b}", "1")
add("{a_b}-{a_b}", "1-1")
add("{a_b}-{a_b}", "1-2")

# --- percent / negative floats ---
add("{:%}", "-5%")
add("{:.2%}", "12.34%")
add("{:f}", "-0.5")
add("{:e}", "-1.5e-10")

# --- month name case-insensitivity in dates ---
# NOTE: lowercase month names here make upstream Python's date_convert raise
# an uncaught KeyError (MONTHS_MAP only has capitalized keys) instead of
# returning None or a Result. That is a real upstream defect, reproduced
# separately in fmtparse's own tests (as a returned error, not a crash),
# not included in this differential set since Python cannot produce a
# comparable expected value for it.
add("{:tg}", "21-Nov-2016")
add("{:th}", "10/Oct/2000:13:55:36 -0700", op="search")

# --- more findall/search positions ---
add("a{:d}b{:d}c", "a1b2c", op="search")
add("<{}>", "before <inside> after", op="search")

# --- unicode-ish literal text (no special fill) ---
add("café {}", "café noir")

# --- custom width digit-only field with no type ---
add("{:5}", "abcde")
add("{:.2}", "abcdef")

# --- fixed + named mixed ---
add("{} likes {name}", "Bob likes pizza")
add("{0} and {1}", "1")  # digit "names" are anonymous in parse.py too
add("{:d} and {:d}", "1 and 2")

cases = [c for c in cases if c is not None]
with open("cases.json", "w") as f:
    json.dump(cases, f, indent=1)
print(f"wrote {len(cases)} cases")
