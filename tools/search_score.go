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
// Tier ceilings leave room for a fixed hub boost and still cap at 1000. The
// gaps are wide enough that a hub boost never bumps a lower tier past a
// higher-tier base score.
const (
	scoreExactFullMatch        = 950
	scoreAllTokensWordBoundary = 800
	scoreAllTokensSubstring    = 650
	scoreSomeTokensMatch       = 350
	scoreHubBoost              = 50
	scoreMaxCap                = 1000
	scoreFloor                 = 200
	scoreMinQueryLength        = 3
	searchCandidateFanout      = 4
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
	nameTokens := tokenize(s.Name)

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

	if isHub(s, paired) {
		score += scoreHubBoost
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

func isHub(s db.Stop, paired []string) bool {
	if len(paired) >= 2 {
		return true
	}
	if s.StopAreaCode != nil && *s.StopAreaCode != "" {
		return true
	}
	return false
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
