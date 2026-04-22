package tools

import (
	"sort"
	"strings"
)

// DefaultLinesIndexLimit bounds the no-arg /line response — the full upstream
// index has ~4300 entries and, even lean, the entire list weighs several
// hundred KB, which is too heavy for an MCP context window. Callers who need
// more can ask for it explicitly up to MaxLinesIndexLimit.
const (
	DefaultLinesIndexLimit = 500
	MaxLinesIndexLimit     = 5000
)

// rawLinesIndex models the /line (no-arg) upstream response: a map of
// "{owner}_{public_number}_{direction}" -> line summary.
type rawLinesIndex map[string]rawLineIndexEntry

type rawLineIndexEntry struct {
	LinePublicNumber         string `json:"LinePublicNumber"`
	LineName                 string `json:"LineName"`
	TransportType            string `json:"TransportType"`
	DataOwnerCode            string `json:"DataOwnerCode"`
	DestinationName50        string `json:"DestinationName50"`
	DestinationCode          string `json:"DestinationCode"`
	LineDirection            int    `json:"LineDirection"`
	LinePlanningNumber       string `json:"LinePlanningNumber"`
	LineWheelchairAccessible string `json:"LineWheelchairAccessible"`
}

// LeanLineIndexEntry is the trimmed shape for each entry in the no-arg lines index.
type LeanLineIndexEntry struct {
	ID           string `json:"id"`
	PublicNumber string `json:"public_number"`
	Name         string `json:"name"`
	Owner        string `json:"owner"`
	Mode         string `json:"mode"`
	Direction    int    `json:"direction"`
	Destination  string `json:"destination"`
}

// LeanLinesIndexResponse is returned by lines() with no line_id argument.
type LeanLinesIndexResponse struct {
	Lines     []LeanLineIndexEntry `json:"lines"`
	Total     int                  `json:"total"`
	Truncated bool                 `json:"truncated,omitempty"`
}

type linesIndexFilters struct {
	// modes and owners both support multi-value (caller passes comma-separated
	// strings, parsed into these slices). An empty slice means "any".
	modes        []string // normalized lowercase; "ferry" is mapped to upstream "boat"
	owners       []string // compared case-insensitively
	nameContains string   // compared case-insensitively; matches LineName or LinePublicNumber or id
	publicNumber string   // exact case-insensitive match on LinePublicNumber
	limit        int      // 0 means DefaultLinesIndexLimit
}

// modeAliases maps caller-friendly mode names to the normalized lowercase
// form of upstream's TransportType. Upstream emits "BOAT" for ferries.
// "train" has no matches in KV78 (NS is a separate feed) but is accepted so
// callers get a clean empty list rather than an error.
var modeAliases = map[string]string{
	"ferry": "boat",
}

func normalizeModeFilters(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, m := range raw {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if canonical, ok := modeAliases[m]; ok {
			m = canonical
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// splitCSV returns non-empty trimmed fragments of a comma-separated value.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// transformLinesIndex converts the raw upstream map into a compact filtered,
// sorted list. Sorting is (owner ASC, public_number ASC by numeric value when
// possible, direction ASC) for stable output. Truncation is applied after
// sorting; Total reports the pre-truncation match count.
func transformLinesIndex(raw rawLinesIndex, f linesIndexFilters) LeanLinesIndexResponse {
	all := make([]LeanLineIndexEntry, 0, len(raw))
	for id, e := range raw {
		if !matchesLineFilter(id, e, f) {
			continue
		}
		all = append(all, LeanLineIndexEntry{
			ID:           id,
			PublicNumber: e.LinePublicNumber,
			Name:         e.LineName,
			Owner:        e.DataOwnerCode,
			Mode:         strings.ToLower(e.TransportType),
			Direction:    e.LineDirection,
			Destination:  e.DestinationName50,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		if a.PublicNumber != b.PublicNumber {
			return comparePublicNumber(a.PublicNumber, b.PublicNumber)
		}
		return a.Direction < b.Direction
	})

	limit := f.limit
	if limit <= 0 {
		limit = DefaultLinesIndexLimit
	}
	if limit > MaxLinesIndexLimit {
		limit = MaxLinesIndexLimit
	}

	resp := LeanLinesIndexResponse{Total: len(all)}
	if len(all) > limit {
		resp.Lines = all[:limit]
		resp.Truncated = true
	} else {
		resp.Lines = all
	}
	if resp.Lines == nil {
		resp.Lines = []LeanLineIndexEntry{}
	}
	return resp
}

func matchesLineFilter(id string, e rawLineIndexEntry, f linesIndexFilters) bool {
	if len(f.modes) > 0 && !containsString(f.modes, strings.ToLower(e.TransportType)) {
		return false
	}
	if len(f.owners) > 0 && !containsFold(f.owners, e.DataOwnerCode) {
		return false
	}
	if f.publicNumber != "" && !strings.EqualFold(f.publicNumber, e.LinePublicNumber) {
		return false
	}
	return matchesNameContains(id, e, f.nameContains)
}

func matchesNameContains(id string, e rawLineIndexEntry, needle string) bool {
	if needle == "" {
		return true
	}
	n := strings.ToLower(needle)
	return strings.Contains(strings.ToLower(e.LineName), n) ||
		strings.Contains(strings.ToLower(e.LinePublicNumber), n) ||
		strings.Contains(strings.ToLower(id), n)
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsFold(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}

// comparePublicNumber orders numeric-looking public numbers by integer value,
// falling back to lexical compare. "1" < "2" < "17" < "N85" < "bus1".
func comparePublicNumber(a, b string) bool {
	ai, aOK := parseLeadingInt(a)
	bi, bOK := parseLeadingInt(b)
	switch {
	case aOK && bOK:
		if ai != bi {
			return ai < bi
		}
		return a < b
	case aOK && !bOK:
		return true
	case !aOK && bOK:
		return false
	default:
		return a < b
	}
}

func parseLeadingInt(s string) (int, bool) {
	n := 0
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if i == 0 {
		return 0, false
	}
	return n, true
}
