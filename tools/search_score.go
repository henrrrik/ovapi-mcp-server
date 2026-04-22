package tools

import (
	"sort"
	"strings"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

// Score tiers (0..1000) for search_stops results. Higher is better.
// The thresholds are close enough that a small hub boost can nudge a
// borderline result across tiers without overwhelming the intent of the
// query.
// Tier ceilings leave room for a scaled hub boost (up to +100) while keeping
// everything inside the 0-1000 band. Exact matches hit the cap outright.
const (
	scoreExactFullMatch        = 1000
	scoreAllTokensWordBoundary = 800
	scoreAllTokensSubstring    = 650
	scoreSomeTokensMatch       = 350
	scoreMaxCap                = 1000
	scoreFloor                 = 200
	scoreMinQueryLength        = 3
	searchCandidateFanout      = 4

	// Hub boost components. Paired_with is scaled (+10/entry up to +50),
	// stop_area_code is a flat +25, and a canonical hub name (Airport,
	// Centraal, or *Station) adds another +25. An exact-match result
	// (already at the cap) is unaffected; boost can lift a word-boundary
	// match to 900, still below an exact match.
	hubBoostPerPair       = 10
	hubBoostPairedCap     = 50
	hubBoostStopAreaCode  = 25
	hubBoostCanonicalName = 25
)

// SearchResultStop is the shape returned by search_stops. It re-exposes the
// stored stop fields plus a ranking score.
type SearchResultStop struct {
	TPCCode      string      `json:"tpc_code"`
	Name         string      `json:"name"`
	Town         string      `json:"town,omitempty"`
	Coord        *[2]float64 `json:"coord"`
	StopAreaCode *string     `json:"stop_area_code,omitempty"`
	PairedWith   []string    `json:"paired_with,omitempty"`
	Score        int         `json:"score"`
}

// scoreAndRank takes raw DB candidates and returns a filtered, scored,
// sorted result set. Stops below scoreFloor are dropped.
func scoreAndRank(query string, candidates []db.Stop, pairs map[string][]string, limit int) []SearchResultStop {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	out := make([]SearchResultStop, 0, len(candidates))
	for _, c := range candidates {
		s := scoreStop(query, tokens, c, pairs[c.TPCCode])
		if s.Score < scoreFloor {
			continue
		}
		out = append(out, s)
	}

	// Stable sort by score desc, then name asc for determinism.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func scoreStop(query string, queryTokens []string, s db.Stop, paired []string) SearchResultStop {
	score := 0
	lowerName := strings.ToLower(strings.TrimSpace(s.Name))
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	nameTokens := expandHubAliases(tokenize(s.Name))

	switch {
	case lowerName == lowerQuery:
		score = scoreExactFullMatch
	case allTokensAtWordBoundary(queryTokens, nameTokens):
		score = scoreAllTokensWordBoundary
	case allTokensAsSubstrings(queryTokens, lowerName):
		score = scoreAllTokensSubstring
	case anyTokenAsSubstring(queryTokens, lowerName):
		score = scoreSomeTokensMatch
	}

	// Hub boost only lifts non-exact matches; exact matches are already at
	// the cap so further points would be clipped anyway.
	if score > 0 && score < scoreExactFullMatch {
		score += hubBoost(s, paired)
	}
	if score > scoreMaxCap {
		score = scoreMaxCap
	}

	coord := cleanCoord(s.Latitude, s.Longitude)
	town := townOrEmpty(s.Town, s.Name)

	var stopAreaCode *string
	if s.StopAreaCode != nil && *s.StopAreaCode != "" {
		v := *s.StopAreaCode
		stopAreaCode = &v
	}

	return SearchResultStop{
		TPCCode:      s.TPCCode,
		Name:         s.Name,
		Town:         town,
		Coord:        coord,
		StopAreaCode: stopAreaCode,
		PairedWith:   paired,
		Score:        score,
	}
}

// hubBoost returns a scaled bonus rewarding true interchanges. Paired stops,
// a stop_area_code, and a canonical hub name each contribute independently.
func hubBoost(s db.Stop, paired []string) int {
	boost := 0
	if n := len(paired); n > 0 {
		p := n * hubBoostPerPair
		if p > hubBoostPairedCap {
			p = hubBoostPairedCap
		}
		boost += p
	}
	if s.StopAreaCode != nil && *s.StopAreaCode != "" {
		boost += hubBoostStopAreaCode
	}
	if isCanonicalHubName(s.Name) {
		boost += hubBoostCanonicalName
	}
	return boost
}

func isCanonicalHubName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "airport") {
		return true
	}
	if strings.Contains(lower, "centraal") {
		return true
	}
	return strings.HasSuffix(lower, "station")
}

// tokenize splits on whitespace and common separators, lowercases, and drops
// empty fragments. ("Amsterdam Centraal" -> ["amsterdam","centraal"].)
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ',', '/', '-', '.':
			return true
		}
		return false
	})
}

// hubAliases is applied to stop-name tokens only. Dutch station names use
// "CS" and "Centraal Station" interchangeably (plus sometimes just
// "Centraal"). Expanding the indexed name makes queries like "Utrecht
// Centraal" match "Utrecht, CS Centrumzijde" without rewriting the query
// side, keeping exact-match semantics intact.
var hubAliases = map[string][]string{
	"cs":       {"centraal", "station"},
	"centraal": {"cs"},
	"station":  {"cs"},
}

func expandHubAliases(tokens []string) []string {
	seen := make(map[string]bool, len(tokens)*2)
	for _, t := range tokens {
		seen[t] = true
	}
	for _, t := range tokens {
		for _, alias := range hubAliases[t] {
			seen[alias] = true
		}
	}
	out := make([]string, 0, len(seen))
	for _, t := range tokens {
		out = append(out, t)
	}
	for alias := range seen {
		if !containsString(tokens, alias) {
			out = append(out, alias)
		}
	}
	return out
}

// allTokensAtWordBoundary returns true when every query token equals some
// token in the name (case-insensitive). Handles "Amsterdam Centraal" ↔
// "Amsterdam, Centraal Station" but rejects "Centraal" ↔ "Rotterdam Centraal"
// when the query is the multi-token "Amsterdam Centraal".
func allTokensAtWordBoundary(queryTokens, nameTokens []string) bool {
	if len(queryTokens) == 0 {
		return false
	}
	nameSet := make(map[string]bool, len(nameTokens))
	for _, n := range nameTokens {
		nameSet[n] = true
	}
	for _, q := range queryTokens {
		if !nameSet[q] {
			return false
		}
	}
	return true
}

func allTokensAsSubstrings(queryTokens []string, lowerName string) bool {
	for _, q := range queryTokens {
		if !strings.Contains(lowerName, q) {
			return false
		}
	}
	return true
}

func anyTokenAsSubstring(queryTokens []string, lowerName string) bool {
	for _, q := range queryTokens {
		if strings.Contains(lowerName, q) {
			return true
		}
	}
	return false
}
