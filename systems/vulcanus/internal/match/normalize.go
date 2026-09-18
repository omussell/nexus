// Package normalize handles string normalization for ROR matching.
package match

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// New returns a StringRep for s, fully normalised (deaccented, lowercased,
// whitespace-collapsed).  This is the entry-point for all matching code.
func New(s string) *StringRep {
	if s == "" {
		return &StringRep{s: s, normalized: ""}
	}
	return &StringRep{
		s:        s,
		normalized: norm(s),
	}
}

// StringRep holds normalised forms of a string so we don't re-normalise on
// every comparison.
type StringRep struct {
	s        string
	normalized string
}

// Original returns the string as passed in.
func (r *StringRep) Original() string {
	return r.s
}

// Lower returns s lowercased (Unicode-aware).
func (r *StringRep) Lower() string {
	return strings.ToLower(r.s)
}

// Normalized returns the fully normalised form.
func (r *StringRep) Normalized() string {
	return r.normalized
}

// Len returns the rune length of s.
func (r *StringRep) Len() int {
	return utf8.RuneCountInString(r.s)
}

// AlphaLower returns s with all non-alpha characters stripped and lowercased.
func (r *StringRep) AlphaLower() string {
	var b strings.Builder
	for _, r := range strings.ToLower(r.s) {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Alpha returns s with all non-alpha characters stripped (preserving case).
func (r *StringRep) Alpha() string {
	var b strings.Builder
	for _, r := range r.s {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// norm is the full normalisation pipeline.
func norm(s string) string {
	out := normWhitespace(s)
	out = deaccentAndLower(out)
	return out
}

// deaccentAndLower converts accented characters to their ASCII equivalent
// and lowercases the result.
func deaccentAndLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		r = deaccent(r)
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// deaccent strips diacritics from a rune (e.g. à -> a).
// When a mapping produces multiple runes (e.g. compose→decompose), we process
// each resulting rune through the loop until no further decomposition occurs.
func deaccent(r rune) rune {
	for r != 0 {
		if r > 0x7E {
			newR := mapping[r]
			if newR == 0 || newR == r {
				return r
			}
			// If mapping produced a simple ASCII char, return it directly.
			// Otherwise (decomposed form), the caller will process the next rune
			// from the decomposition, so we signal continuation.
			if newR <= 0x7E {
				return newR
			}
			r = newR
		} else {
			return r
		}
	}
	return r
}

// mapping is a partial table of accented characters and their ASCII
// equivalents.  It covers the most common cases seen in ROR data.
var mapping = map[rune]rune{
	// Latin-1 Supplement
	0xE0: 'a', 0xE1: 'a', 0xE2: 'a', 0xE3: 'a', 0xE4: 'a', 0xE5: 'a',
	0xE6: 'c', 0xE7: 'c', 0xE8: 'c', 0xE9: 'e', 0xEA: 'e', 0xEB: 'e',
	0xEC: 'i', 0xED: 'i', 0xEE: 'i', 0xEF: 'i',
	0xF0: 'd', 0xF1: 'n',
	0xF2: 'o', 0xF3: 'o', 0xF4: 'o', 0xF5: 'o', 0xF6: 'o', 0xF8: 'o',
	0xF9: 'u', 0xFA: 'u', 0xFB: 'u', 0xFC: 'u',
	0xFD: 'y', 0xFF: 'y',
	// Latin Extended
	0x101: 'a', 0x103: 'a', 0x105: 'a',
	0x107: 'c', 0x109: 'c', 0x10B: 'c', 0x10D: 'c',
	0x10F: 'd', 0x111: 'd',
	0x113: 'e', 0x115: 'e', 0x117: 'e', 0x119: 'e', 0x11B: 'e',
	0x11D: 'g', 0x11F: 'g', 0x121: 'g', 0x123: 'g',
	0x125: 'h', 0x127: 'h', 0x129: 'i', 0x12B: 'i', 0x12D: 'i',
	0x12F: 'i', 0x131: 'i', 0x133: 'i', 0x135: 'i',
	0x137: 'k', 0x13A: 'l', 0x13C: 'l', 0x13E: 'l', 0x140: 'l', 0x142: 'l',
	0x143: 'n', 0x145: 'n', 0x147: 'n', 0x149: 'n', 0x14B: 'n', 0x14D: 'n',
	0x14F: 'o', 0x151: 'o', 0x153: 'o', 0x155: 'o', 0x157: 'o', 0x159: 'r',
	0x15B: 'r', 0x15D: 'r', 0x15F: 'r', 0x161: 'r',
	0x163: 's', 0x165: 's', 0x167: 's', 0x169: 's', 0x16B: 's', 0x16D: 's',
	0x16F: 't', 0x171: 't', 0x173: 't',
	0x175: 'u', 0x177: 'u', 0x179: 'u', 0x17B: 'u', 0x17D: 'u', 0x17F: 'u',
	0x181: 't', 0x192: 'f',
	// Greek & Cyrillic (common in transliterations)
	0x03B1: 'a', 0x03B2: 'b', 0x03B3: 'g', 0x03B4: 'd',
	0x03B5: 'e', 0x03B6: 'z', 0x03B7: 'e', 0x03B8: 'θ',
	0x03B9: 'i', 0x03BA: 'k', 0x03BB: 'l', 0x03BC: 'm',
	0x03BD: 'n', 0x03BE: 'x', 0x03BF: 'o', 0x03C1: 'r',
	0x03C3: 's', 0x03C4: 't', 0x03C5: 'u', 0x03C6: 'φ',
	0x03C7: 'x', 0x03C8: 'ψ', 0x03C9: 'o',
}

// normWhitespace collapses runs of whitespace into a single space and
// trims leading/trailing whitespace.
func normWhitespace(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " ")
}

// CheckLatin reports whether every alphabetic character in s is Latin.
// Non-alpha characters (digits, punctuation) are skipped.
func CheckLatin(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return false
		}
	}
	return true
}

var abbrPattern = regexp.MustCompile(`(?i)(\b\w+\.?)([ ,;.]|$)`)

// ExpandSuffix expands common ROR abbreviation suffixes to their full forms.
// This is applied during matching to enable searches like "Univ. of Tech."
// to match "University of Technology".
func ExpandSuffix(s string) string {
	abbrevs := map[string]string{
		"univ.": "university",
		"inst.": "institute",
		"tech.": "technology",
		"lab.":  "laboratory",
		"dept.": "department",
		"prof.": "professor",
		"st.":   "saint",
		"mr.":   "mister",
		"ms.":   "miss",
		"mt.":   "mount",
		"av.":   "avenue",
		"blvd.": "boulevard",
		"dr.":   "drive",
		"ln.":   "lane",
		"pkwy.": "parkway",
	}
	for abbr, full := range abbrevs {
		// Build a case-insensitive word-boundary regex for each abbreviation.
		re := regexp.MustCompile(`(?i)(^|[^a-z])` + regexp.QuoteMeta(abbr) + `(?:\s|\.|\,|$)`)
		s = re.ReplaceAllString(s, "$1"+full+"$2")
	}
	return s
}
