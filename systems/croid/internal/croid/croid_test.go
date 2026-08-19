package croid

import (
	"strings"
	"testing"
)

func TestGenerateLengthAndAlphabet(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		g, err := Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if len(g) != Length {
			t.Fatalf("length = %d, want %d (croid=%q)", len(g), Length, g)
		}
		if !Valid(g) {
			t.Fatalf("generated CROID is not valid: %q", g)
		}
		seen[g] = true
	}
	if len(seen) == 1 {
		t.Fatal("all 1000 generated CROIDs are identical")
	}
}

func TestGenerateUsesOnlyAlphabet(t *testing.T) {
	g, _ := Generate()
	for _, r := range g {
		if !strings.ContainsRune(Alphabet, r) {
			t.Fatalf("CROID %q contains out-of-alphabet rune %q", g, r)
		}
	}
}

func TestValid(t *testing.T) {
	runs := func(n int) string { return strings.Repeat("a", n) }

	cases := []struct {
		in   string
		want bool
	}{
		{"a0-Z_x-_01234567890123456789012a", true}, // 32 valid chars
		{runs(32), true}, // 32 a's
		{"", false},
		{"a", false},
		{runs(31) + " ", false}, // 32 chars, space not in alphabet
		{runs(31) + "!", false}, // 32 chars, bang not in alphabet
		{runs(33), false},       // wrong length
	}
	for i, c := range cases {
		if got := Valid(c.in); got != c.want {
			t.Errorf("case %d: Valid(%q) = %v, want %v", i, c.in, got, c.want)
		}
	}
}
