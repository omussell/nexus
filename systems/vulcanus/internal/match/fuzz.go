// Fuzzy string matching wrappers around github.com/adrg/strutil.  All return
// values are in [0, 100] to match Python's thefuzz / fuzzywuzzy library.
package match

import (
	"sort"
	"strings"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
)

// levenshtein is the edit-distance measurer used by all fuzzy scores. It
// must be built with metrics.NewLevenshtein (insert/delete/replace cost 1);
// the zero value gives every edit cost 0, making all distances 0 and every
// similarity 1.
var levenshtein = metrics.NewLevenshtein()

// Ratio returns the Levenshtein-based similarity between a and b as a
// percentage in [0, 100].
func Ratio(a, b string) float64 {
	return strutil.Similarity(a, b, levenshtein) * 100.0
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
		s := strutil.Similarity(sub, b, levenshtein)
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
// highest-scoring partial alignment with target occurs. SrcStart and SrcEnd
// are always coordinates within source (the first argument), matching
// rapidfuzz's partial_ratio_alignment src_* semantics: when target is longer
// than source, source aligns in full, so the range is [0, len(source)].
func FindPartialRatioAlignment(source, target string) PartialRatioAlignment {
	longer, shorter := source, target
	sourceIsLonger := len(source) >= len(target)
	if !sourceIsLonger {
		longer, shorter = target, source
	}
	if len(longer) == 0 || len(shorter) == 0 {
		return PartialRatioAlignment{
			SrcStart: 0,
			SrcEnd:   len(source),
			Target:   source,
			Score:    100.0,
		}
	}
	best := 0.0
	bestStart := 0
	bestTarget := ""
	for i := 0; i <= len(longer)-len(shorter); i++ {
		sub := longer[i : i+len(shorter)]
		s := strutil.Similarity(sub, shorter, levenshtein)
		if s > best {
			best = s
			bestStart = i
			bestTarget = sub
		}
	}
	srcStart, srcEnd := bestStart, bestStart+len(shorter)
	if !sourceIsLonger {
		// The window lies in target; source aligns with it in full.
		srcStart, srcEnd = 0, len(source)
	}
	return PartialRatioAlignment{
		SrcStart: srcStart,
		SrcEnd:   srcEnd,
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
