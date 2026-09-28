package fmtparse

import "testing"

// BenchmarkParsePackageLevel recompiles the format string on every call —
// the cost of calling the package-level Parse in a loop instead of
// Compile-ing once.
func BenchmarkParsePackageLevel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Parse("{name} scored {score:d} points", "Alice scored 42 points"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParsePrecompiled reuses a single compiled Parser, which is the
// pattern recommended in the README for parsing many strings against one
// format.
func BenchmarkParsePrecompiled(b *testing.B) {
	p, err := Compile("{name} scored {score:d} points")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Parse("Alice scored 42 points"); err != nil {
			b.Fatal(err)
		}
	}
}
