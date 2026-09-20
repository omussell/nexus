// Package match implements ROR matching against data stored in vulcanus's
// DuckDB database.  ROR organizations live as full JSON objects in the `ror`
// table's `record` column; fields are extracted at query time via
// json_extract_string so no schema migration is needed.
//
// Matching workflow
// =================
// 1. User provides a string to match (e.g. "National Science Foundation").
// 2. We normalize the string (deaccent, lowercase, collapse whitespace).
// 3. We query the `ror` table with a LIKE search, extracting name fields
//    via json_extract_string.
// 4. For each candidate we compute a fuzzy match score (partial ratio).
// 5. We filter out generic/exclusion names.
// 6. We pick the best match (or return nil if nothing exceeds threshold).
package match

import (
	"regexp"
	"strings"

	"github.com/nexus/vulcanus/internal/match/country"
)

// CandidateMatch represents a scored match candidate.
type CandidateMatch struct {
	ID    string
	Name  *StringRep
	Score float64
	Start int
	End   int
}

// Candidate is an ROR organization from the index.
type Candidate struct {
	ID      string
	Country string
	Status  string
	Names   []string
}

// DuckDBClient wraps the DuckDB-based org index for matching queries.
type DuckDBClient struct {
	client  *Client
	countries []country.Country
}

// NewDuckDBClient creates a DuckDB-backed client from an existing DB file.
func NewDuckDBClient(dbPath string, countries []country.Country) *DuckDBClient {
	c := Reopen(dbPath)
	return &DuckDBClient{client: c, countries: countries}
}

// Query searches for organization candidates from the DuckDB index.
func (c *DuckDBClient) Query(keyword string, maxCandidates int) []Candidate {
	if c.client == nil {
		return nil
	}
	orgs := c.client.Query(keyword, maxCandidates)
	if orgs == nil {
		return nil
	}
	candidates := make([]Candidate, 0, len(orgs))
	for _, o := range orgs {
		cand := Candidate{
			ID:      o.ID,
			Country: o.Country,
			Status:  o.Status,
		}
		cand.Names = o.Names
		if len(cand.Names) == 0 {
			continue
		}
		candidates = append(candidates, cand)
	}
	return candidates
}

// Count returns the number of organizations in the index.
func (c *DuckDBClient) Count() int {
	if c.client == nil {
		return 0
	}
	return c.client.Count()
}

// Close closes the underlying database connection.
func (c *DuckDBClient) Close() error {
	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

// MatchFunder runs funder-name-to-ROR matching against the DuckDB index.
func (c *DuckDBClient) MatchFunder(inputData string, fundCountries []string) []map[string]interface{} {
	fund := New(inputData)
	candidates := c.Query(inputData, 200)
	candidates = filterEligible(candidates, fundCountries)
	if len(candidates) == 0 {
		return nil
	}

	scored := make([]*CandidateMatch, 0, len(candidates))
	for i := range candidates {
		s := ScoreCandidate(fund, &candidates[i])
		if s.Score > 0 {
			scored = append(scored, s)
		}
	}
	if len(scored) == 0 {
		return nil
	}

	best := ChooseCandidate(fund, scored)
	if best == nil || best.Score < 96.0 {
		return nil
	}

	return []map[string]interface{}{
		{
			"id":         best.ID,
			"name":       best.Name.Original(),
			"confidence": best.Score,
			"substring":  fund.Original(),
		},
	}
}

// MatchAffiliation runs affiliation matching against the DuckDB index.
func (c *DuckDBClient) MatchAffiliation(inputData string, affCountries []string) []map[string]interface{} {
	aff := New(inputData)
	candidates := c.Query(inputData, 200)
	candidates = filterEligible(candidates, affCountries)
	if len(candidates) == 0 {
		return nil
	}

	scored := make([]*CandidateMatch, 0, len(candidates))
	for i := range candidates {
		s := ScoreCandidate(aff, &candidates[i])
		if s.Score > 0 {
			scored = append(scored, s)
		}
	}
	if len(scored) == 0 {
		return nil
	}

	best := ChooseCandidate(aff, scored)
	if best == nil || best.Score < 96.0 {
		return nil
	}

	return []map[string]interface{}{
		{
			"id":         best.ID,
			"name":       best.Name.Original(),
			"confidence": best.Score,
			"substring":  aff.Original(),
		},
	}
}

// FindCountries finds country codes from a string using fuzzy matching.
func FindCountries(s string, countries []country.Country, strRep *StringRep) []string {
	codes := make(map[string]bool)

	for _, c := range countries {
		if strings.IndexAny(c.Name, " ") >= 0 {
			score := PartialRatio(c.Name, strRep.Lower())
			if score >= 90.0 {
				codes[strings.ToUpper(c.Code)] = true
			}
		} else if len(c.Name) <= 2 {
			score := Ratio(strings.ToUpper(c.Name), strRep.Alpha())
			if score >= 90.0 {
				codes[strings.ToUpper(c.Code)] = true
			}
		} else {
			score := PartialRatio(c.Name, strRep.AlphaLower())
			if score >= 80.0 {
				codes[strings.ToUpper(c.Code)] = true
			}
		}
	}

	result := make([]string, 0, len(codes))
	for code := range codes {
		result = append(result, code)
	}
	return result
}

// filterEligible removes candidates that don't pass country/status filters.
func filterEligible(candidates []Candidate, fundCountries []string) []Candidate {
	result := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if eligibleCandidateFilter(&c, fundCountries) {
			result = append(result, c)
		}
	}
	return result
}

