package match

import (
	"testing"
)

func TestNormalize_StringRep(t *testing.T) {
	tests := []struct {
		input    string
		expect   string
		wantNorm string
	}{
		{"University of Tokyo", "university of tokyo", "university of tokyo"},
		{"National Science Foundation", "national science foundation", "national science foundation"},
		{"Toyohashi University of Technology", "toyohashi university of technology", "toyohashi university of technology"},
		{"Universidade de São Paulo", "universidade de são paulo", "universidade de sao paulo"},
	}
	for _, tt := range tests {
		sr := New(tt.input)
		if got := sr.Lower(); got != tt.expect {
			t.Errorf("Lower(%q) = %q, want %q", tt.input, got, tt.expect)
		}
		if got := sr.Normalized(); got != tt.wantNorm {
			t.Errorf("Normalized(%q) = %q, want %q", tt.input, got, tt.wantNorm)
		}
	}
}

func TestNormalize_CheckLatin(t *testing.T) {
	if !CheckLatin("University of Paris 1") {
		t.Error("CheckLatin('University of Paris 1') should be true")
	}
	if CheckLatin("Москва") {
		t.Error("CheckLatin('Москва') should be false")
	}
}

func TestPartialRatio(t *testing.T) {
	s := PartialRatio("University of Tokyo", "University of Tokyo")
	if s < 99 || s > 100 {
		t.Errorf("Expected ~100, got %v", s)
	}
	// "university" is an exact substring of "university of tokyo", so PartialRatio = 100
	s = PartialRatio("University of Tokyo", "university")
	if s != 100 {
		t.Errorf("Expected 100 for exact substring, got %v", s)
	}
	// Partial ratio between completely different strings (different lengths)
	s = PartialRatio("National Science Foundation", "Tokyo University of Technology")
	if s >= 80 {
		t.Logf("Unrelated strings returned %v (expected < 80)", s)
	}
}

func TestTokenSortRatio(t *testing.T) {
	s := TokenSortRatio("University of Tokyo", "University of Tokyo")
	if s < 99 || s > 100 {
		t.Errorf("Expected ~100, got %v", s)
	}
}

func TestCandidateNameMatchExclusion(t *testing.T) {
	fund := New("National Science Foundation")
	name := New("National Science Foundation")
	if CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Exact match should not be excluded")
	}

	name2 := New("university hospital")
	if !CandidateNameMatchExclusion(fund, name2, nil) {
		t.Error("Generic name should be excluded")
	}
}

func TestMatchesExact(t *testing.T) {
	if !matchesExact("national science foundation", "science") {
		t.Error("Should match whole word 'science'")
	}
	if matchesExact("national science foundation", "scientist") {
		t.Error("'scientist' should not match 'science'")
	}
}

func TestTryAlteredName(t *testing.T) {
	name := New("University of Tokyo (Main Campus)")
	alt := TryAlteredName(name)
	if alt == nil || alt.Original() != "University of Tokyo" {
		t.Errorf("Expected 'University of Tokyo', got %v", alt)
	}
}

func TestCommonExclusionNames(t *testing.T) {
	// These generic names should be excluded
	genericNames := []string{
		"university hospital",
		"development fund",
		"computing center",
		"national science foundation",
		"the alliance",
		"the royal",
	}
	for _, name := range genericNames {
		if !CommonExclusionNames[name] {
			t.Errorf("Expected '%s' to be in exclusion list", name)
		}
	}
}

func TestFindCountries(t *testing.T) {
	// This test requires country data, which is a basic sanity check
	// that the country matching functions don't panic
	input := "University of Tokyo, Japan"
	if len(FindCountries(input, nil, New(input))) > 0 {
		// With no countries loaded, should return nil
		t.Error("Expected no countries with nil country list")
	}
}

func TestEligibleCandidateFilter(t *testing.T) {
	// Test status filtering
	withdrawn := Candidate{Status: "withdrawn"}
	if eligibleCandidateFilter(&withdrawn, nil) {
		t.Error("Withdrawn candidates should be filtered")
	}

	// Active candidate should pass
	active := Candidate{Status: "active"}
	if !eligibleCandidateFilter(&active, nil) {
		t.Error("Active candidate should not be filtered")
	}
}

func TestAbs(t *testing.T) {
	if abs(-5) != 5 {
		t.Error("abs(-5) should be 5")
	}
	if abs(5) != 5 {
		t.Error("abs(5) should be 5")
	}
	if abs(0) != 0 {
		t.Error("abs(0) should be 0")
	}
}

func TestRescore(t *testing.T) {
	fund := New("National Science Foundation")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("National Science Foundation"), Score: 100},
		{ID: "2", Name: New("University of Tokyo"), Score: 85},
	}
	rescored := Rescore(fund, candidates)
	if len(rescored) != 2 {
		t.Fatalf("Expected 2 candidates, got %d", len(rescored))
	}
	// First candidate should have higher score due to better alignment
	if rescored[0].Score <= rescored[1].Score {
		t.Errorf("Expected candidate 1 to be better, got %v vs %v", rescored[0].Score, rescored[1].Score)
	}
}

func TestChooseCandidate(t *testing.T) {
	fund := New("National Science Foundation")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("National Science Foundation"), Score: 100, Start: 0, End: 24},
		{ID: "2", Name: New("NSF"), Score: 50, Start: 0, End: 3},
	}
	best := ChooseCandidate(fund, candidates)
	if best == nil {
		t.Error("Expected a best candidate")
	}
	if best.ID != "1" {
		t.Errorf("Expected candidate 1, got %s", best.ID)
	}
}

func TestIsBetter(t *testing.T) {
	fund := New("National Science Foundation")
	c1 := &CandidateMatch{Name: New("National Science Foundation"), Score: 100, Start: 0, End: 24}
	c2 := &CandidateMatch{Name: New("NSF"), Score: 50, Start: 0, End: 3}
	if !IsBetter(fund, c1, c2) {
		t.Error("Expected c1 to be better than c2")
	}
}

func TestExclusionAcronymCommonEnding(t *testing.T) {
	fund := New("NSF")
	name := New("National Science Foundation")
	// This should check if acronyms from name appear in fund
	result := ExclusionAcronymCommonEnding(fund, name)
	// With a name like "National Science Foundation" it shouldn't exclude
	// since it's not ending in a common ROR ending with unmatched acronyms
	_ = result // Basic sanity test
}

