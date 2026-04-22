package tools

import (
	"encoding/json"
	"sort"
	"time"
)

// verboseStopEntry is a pass-through shape for /tpc/{codes} that keeps Stop
// and GeneralMessages as raw JSON, so we only re-encode Passes after
// filtering.
type verboseStopEntry struct {
	Stop            json.RawMessage            `json:"Stop"`
	Passes          map[string]json.RawMessage `json:"Passes"`
	GeneralMessages json.RawMessage            `json:"GeneralMessages"`
}

// filterVerboseTPC applies the same departure filters to a raw upstream body
// as the lean transform does, but preserves the upstream field names so
// verbose callers still see raw BISON fields like JourneyPatternCode.
func filterVerboseTPC(body []byte, filters departureFilters) ([]byte, error) {
	if !anyFilterSet(filters) {
		return body, nil
	}

	var parsed map[string]verboseStopEntry
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	now := timeNow().In(amsterdamLoc)
	for code, entry := range parsed {
		entry.Passes = filterPassMap(entry.Passes, filters, now)
		parsed[code] = entry
	}
	return json.Marshal(parsed)
}

func anyFilterSet(f departureFilters) bool {
	return f.line != "" || f.direction != "" || f.timeWindowMinutes > 0 || f.maxDepartures > 0
}

func filterPassMap(passes map[string]json.RawMessage, f departureFilters, now time.Time) map[string]json.RawMessage {
	type ordered struct {
		id      string
		planned time.Time
		body    json.RawMessage
	}
	kept := make([]ordered, 0, len(passes))
	for id, raw := range passes {
		var p rawPass
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		planned := parseAmsterdamTime(p.TargetDepartureTime)
		if !passesFilters(p, f, planned, now) {
			continue
		}
		kept = append(kept, ordered{id: id, planned: planned, body: raw})
	}

	if f.maxDepartures > 0 && len(kept) > f.maxDepartures {
		sort.SliceStable(kept, func(i, j int) bool {
			return kept[i].planned.Before(kept[j].planned)
		})
		kept = kept[:f.maxDepartures]
	}

	out := make(map[string]json.RawMessage, len(kept))
	for _, k := range kept {
		out[k.id] = k.body
	}
	return out
}