// eligibleCandidateFilter checks if a candidate is eligible for matching.
func eligibleCandidateFilter(candidate *Candidate, fundCountries []string) bool {
	// Check status - withdrawn organizations are excluded
	if candidate.Status == "withdrawn" {
		return false
	}

	// If country restriction is provided, check if candidate's country matches
	if len(fundCountries) > 0 {
		region := toRegion(candidate.Country)
		for _, fc := range fundCountries {
			if region == fc {
				return true
			}
			// Also check direct country match
			if candidate.Country == fc {
				return true
			}
		}
		return false
	}

	return true
}

// toRegion converts a country code to its region.
func toRegion(code string) string {
	return country.ToRegion(code)
}

// CommonExclusionNames contains generic organization names that should be
// excluded unless they are an exact match or have a country filter.
var CommonExclusionNames = map[string]bool{
	"university school":                                true,
	"university hospital":                              true,
	"development fund":                                 true,
	"korea university":                                 true,
	"computing center":                                 true,
	"center for international studies":                 true,
	"central government":                               true,
	"centre for international studies":                 true,
	"department of education":                          true,
	"department of conservation":                       true,
	"marie curie":                                      true,
	"department of the environment":                    true,
	"department of veterans affairs":                   true,
	"development research center":                      true,
	"education trust":                                  true,
	"heritage fund":                                    true,
	"hospital research foundation":                     true,
	"medical research council":                         true,
	"medical research foundation":                      true,
	"ministry of agriculture, food and rural affairs":  true,
	"ministry of education":                            true,
	"ministry of science":                              true,
	"national council of science and technology":       true,
	"national institute of health research":            true,
	"national research institute":                      true,
	"national science foundation":                      true,
	"national science fund":                            true,
	"prostate cancer research":                         true,
	"research promotion foundation":                    true,
	"startup foundation":                               true,
	"the alliance":                                     true,
	"l'alliance":                                       true,
	"alliance":                                         true,
	"the arc":                                          true,
	"the data lab":                                     true,
	"the george":                                       true,
	"the research council":                             true,
	"the research network":                             true,
	"the royal":                                        true,
	"the tech":                                         true,
	"the thousand":                                     true,
	"the trust":                                        true,
	"the way":                                          true,
	"union government":                                 true,
	"waste management":                                 true,
	"r core":                                           true,
	"creative foundation":                              true,
	"fonds de la recherche scientifique":               true,
	"fund for scientific research":                     true,
	"fonds national de la recherche":                   true,
	"foundation for strategic research":                true,
}

var commonRorEndings = []string{
	"center", "foundation", "trust", "university", "hospital",
	"institute", "centre", "college", "technology", "system",
	"group", "laboratory", "school", "network", "international",
	"health", "association", "division", "fellowship",
	"corporation", "services", "management", "education",
	"clinic", "alliance", "consortium", "academy", "medicine",
	"fund", "service", "systems", "engineering", "facility",
	"studies", "medical center", "foundation trust",
	"science center", "research institute", "cancer center",
	"research centre", "research foundation", "medical college",
	"healthcare system", "community college", "academy of sciences",
	"health care system",
}

var acronymPattern = regexp.MustCompile(`\b[A-Z]{2,}\b`)

// ExclusionAcronymCommonEnding checks if a name ending in a common ROR ending
// contains an acronym that doesn't also occur in the fund name.
func ExclusionAcronymCommonEnding(fund *StringRep, name *StringRep) bool {
	acronyms := acronymPattern.FindAllString(name.Original(), -1)
	if len(acronyms) == 0 {
		return false
	}
	for _, ending := range commonRorEndings {
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(ending) + `$`)
		if pattern.MatchString(name.Lower()) {
			// Check if all acronyms from name also occur in fund
			for _, acronym := range acronyms {
				if !matchesExact(fund.Lower(), acronym) {
					return true
				}
			}
		}
	}
	return false
}

