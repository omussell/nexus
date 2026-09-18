// Fuzzy string matching wrappers around github.com/adrg/strutil.  All return
// values are in [0, 100] to match Python's thefuzz / fuzzywuzzy library.
package match

import (
	"sort"
	"strings"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
)

// Ratio returns the Levenshtein-based similarity between a and b as a
// percentage in [0, 100].
func Ratio(a, b string) float64 {
	return strutil.Similarity(a, b, &metrics.Levenshtein{}) * 100.0
}

// PartialRatio returns the highest Ratio between any substring of the longer
// string and the shorter string, as a percentage in [0, 100].
func PartialRatio(a, b string) float64 {
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(a) == 0 || len(b) == 0 {
		return 100.0
	}
	best := 0.0
	for i := 0; i <= len(a)-len(b); i++ {
		sub := a[i : i+len(b)]
		s := strutil.Similarity(sub, b, &metrics.Levenshtein{})
		if s > best {
			best = s
		}
	}
	return best * 100.0
}

// PartialRatioAlignment represents the position of a partial match.
type PartialRatioAlignment struct {
	SrcStart int
	SrcEnd   int
	Target   string
	Score    float64
}

// FindPartialRatioAlignment returns the position within source where the
// highest-scoring partial alignment with target occurs.
func FindPartialRatioAlignment(source, target string) PartialRatioAlignment {
	if len(source) < len(target) {
		source, target = target, source
	}
	if len(source) == 0 || len(target) == 0 {
		return PartialRatioAlignment{
			SrcStart: 0,
			SrcEnd:   len(source),
			Target:   source,
			Score:    100.0,
		}
	}
	best := 0.0
	bestStart := 0
	bestEnd := 0
	bestTarget := ""
	for i := 0; i <= len(source)-len(target); i++ {
		sub := source[i : i+len(target)]
		s := strutil.Similarity(sub, target, &metrics.Levenshtein{})
		if s > best {
			best = s
			bestStart = i
			bestEnd = i + len(target)
			bestTarget = sub
		}
	}
	return PartialRatioAlignment{
		SrcStart: bestStart,
		SrcEnd:   bestEnd,
		Target:   bestTarget,
		Score:    best * 100.0,
	}
}

// TokenSortRatio splits a and b into tokens, sorts them, then computes
// the ratio of the sorted token strings. Returns a value in [0, 100].
func TokenSortRatio(a, b string) float64 {
	tokensA := tokenize(a)
	tokensB := tokenize(b)
	sort.Strings(tokensA)
	sort.Strings(tokensB)
	return Ratio(strings.Join(tokensA, " "), strings.Join(tokensB, " "))
}

// tokenize splits s into lowercase tokens (words and non-whitespace runs).
func tokenize(s string) []string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return words
}