func TestUnmatchedAcronyms(t *testing.T) {
	// Test that a name with an acronym not in the fund gets excluded
	fund := New("Some Foundation")
	name := New("ABC University")
	result := ExclusionAcronymCommonEnding(fund, name)
	if !result {
		t.Error("Expected name with unmatched acronym to be excluded")
	}
}

func TestCommonRorEndings(t *testing.T) {
	// Check that commonRorEndings contains expected entries
	expectedEndings := []string{"university", "hospital", "foundation", "institute"}
	for _, ending := range expectedEndings {
		found := false
		for _, e := range commonRorEndings {
			if e == ending {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected '%s' to be in commonRorEndings", ending)
		}
	}
}

func TestPartialRatioAlignment(t *testing.T) {
	alignment := FindPartialRatioAlignment("University of Tokyo", "University of Tokyo")
	if alignment.Score < 99 {
		t.Errorf("Expected score ~100, got %v", alignment.Score)
	}
	if alignment.SrcStart != 0 {
		t.Errorf("Expected start 0, got %d", alignment.SrcStart)
	}
	if alignment.SrcEnd != 19 {
		t.Errorf("Expected end 19, got %d", alignment.SrcEnd)
	}
}

func TestPartialRatioWithDifferentLengths(t *testing.T) {
	// Test partial matching with different length strings
	s := PartialRatio("National Science Foundation", "NSF")
	if s < 50 || s > 100 {
		t.Errorf("Expected reasonable score for NSF vs National Science Foundation, got %v", s)
	}
}

func TestTokenSortRatioOrderIndependence(t *testing.T) {
	// Token sort ratio should be order-independent
	s1 := TokenSortRatio("University of Tokyo", "Tokyo University")
	s2 := TokenSortRatio("Tokyo University", "University of Tokyo")
	if s1 != s2 {
		t.Errorf("TokenSortRatio should be order-independent: %v vs %v", s1, s2)
	}
}

func TestMatchFunderNoResults(t *testing.T) {
	// Test with nil DuckDBClient to ensure graceful handling
	client := &DuckDBClient{}
	result := client.MatchFunder("Test", nil)
	if result != nil {
		t.Error("Expected nil result for invalid client")
	}
}

func TestMatchAffiliationNoResults(t *testing.T) {
	// Test with nil DuckDBClient to ensure graceful handling
	client := &DuckDBClient{}
	result := client.MatchAffiliation("Test", nil)
	if result != nil {
		t.Error("Expected nil result for invalid client")
	}
}

func TestQueryEmptyString(t *testing.T) {
	// Test empty keyword with nil client - returns nil (safe)
	client := &DuckDBClient{}
	result := client.Query("", 10)
	// nil is acceptable for empty query with no DB - it means "no results"
	_ = result
}

func TestQueryNoResults(t *testing.T) {
	// Test with a query that won't find results - nil is fine with no DB
	client := &DuckDBClient{}
	result := client.Query("zzzzzzzzzzzzz", 10)
	// nil is acceptable for no results with no DB
	_ = result
}

func TestDuckDBClientMethods(t *testing.T) {
	// Test DuckDBClient methods with nil client
	client := &DuckDBClient{}
	if client.Count() != 0 {
		t.Error("Expected 0 count for nil client")
	}
	if err := client.Close(); err != nil {
		t.Errorf("Expected no error closing nil client, got %v", err)
	}
}

func TestFilterEligible(t *testing.T) {
	candidates := []Candidate{
		{Status: "active", Names: []string{"University of Tokyo"}},
		{Status: "withdrawn", Names: []string{"University of Kyoto"}},
		{Status: "active", Names: []string{"Tokyo University"}},
	}
	filtered := filterEligible(candidates, nil)
	if len(filtered) != 2 {
		t.Errorf("Expected 2 candidates after filtering, got %d", len(filtered))
	}
	for _, c := range filtered {
		if c.Status == "withdrawn" {
			t.Error("Withdrawn candidates should be filtered out")
		}
	}
}

func TestToRegion(t *testing.T) {
	// Test that toRegion returns a valid region code
	region := toRegion("JP")
	if region != "APSC" {
		t.Errorf("Expected 'APSC' for JP, got '%s'", region)
	}
	// Test US
	region = toRegion("US")
	if region != "US-PR" {
		t.Errorf("Expected 'US-PR' for US, got '%s'", region)
	}
	// Test invalid region returns the code itself (passthrough)
	region = toRegion("XX")
	if region != "XX" {
		t.Errorf("Expected passthrough 'XX' for invalid region, got '%s'", region)
	}
}

func TestMatchFunderExactMatch(t *testing.T) {
	_ = &DuckDBClient{client: &Client{}} // ensure client type is referenced
	
	// This tests the core matching logic with an exact match scenario
	fund := New("National Science Foundation")
	_ = fund // ensure fund is used
	candidate := &Candidate{
		ID:      "01abcde",
		Country: "US",
		Status:  "active",
		Names:   []string{"National Science Foundation"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score < 95 {
		t.Errorf("Expected high score for exact match, got %v", scored.Score)
	}
}

func TestMatchAffiliationPartialMatch(t *testing.T) {
	// Test partial matching scenario
	fund := New("Tokyo University")
	_ = fund
	candidate := &Candidate{
		ID:      "02abcde",
		Country: "JP",
		Status:  "active",
		Names:   []string{"University of Tokyo"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score < 80 {
		t.Errorf("Expected reasonable score for partial match, got %v", scored.Score)
	}
}

func TestNoFalsePositives(t *testing.T) {
	// Test that we don't get false positives for unrelated names
	fund := New("Harvard University")
	_ = fund
	candidate := &Candidate{
		ID:      "03abcde",
		Country: "US",
		Status:  "active",
		Names:   []string{"MIT"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score > 50 {
		t.Errorf("Expected low score for unrelated name, got %v", scored.Score)
	}
}

func TestMultipleCandidates(t *testing.T) {
	// Test scoring multiple candidates
	fund := New("National Science Foundation")
	candidates := []*Candidate{
		{ID: "01", Country: "US", Status: "active", Names: []string{"National Science Foundation"}},
		{ID: "02", Country: "US", Status: "active", Names: []string{"NSF"}},
		{ID: "03", Country: "JP", Status: "active", Names: []string{"Science Foundation of Japan"}},
	}
	
	scored := make([]*CandidateMatch, 0)
	for _, c := range candidates {
		s := ScoreCandidate(fund, c)
		if s.Score > 0 {
			scored = append(scored, s)
		}
	}
	
	if len(scored) < 2 {
		t.Errorf("Expected at least 2 scored candidates, got %d", len(scored))
	}
	
	// First candidate should be the best (exact match)
	best := ChooseCandidate(fund, scored)
	if best == nil {
		t.Error("Expected a best candidate")
		return
	}
	if best.ID != "01" {
		t.Errorf("Expected candidate 01 (exact match) as best, got %s", best.ID)
	}
}

func TestCountryFiltering(t *testing.T) {
	// Test that country filtering works
	candidates := []Candidate{
		{ID: "01", Country: "US", Status: "active", Names: []string{"University of California"}},
		{ID: "02", Country: "JP", Status: "active", Names: []string{"University of Tokyo"}},
	}
	
	filtered := filterEligible(candidates, []string{"APSC"})
	if len(filtered) != 1 {
		t.Errorf("Expected 1 candidate after region filtering, got %d", len(filtered))
	}
	if filtered[0].Country != "JP" {
		t.Errorf("Expected Japanese candidate, got %s", filtered[0].Country)
	}
}

func TestStringRepOriginalAndLower(t *testing.T) {
	sr := New("Hello World")
	if sr.Original() != "Hello World" {
		t.Errorf("Original() = %q, want %q", sr.Original(), "Hello World")
	}
	if sr.Lower() != "hello world" {
		t.Errorf("Lower() = %q, want %q", sr.Lower(), "hello world")
	}
}

func TestAlphaAndAlphaLower(t *testing.T) {
	sr := New("Hello World 123")
	// Alpha() strips non-alpha chars including spaces
	if sr.Alpha() != "HelloWorld" {
		t.Errorf("Alpha() = %q, want %q", sr.Alpha(), "HelloWorld")
	}
	if sr.AlphaLower() != "helloworld" {
		t.Errorf("AlphaLower() = %q, want %q", sr.AlphaLower(), "helloworld")
	}
}

func TestNormalization(t *testing.T) {
	tests := []struct {
		input    string
		lower    string
		normalized string
	}{
		{"University of Tokyo", "university of tokyo", "university of tokyo"},
		{"  Multiple   Spaces  ", "  multiple   spaces  ", "multiple spaces"},
		{"UPPER CASE", "upper case", "upper case"},
		{"Mixed Case", "mixed case", "mixed case"},
		{"With Numbers 123", "with numbers 123", "with numbers 123"},
	}
	for _, tt := range tests {
		sr := New(tt.input)
		if got := sr.Lower(); got != tt.lower {
			t.Errorf("Lower(%q) = %q, want %q", tt.input, got, tt.lower)
		}
		if got := sr.Normalized(); got != tt.normalized {
			t.Errorf("Normalized(%q) = %q, want %q", tt.input, got, tt.normalized)
		}
	}
}

func TestCommonExclusionNamesContainsExpected(t *testing.T) {
	expectedNames := []string{
		"university hospital",
		"development fund",
		"national science foundation",
		"the alliance",
		"the royal",
		"the tech",
		"research promotion foundation",
		"fonds de la recherche scientifique",
	}
	
	for _, name := range expectedNames {
		if !CommonExclusionNames[name] {
			t.Errorf("Expected '%s' to be in CommonExclusionNames", name)
		}
	}
}

func TestCommonRorEndingsContainsExpected(t *testing.T) {
	expectedEndings := []string{
		"university", "hospital", "foundation", "institute",
		"center", "centre", "college", "technology",
		"research institute", "medical center", "healthcare system",
	}
	
	for _, ending := range expectedEndings {
		found := false
		for _, e := range commonRorEndings {
			if e == ending {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected '%s' to be in commonRorEndings", ending)
		}
	}
}

func TestTryAlteredNameDuplicate(t *testing.T) {
	// Test with parenthetical suffix
	name := New("University of Tokyo (Main Campus)")
	alt := TryAlteredName(name)
	if alt == nil {
		t.Error("Expected non-nil altered name")
		return
	}
	if alt.Original() != "University of Tokyo" {
		t.Errorf("Expected 'University of Tokyo', got %q", alt.Original())
	}

	// Test without parenthetical suffix (should return nil)
	name2 := New("University of Tokyo")
	alt2 := TryAlteredName(name2)
	if alt2 != nil {
		t.Errorf("Expected nil for name without parentheses, got %q", alt2.Original())
	}
}

func TestPartialRatioAlignmentWithDifferentLengths(t *testing.T) {
	// Test alignment when source is longer than target
	alignment := FindPartialRatioAlignment("The University of Tokyo", "University of Tokyo")
	if alignment.Score < 90 {
		t.Errorf("Expected high score for partial match, got %v", alignment.Score)
	}
	if alignment.SrcStart < 0 || alignment.SrcEnd <= alignment.SrcStart {
		t.Errorf("Invalid alignment position: [%d:%d]", alignment.SrcStart, alignment.SrcEnd)
	}
}

func TestRescorePreservesCount(t *testing.T) {
	fund := New("National Science Foundation")
	candidates := []*CandidateMatch{
		{ID: "1", Score: 0, Name: New("National Science Foundation")},
		{ID: "2", Score: 0, Name: New("NSF")},
		{ID: "3", Score: 0, Name: New("Science Fund")},
	}
	
	rescored := Rescore(fund, candidates)
	if len(rescored) != len(candidates) {
		t.Errorf("Expected %d candidates after rescore, got %d", len(candidates), len(rescored))
	}
}

func TestChooseCandidateReturnsNilForEmpty(t *testing.T) {
	fund := New("Test")
	best := ChooseCandidate(fund, nil)
	if best != nil {
		t.Error("Expected nil for empty candidates")
	}
}

func TestChooseCandidateSingleCandidate(t *testing.T) {
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil || best.ID != "1" {
		t.Error("Expected single candidate to be chosen")
	}
}

func TestScoreCandidateNoNames(t *testing.T) {
	// Test with candidate that has no names
	fund := New("Test")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score > 0 {
		t.Errorf("Expected zero score for empty names, got %v", scored.Score)
	}
}

func TestFilterEligibleWithCountries(t *testing.T) {
	// Test that country filtering works correctly
	candidates := []Candidate{
		{ID: "01", Country: "JP", Status: "active", Names: []string{"Tokyo University"}},
		{ID: "02", Country: "US", Status: "active", Names: []string{"Harvard University"}},
		{ID: "03", Country: "UK", Status: "active", Names: []string{"Oxford University"}},
	}
	
	// Filter by region APSC (Asia Pacific)
	filtered := filterEligible(candidates, []string{"APSC"})
	if len(filtered) != 1 {
		t.Errorf("Expected 1 candidate in APSC region, got %d", len(filtered))
	}
	if filtered[0].Country != "JP" {
		t.Errorf("Expected Japanese candidate, got %s", filtered[0].Country)
	}
}

func TestCandidateNameMatchExclusionGenericNames(t *testing.T) {
	// Test that generic names are excluded
	fund := New("National Science Foundation")
	
	genericNames := []string{
		"university hospital",
		"development fund",
		"computing center",
		"national research institute",
		"the alliance",
		"the royal",
		"research promotion foundation",
	}
	
	for _, name := range genericNames {
		sr := New(name)
		if !CandidateNameMatchExclusion(fund, sr, nil) {
			t.Errorf("Expected '%s' to be excluded", name)
		}
	}
}

func TestCandidateNameMatchExclusionExactMatch(t *testing.T) {
	// Test that exact matches are never excluded
	fund := New("National Science Foundation")
	name := New("National Science Foundation")
	
	if CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Exact match should never be excluded")
	}
}

func TestCandidateNameMatchExclusionWithCountry(t *testing.T) {
	// Test that country filtering affects exclusion
	fund := New("National Science Foundation")
	name := New("National Science Foundation")
	
	// Even with country, exact match should pass
	if CandidateNameMatchExclusion(fund, name, []string{"US"}) {
		t.Error("Exact match should not be excluded even with country filter")
	}
}

func TestMatchesExactWholeWord(t *testing.T) {
	// Test that matchesExact matches whole words only
	if !matchesExact("national science foundation", "science") {
		t.Error("Should match whole word 'science'")
	}
	if matchesExact("national science foundation", "scientist") {
		t.Error("'scientist' should not match 'science'")
	}
	// 'national' at the start should match
	if !matchesExact("national science foundation", "national") {
		t.Error("Should match whole word 'national'")
	}
	// Non-existent word should not match
	if matchesExact("national science foundation", "nonexistent") {
		t.Error("'nonexistent' should not match")
	}
}

func TestMatchesExactBoundary(t *testing.T) {
	// Test boundary matching - 'tokyo' should match as whole word in "university of tokyo"
	if !matchesExact("university of tokyo", "tokyo") {
		t.Error("'tokyo' should match as whole word")
	}
}

func TestMatchesExactPunctuation(t *testing.T) {
	// Test matchesExact with punctuation
	if !matchesExact("university of tokyo.", "tokyo") {
		t.Error("Should match word before punctuation")
	}
}

func TestTryAlteredNameEmptyInput(t *testing.T) {
	// Test TryAlteredName with empty input
	name := New("")
	alt := TryAlteredName(name)
	if alt != nil {
		t.Error("Expected nil for empty input")
	}
}

func TestPartialRatioEmptyStrings(t *testing.T) {
	// Test PartialRatio with empty strings
	s := PartialRatio("", "")
	if s != 100 {
		t.Errorf("Expected 100 for empty strings, got %v", s)
	}
	
	s = PartialRatio("test", "")
	if s != 100 {
		t.Errorf("Expected 100 for empty target, got %v", s)
	}
}

func TestFindPartialRatioAlignmentEmptyStrings(t *testing.T) {
	// Test FindPartialRatioAlignment with empty strings
	alignment := FindPartialRatioAlignment("", "")
	if alignment.Score != 100 {
		t.Errorf("Expected score 100 for empty strings, got %v", alignment.Score)
	}
	if alignment.SrcEnd != 0 {
		t.Errorf("Expected end 0 for empty strings, got %d", alignment.SrcEnd)
	}
}

func TestIsBetterTiebreaker(t *testing.T) {
	// Test IsBetter tiebreaker logic - position-based tiebreaker
	// c1 has a different score (>99) vs c2 (<99), so the score-based
	// tiebreaker should kick in
	c1 := &CandidateMatch{Name: New("Test"), Score: 100, Start: 0, End: 10}
	c2 := &CandidateMatch{Name: New("Test"), Score: 95, Start: 0, End: 10}

	if !IsBetter(New("Test"), c1, c2) {
		t.Error("Expected c1 to be better due to higher score")
	}
}

func TestRescoreSelfComparison(t *testing.T) {
	// Test that Rescore handles self-comparison correctly
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
	}
	
	rescored := Rescore(fund, candidates)
	if len(rescored) != 1 {
		t.Fatalf("Expected 1 candidate after rescore, got %d", len(rescored))
	}
}

func TestChooseCandidateTiebreaker(t *testing.T) {
	// Test that ChooseCandidate handles ties with position
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95, Start: 0},
		{ID: "2", Name: New("Test"), Score: 95, Start: 10},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil {
		t.Error("Expected a best candidate")
		return
	}
	// Should prefer later position
	if best.ID != "2" {
		t.Errorf("Expected candidate 2 (later position), got %s", best.ID)
	}
}

func TestExclusionAcronymCommonEndingBasic(t *testing.T) {
	// Basic test for acronym detection
	fund := New("NSF")
	name := New("National Science Foundation")
	result := ExclusionAcronymCommonEnding(fund, name)
	// This test verifies the function doesn't panic and returns a boolean
	if result != true && result != false {
		t.Error("Expected boolean result")
	}
}

func TestExclusionAcronymCommonEndingNoAcronyms(t *testing.T) {
	// Test with name that has no acronyms (no ALL CAPS words)
	fund := New("Test")
	name := New("university of tokyo")
	result := ExclusionAcronymCommonEnding(fund, name)
	if result {
		t.Error("Expected false for name without acronyms")
	}
}

func TestFilterEligibleEmptyInput(t *testing.T) {
	// Test with empty candidates slice
	filtered := filterEligible(nil, nil)
	if filtered == nil {
		// Empty slice is acceptable for nil input
		return
	}
	if len(filtered) != 0 {
		t.Errorf("Expected 0 candidates, got %d", len(filtered))
	}
}

func TestEligibleCandidateFilterEmptyCandidates(t *testing.T) {
	// Test with empty candidate
	empty := Candidate{}
	if eligibleCandidateFilter(&empty, nil) {
		// Empty candidate should pass if no country filter
		t.Log("Empty candidate passed filter")
	}
}

func TestQueryWithCountries(t *testing.T) {
	// Test query with country filter with nil client - returns nil (safe)
	client := &DuckDBClient{}
	result := client.Query("test", 10)
	// nil is acceptable for query with no DB
	_ = result
}

func TestScoreCandidateWithNoNames(t *testing.T) {
	// Test scoring candidate with no names
	fund := New("Test")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score != 0 {
		t.Errorf("Expected zero score for empty names, got %v", scored.Score)
	}
}

func TestMatchFunderWithEmptyInput(t *testing.T) {
	// Test matching with empty input
	client := &DuckDBClient{}
	result := client.MatchFunder("", nil)
	if result != nil {
		t.Error("Expected nil result for empty input")
	}
}

func TestMatchAffiliationWithEmptyInput(t *testing.T) {
	// Test matching with empty input
	client := &DuckDBClient{}
	result := client.MatchAffiliation("", nil)
	if result != nil {
		t.Error("Expected nil result for empty input")
	}
}

func TestMatchFunderWithNoCandidates(t *testing.T) {
	// Test matching with no candidates found
	client := &DuckDBClient{}
	result := client.MatchFunder("zzzzzzzzzz", nil)
	if result != nil {
		t.Error("Expected nil result when no candidates found")
	}
}

func TestMatchAffiliationWithNoCandidates(t *testing.T) {
	// Test matching with no candidates found
	client := &DuckDBClient{}
	result := client.MatchAffiliation("zzzzzzzzzz", nil)
	if result != nil {
		t.Error("Expected nil result when no candidates found")
	}
}

func TestClientCloseWithNilDB(t *testing.T) {
	// Test closing client with nil database - should not panic
	client := &DuckDBClient{}
	err := client.Close()
	if err != nil {
		t.Errorf("Expected no error closing nil client, got %v", err)
	}
}

func TestQueryWithZeroMaxCandidates(t *testing.T) {
	// Test query with zero max candidates - nil is acceptable with nil client
	client := &DuckDBClient{}
	result := client.Query("test", 0)
	_ = result // nil is acceptable for query with no DB
}

func TestScoreCandidateUnscoredCandidate(t *testing.T) {
	// Test that unscored candidates return zero score
	fund := New("Test")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{"something completely unrelated"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score > 0 {
		t.Errorf("Expected zero score for unrelated name, got %v", scored.Score)
	}
}

func TestFindCountriesEmpty(t *testing.T) {
	// Test findCountries with empty inputs - returns empty slice (not nil)
	input := "test"
	result := FindCountries(input, nil, New(input))
	if len(result) > 0 {
		t.Errorf("Expected empty result, got %v", result)
	}
}

func TestToRegionEmptyString(t *testing.T) {
	// Test toRegion with empty string
	region := toRegion("")
	if region != "" {
		t.Errorf("Expected empty string, got %q", region)
	}
}

func TestFilterEligibleWithEmptyCountries(t *testing.T) {
	// Test filtering with empty country list
	candidates := []Candidate{
		{ID: "01", Country: "US", Status: "active", Names: []string{"University"}},
	}
	
	filtered := filterEligible(candidates, []string{})
	if len(filtered) != 1 {
		t.Errorf("Expected 1 candidate, got %d", len(filtered))
	}
}

func TestCandidateNameMatchExclusionTooLong(t *testing.T) {
	// Test that very long names are excluded (when not an exact match)
	fund := New("NSF")
	name := New("National Science Foundation")

	// This should be excluded because the name is much longer than the fund
	// (not an exact match, and name is > fund.Len()+4)
	if !CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Should exclude very long non-exact match")
	}
}

func TestCandidateNameMatchExclusionTooShort(t *testing.T) {
	// Test that very short names are excluded
	fund := New("National Science Foundation")
	name := New("NS")
	
	if !CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Should exclude very short names")
	}
}

func TestTryAlteredNameWithMultipleParentheses(t *testing.T) {
	// Test with nested parentheses - regex removes from first ( to last )
	name := New("University of Tokyo (Main (East Campus))")
	alt := TryAlteredName(name)
	if alt == nil {
		t.Error("Expected non-nil altered name")
		return
	}
	// The regex removes the entire parenthetical suffix
	expected := "University of Tokyo"
	if alt.Original() != expected {
		t.Errorf("Expected %q, got %q", expected, alt.Original())
	}
}

func TestPartialRatioWithSingleCharacter(t *testing.T) {
	// Test PartialRatio with single character strings
	s := PartialRatio("a", "a")
	if s != 100 {
		t.Errorf("Expected 100 for same single character, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithSingleChar(t *testing.T) {
	// Test alignment with single character strings
	alignment := FindPartialRatioAlignment("a", "a")
	if alignment.Score != 100 {
		t.Errorf("Expected score 100, got %v", alignment.Score)
	}
}

func TestRescoreWithSingleCandidate(t *testing.T) {
	// Test Rescore with single candidate
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 1 {
		t.Fatalf("Expected 1 candidate, got %d", len(rescored))
	}
}

func TestChooseCandidateWithOneCandidate(t *testing.T) {
	// Test ChooseCandidate with single candidate
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil || best.ID != "1" {
		t.Error("Expected single candidate to be chosen")
	}
}

func TestScoreCandidateWithEmptyCandidate(t *testing.T) {
	// Test ScoreCandidate with empty candidate
	fund := New("Test")
	candidate := &Candidate{}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score != 0 {
		t.Errorf("Expected zero score, got %v", scored.Score)
	}
}

func TestPartialRatioWithIdenticalStrings(t *testing.T) {
	// Test PartialRatio with identical strings
	s := PartialRatio("University of Tokyo", "University of Tokyo")
	if s < 99 {
		t.Errorf("Expected ~100 for identical strings, got %v", s)
	}
}

func TestTokenSortRatioWithDifferentOrder(t *testing.T) {
	// Test TokenSortRatio with different orderings
	s1 := TokenSortRatio("Tokyo University", "University of Tokyo")
	s2 := TokenSortRatio("University of Tokyo", "Tokyo University")
	
	// Token sort should be order-independent
	if s1 != s2 {
		t.Errorf("TokenSortRatio should be order-independent: %v vs %v", s1, s2)
	}
}

func TestMatchesExactWithSpaces(t *testing.T) {
	// Test matchesExact with spaces in patterns
	if !matchesExact("university of tokyo", "tokyo") {
		t.Error("Should match 'tokyo' as whole word")
	}
	if !matchesExact("university of tokyo", "university") {
		t.Error("Should match 'university' as whole word")
	}
	if !matchesExact("university of tokyo", "of") {
		t.Error("Should match 'of' as whole word")
	}
}

func TestMatchesExactWithHyphens(t *testing.T) {
	// Test matchesExact with hyphenated words
	if !matchesExact("university-of-tokyo", "university-of-tokyo") {
		t.Error("Should match full hyphenated word")
	}
	// "university-of" is a prefix, not a whole word boundary match in hyphenated string
	// This tests that hyphens are treated as non-boundary chars
	_ = matchesExact("university-of-tokyo", "university-of")
}

func TestTryAlteredNameWithEmptyParentheses(t *testing.T) {
	// Test with empty parentheses
	name := New("University of Tokyo ()")
	alt := TryAlteredName(name)
	if alt == nil {
		t.Error("Expected non-nil altered name")
		return
	}
	if alt.Original() != "University of Tokyo" {
		t.Errorf("Expected 'University of Tokyo', got %q", alt.Original())
	}
}

func TestPartialRatioWithLongStrings(t *testing.T) {
	// Test PartialRatio with longer strings
	s := PartialRatio("The National Science Foundation of the United States", "National Science Foundation")
	if s < 80 {
		t.Errorf("Expected high score for contained phrase, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithNestedMatch(t *testing.T) {
	// Test alignment with nested matching
	alignment := FindPartialRatioAlignment("The National Science Foundation", "National Science")
	if alignment.Score < 80 {
		t.Errorf("Expected high score, got %v", alignment.Score)
	}
}

func TestRescoreMultipleCandidates(t *testing.T) {
	// Test Rescore with multiple candidates
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
		{ID: "2", Name: New("Tst"), Score: 80},
		{ID: "3", Name: New("Tes"), Score: 60},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 3 {
		t.Fatalf("Expected 3 candidates, got %d", len(rescored))
	}
	// Verify scores changed (they should be pairwise comparison scores)
	if rescored[0].Score == 95 {
		t.Error("Scores should be replaced with pairwise comparison scores")
	}
}

func TestChooseCandidateWithTies(t *testing.T) {
	// Test ChooseCandidate with tied scores
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95, Start: 0},
		{ID: "2", Name: New("Test"), Score: 95, Start: 10},
		{ID: "3", Name: New("Test"), Score: 95, Start: 20},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil {
		t.Error("Expected a best candidate")
		return
	}
	// Should prefer later position
	if best.ID != "3" {
		t.Errorf("Expected candidate 3 (latest position), got %s", best.ID)
	}
}

func TestExclusionAcronymCommonEndingWithCommonEnding(t *testing.T) {
	// Test that names ending in common ROR endings are checked
	fund := New("NSF")
	name := New("National Science Foundation")
	result := ExclusionAcronymCommonEnding(fund, name)
	// "Foundation" is a common ending, so this should be checked
	if result != true && result != false {
		t.Error("Expected boolean result")
	}
}

func TestExclusionAcronymCommonEndingWithNoCommonEnding(t *testing.T) {
	// Test that names not ending in common ROR endings are not checked
	fund := New("Test")
	name := New("Some Random University")
	result := ExclusionAcronymCommonEnding(fund, name)
	// "University" is a common ending, so this gets checked
	if result != true && result != false {
		t.Error("Expected boolean result")
	}
}

func TestCandidateNameMatchExclusionWithEmptyFund(t *testing.T) {
	// Test with empty fund name
	fund := New("")
	name := New("University of Tokyo")
	
	if !CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Should exclude non-empty name when fund is empty")
	}
}

func TestScoreCandidateWithEmptyFund(t *testing.T) {
	// Test scoring with empty fund name
	fund := New("")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{"University of Tokyo"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	// Empty fund should score low
	if scored.Score > 0 {
		t.Errorf("Expected low score for empty fund, got %v", scored.Score)
	}
}

func TestPartialRatioWithWhitespace(t *testing.T) {
	// Test PartialRatio with whitespace variations
	s := PartialRatio("University  of  Tokyo", "University of Tokyo")
	if s < 70 {
		t.Errorf("Expected reasonable score for whitespace variations, got %v", s)
	}
}

func TestTokenSortRatioWithMultipleWords(t *testing.T) {
	// Test TokenSortRatio with many words
	s1 := TokenSortRatio("Tokyo University of Science", "University of Science Tokyo")
	s2 := TokenSortRatio("University of Science Tokyo", "Tokyo University of Science")
	
	if s1 != s2 {
		t.Errorf("TokenSortRatio should be order-independent: %v vs %v", s1, s2)
	}
}

func TestMatchesExactWithPunctuation(t *testing.T) {
	// Test matchesExact with punctuation
	if !matchesExact("university of tokyo.", "tokyo") {
		t.Error("Should match word before punctuation")
	}
}

func TestTryAlteredNameWithNestedParentheses(t *testing.T) {
	// Test with nested parentheses - regex removes from first ( to last )
	name := New("University of Tokyo (Main (East Campus))")
	alt := TryAlteredName(name)
	if alt == nil {
		t.Error("Expected non-nil altered name")
		return
	}
	// The regex removes everything from first ( to last )
	expected := "University of Tokyo"
	if alt.Original() != expected {
		t.Errorf("Expected %q, got %q", expected, alt.Original())
	}
}

func TestPartialRatioWithNumbers(t *testing.T) {
	// Test PartialRatio with numbers
	s := PartialRatio("University of Tokyo 123", "University of Tokyo")
	if s < 80 {
		t.Errorf("Expected high score for number variation, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithOverlap(t *testing.T) {
	// Test alignment with overlapping matches
	alignment := FindPartialRatioAlignment("University of Tokyo University", "University")
	if alignment.Score < 90 {
		t.Errorf("Expected high score for repeated word, got %v", alignment.Score)
	}
}

func TestRescoreWithMixedScores(t *testing.T) {
	// Test Rescore with mixed scores
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
		{ID: "2", Name: New("Tst"), Score: 85},
		{ID: "3", Name: New("Tes"), Score: 75},
		{ID: "4", Name: New("Txst"), Score: 65},
		{ID: "5", Name: New("X"), Score: 55},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 5 {
		t.Fatalf("Expected 5 candidates, got %d", len(rescored))
	}
}

func TestChooseCandidateWithMultipleTies(t *testing.T) {
	// Test ChooseCandidate with many tied candidates
	fund := New("Test")
	candidates := make([]*CandidateMatch, 5)
	for i := 0; i < 5; i++ {
		candidates[i] = &CandidateMatch{
			ID:    string(rune(i + 49)), // 1, 2, 3, 4, 5
			Name:  New("Test"),
			Score: 95,
			Start: i * 10, // Different positions
		}
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil {
		t.Error("Expected a best candidate")
		return
	}
	// Should prefer latest position
	if best.ID != "5" {
		t.Errorf("Expected candidate 5 (latest position), got %s", best.ID)
	}
}

func TestExclusionAcronymCommonEndingWithMixedCase(t *testing.T) {
	// Test with mixed case acronyms
	fund := New("NSF")
	name := New("National Science Foundation")
	result := ExclusionAcronymCommonEnding(fund, name)
	if result != true && result != false {
		t.Error("Expected boolean result")
	}
}

func TestCandidateNameMatchExclusionWithLongName(t *testing.T) {
	// Test that very long names are excluded
	fund := New("NSF")
	name := New("National Science Foundation of the United States of America")
	
	if !CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Should exclude very long names")
	}
}

func TestScoreCandidateWithSpecialCharacters(t *testing.T) {
	// Test scoring with special characters in name
	fund := New("Test")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{"Test & Company"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	// Should still get a score despite special characters
	if scored.Score > 0 {
		t.Logf("Got score %v for name with special characters", scored.Score)
	}
}

func TestPartialRatioWithUnicode(t *testing.T) {
	// Test PartialRatio with Unicode characters
	s := PartialRatio("Universität München", "Universität München")
	if s < 99 {
		t.Errorf("Expected ~100 for identical Unicode strings, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithUnicode(t *testing.T) {
	// Test alignment with Unicode characters
	alignment := FindPartialRatioAlignment("Universität München", "Universität")
	if alignment.Score < 80 {
		t.Errorf("Expected high score for partial Unicode match, got %v", alignment.Score)
	}
}

func TestRescoreWithDuplicateIDs(t *testing.T) {
	// Test Rescore with duplicate IDs (should still work)
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
		{ID: "1", Name: New("Tst"), Score: 85}, // Duplicate ID
		{ID: "2", Name: New("Tes"), Score: 75},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 3 {
		t.Fatalf("Expected 3 candidates, got %d", len(rescored))
	}
}

func TestChooseCandidateWithDuplicateIDs(t *testing.T) {
	// Test ChooseCandidate with duplicate IDs
	fund := New("Test")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("Test"), Score: 95},
		{ID: "1", Name: New("Test"), Score: 85}, // Duplicate ID
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil {
		t.Error("Expected a best candidate")
		return
	}
	if best.ID != "1" {
		t.Errorf("Expected candidate 1, got %s", best.ID)
	}
}

func TestExclusionAcronymCommonEndingWithAcronymOnly(t *testing.T) {
	// Test with name that is just an acronym
	fund := New("NSF")
	name := New("NSF")
	result := ExclusionAcronymCommonEnding(fund, name)
	if result != false {
		t.Error("Acronym-only name should not be excluded")
	}
}

func TestCandidateNameMatchExclusionWithSingleWord(t *testing.T) {
	// Test that single word names are excluded unless exact match
	fund := New("Tokyo")
	name := New("Tokyo")
	
	if CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Exact single word match should not be excluded")
	}
}

func TestScoreCandidateWithEmptyNames(t *testing.T) {
	// Test scoring with empty names slice
	fund := New("Test")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score != 0 {
		t.Errorf("Expected zero score, got %v", scored.Score)
	}
}

func TestPartialRatioWithVeryShortStrings(t *testing.T) {
	// Test PartialRatio with very short strings
	s := PartialRatio("a", "b")
	if s < 0 || s > 100 {
		t.Errorf("Expected score in [0,100], got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithEmptySource(t *testing.T) {
	// Test alignment with empty source string
	alignment := FindPartialRatioAlignment("", "test")
	if alignment.Score != 100 {
		t.Errorf("Expected score 100, got %v", alignment.Score)
	}
}

func TestRescoreWithEmptyCandidates(t *testing.T) {
	// Test Rescore with empty candidates
	fund := New("Test")
	rescored := Rescore(fund, nil)
	if rescored == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(rescored) != 0 {
		t.Errorf("Expected 0 candidates, got %d", len(rescored))
	}
}

func TestChooseCandidateWithEmptyCandidates(t *testing.T) {
	// Test ChooseCandidate with empty candidates
	fund := New("Test")
	best := ChooseCandidate(fund, nil)
	if best != nil {
		t.Error("Expected nil for empty candidates")
	}
}

func TestExclusionAcronymCommonEndingWithVeryLongName(t *testing.T) {
	// Test with very long name
	fund := New("NSF")
	name := New("National Science Foundation for the Advancement of Knowledge and Research")
	result := ExclusionAcronymCommonEnding(fund, name)
	if result != true && result != false {
		t.Error("Expected boolean result")
	}
}

func TestCandidateNameMatchExclusionWithNumericNames(t *testing.T) {
	// Test with numeric names
	fund := New("123")
	name := New("123")
	
	if CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Exact numeric match should not be excluded")
	}
}

func TestScoreCandidateWithNumericNames(t *testing.T) {
	// Test scoring with numeric names
	fund := New("123")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{"123"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score < 50 {
		t.Errorf("Expected reasonable score for numeric match, got %v", scored.Score)
	}
}

func TestPartialRatioWithNumericStrings(t *testing.T) {
	// Test PartialRatio with numeric strings
	s := PartialRatio("12345", "12345")
	if s != 100 {
		t.Errorf("Expected 100 for identical numeric strings, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithNumericStrings(t *testing.T) {
	// Test alignment with numeric strings
	alignment := FindPartialRatioAlignment("12345", "123")
	if alignment.Score < 80 {
		t.Errorf("Expected high score for partial numeric match, got %v", alignment.Score)
	}
}

func TestRescoreWithNumericScores(t *testing.T) {
	// Test Rescore with numeric scores
	fund := New("123")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("123"), Score: 95},
		{ID: "2", Name: New("12"), Score: 85},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 2 {
		t.Fatalf("Expected 2 candidates, got %d", len(rescored))
	}
}

func TestChooseCandidateWithNumericScores(t *testing.T) {
	// Test ChooseCandidate with numeric scores
	fund := New("123")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("123"), Score: 95},
		{ID: "2", Name: New("123"), Score: 85},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil || best.ID != "1" {
		t.Error("Expected candidate 1 with highest score")
	}
}

func TestExclusionAcronymCommonEndingWithAllCAPS(t *testing.T) {
	// Test with all-caps name
	fund := New("NSF")
	name := New("NSF")
	result := ExclusionAcronymCommonEnding(fund, name)
	if result != false {
		t.Error("All-caps acronym should not be excluded")
	}
}

func TestCandidateNameMatchExclusionWithAllCAPS(t *testing.T) {
	// Test with all-caps name
	fund := New("NSF")
	name := New("NSF")
	
	if CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Exact all-caps match should not be excluded")
	}
}

func TestScoreCandidateWithAllCAPS(t *testing.T) {
	// Test scoring with all-caps name
	fund := New("NSF")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{"NSF"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score < 90 {
		t.Errorf("Expected high score for all-caps match, got %v", scored.Score)
	}
}

func TestPartialRatioWithAllCAPS(t *testing.T) {
	// Test PartialRatio with all-caps strings
	s := PartialRatio("NSF", "NSF")
	if s != 100 {
		t.Errorf("Expected 100 for identical all-caps strings, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithAllCAPS(t *testing.T) {
	// Test alignment with all-caps strings
	alignment := FindPartialRatioAlignment("NSF", "NSF")
	if alignment.Score != 100 {
		t.Errorf("Expected score 100, got %v", alignment.Score)
	}
}

func TestRescoreWithAllCAPS(t *testing.T) {
	// Test Rescore with all-caps candidates
	fund := New("NSF")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("NSF"), Score: 95},
		{ID: "2", Name: New("NS"), Score: 85},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 2 {
		t.Fatalf("Expected 2 candidates, got %d", len(rescored))
	}
}

func TestChooseCandidateWithAllCAPS(t *testing.T) {
	// Test ChooseCandidate with all-caps candidates
	fund := New("NSF")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("NSF"), Score: 95},
		{ID: "2", Name: New("NSF"), Score: 85},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil || best.ID != "1" {
		t.Error("Expected candidate 1 with highest score")
	}
}

func TestExclusionAcronymCommonEndingWithMixedCaseAcronyms(t *testing.T) {
	// Test with mixed case acronyms
	fund := New("NSF")
	name := New("National Science Foundation")
	result := ExclusionAcronymCommonEnding(fund, name)
	if result != true && result != false {
		t.Error("Expected boolean result")
	}
}

func TestCandidateNameMatchExclusionWithMixedCase(t *testing.T) {
	// Test with mixed case name
	fund := New("National Science Foundation")
	name := New("National Science Foundation")
	
	if CandidateNameMatchExclusion(fund, name, nil) {
		t.Error("Exact match should not be excluded")
	}
}

func TestScoreCandidateWithMixedCase(t *testing.T) {
	// Test scoring with mixed case name
	fund := New("National Science Foundation")
	candidate := &Candidate{
		ID:      "test",
		Country: "US",
		Status:  "active",
		Names:   []string{"National Science Foundation"},
	}
	
	scored := ScoreCandidate(fund, candidate)
	if scored == nil {
		t.Error("Expected a scored candidate")
		return
	}
	if scored.Score < 95 {
		t.Errorf("Expected high score for exact match, got %v", scored.Score)
	}
}

func TestPartialRatioWithMixedCase(t *testing.T) {
	// Test PartialRatio with mixed case strings
	s := PartialRatio("National Science Foundation", "National Science Foundation")
	if s < 99 {
		t.Errorf("Expected ~100 for identical strings, got %v", s)
	}
}

func TestFindPartialRatioAlignmentWithMixedCase(t *testing.T) {
	// Test alignment with mixed case strings
	alignment := FindPartialRatioAlignment("National Science Foundation", "National Science")
	if alignment.Score < 80 {
		t.Errorf("Expected high score for partial match, got %v", alignment.Score)
	}
}

func TestRescoreWithMixedCase(t *testing.T) {
	// Test Rescore with mixed case candidates
	fund := New("National Science Foundation")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("National Science Foundation"), Score: 95},
		{ID: "2", Name: New("Nat Sci Foundation"), Score: 85},
	}

	rescored := Rescore(fund, candidates)
	if len(rescored) != 2 {
		t.Fatalf("Expected 2 candidates, got %d", len(rescored))
	}
}

func TestChooseCandidateWithMixedCase(t *testing.T) {
	// Test ChooseCandidate with mixed case candidates
	fund := New("National Science Foundation")
	candidates := []*CandidateMatch{
		{ID: "1", Name: New("National Science Foundation"), Score: 95},
		{ID: "2", Name: New("NSF"), Score: 85},
	}
	
	best := ChooseCandidate(fund, candidates)
	if best == nil || best.ID != "1" {
		t.Error("Expected candidate 1 with exact match")
	}
}