// matchesExact checks if substring appears as a whole word in name.
func matchesExact(name, substring string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(substring) + `\b`)
	return re.MatchString(name)
}

// CandidateNameMatchExclusion checks if a candidate name should be excluded.
func CandidateNameMatchExclusion(fund *StringRep, name *StringRep, fundCountries []string) bool {
	lowerName := name.Lower()

	// Never exclude an exact match — this is a real org, not a generic name.
	if strings.EqualFold(fund.Original(), name.Original()) {
		return false
	}

	// Check excluded general names
	if CommonExclusionNames[lowerName] {
		return true
	}

	// Check names too general without country
	if len(fundCountries) == 0 {
		if CommonExclusionNames[lowerName] {
			return true
		}
	}

	// Names too long
	if name.Len() > fund.Len()+4 {
		return true
	}

	// Names too short
	if name.Len() < 5 {
		return true
	}

	// Single word names
	if !strings.Contains(name.Original(), " ") && fund.Lower() != name.Lower() {
		return true
	}

	// Fund is single word
	if !strings.Contains(fund.Original(), " ") && fund.Lower() != name.Lower() {
		return true
	}

	// Check acronym + common ending
	if ExclusionAcronymCommonEnding(fund, name) {
		return true
	}

	return false
}

// TryAlteredName tries removing a parenthetical phrase at the end of the name.
func TryAlteredName(name *StringRep) *StringRep {
	re := regexp.MustCompile(`\s*\(.*?\)\s*$`)
	newName := re.ReplaceAllString(name.Original(), "")
	if newName != "" && newName != name.Original() {
		return New(newName)
	}
	return nil
}

// ScoreCandidate scores a candidate against the input string.
func ScoreCandidate(fund *StringRep, candidate *Candidate) *CandidateMatch {
	best := &CandidateMatch{
		Name:  New(""),
		Score: 0,
		Start: -1,
		End:   -1,
	}

	for _, nameStr := range candidate.Names {
		name := New(nameStr)
		if CandidateNameMatchExclusion(fund, name, nil) {
			altName := TryAlteredName(name)
			if altName == nil || CandidateNameMatchExclusion(fund, altName, nil) {
				continue
			}
			name = altName
		}

		alignment := FindPartialRatioAlignment(
			fund.Normalized(), name.Normalized(),
		)

		if alignment.Score > best.Score {
			best = &CandidateMatch{
				ID:    candidate.ID,
				Name:  name,
				Score: alignment.Score,
				Start: alignment.SrcStart,
				End:   alignment.SrcEnd,
			}
		}
	}

	return best
}

// IsBetter determines if candidate1 is a better match than candidate2.
func IsBetter(fund *StringRep, candidate1, candidate2 *CandidateMatch) bool {
	score := 0

	if strings.Contains(candidate1.Name.Lower(), "univ") && !strings.Contains(candidate2.Name.Lower(), "univ") {
		score++
	}
	if !strings.Contains(candidate1.Name.Lower(), "univ") && strings.Contains(candidate2.Name.Lower(), "univ") {
		score--
	}

	cDiff := abs(candidate1.Name.Len() - fund.Len())
	oDiff := abs(candidate2.Name.Len() - fund.Len())

	if oDiff-cDiff > 4 {
		score++
	}
	if cDiff-oDiff > 4 {
		score--
	}

	if candidate1.Start > candidate2.End {
		score++
	}
	if candidate2.Start > candidate1.End {
		score--
	}

	if candidate1.Score > 99 && candidate2.Score < 99 {
		score++
	}
	if candidate1.Score < 99 && candidate2.Score > 99 {
		score--
	}

	return score > 0
}

// Rescore rescores candidates using pairwise comparison.
func Rescore(fund *StringRep, candidates []*CandidateMatch) []*CandidateMatch {
	newScores := make([]float64, len(candidates))
	for i, candidate := range candidates {
		for _, other := range candidates {
			if candidate == other {
				continue
			}
			if IsBetter(fund, candidate, other) {
				newScores[i]++
			}
		}
	}

	result := make([]*CandidateMatch, len(candidates))
	for i, c := range candidates {
		c.Score = newScores[i]
		result[i] = c
	}
	return result
}

// ChooseCandidate picks the best match from scored candidates.
func ChooseCandidate(fund *StringRep, candidates []*CandidateMatch) *CandidateMatch {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	rescored := Rescore(fund, candidates)

	best := rescored[0]
	bestIdx := 0
	for i, c := range rescored {
		if c.Score > best.Score {
			best = c
			bestIdx = i
		} else if c.Score == best.Score && c.Score > 0 {
			// Tie-break: prefer later position (less context overlap)
			if c.Start > best.Start {
				best = c
				bestIdx = i
			}
		}
	}
	return rescored[bestIdx]
}

// abs returns the absolute value of x.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
