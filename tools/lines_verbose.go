package tools

import "encoding/json"

// filterVerboseLinesIndex applies the same filters and limit to the raw
// upstream index as the lean transform does, keeping upstream field names.
// The kept set is exactly the lean result's membership, so verbose never
// returns the whole 1 MB index unless the caller asked for it.
func filterVerboseLinesIndex(body []byte, f linesIndexFilters) ([]byte, error) {
	var raw rawLinesIndex
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	lean := transformLinesIndex(raw, f)
	out := make(map[string]json.RawMessage, len(lean.Lines))
	for _, l := range lean.Lines {
		out[l.ID] = entries[l.ID]
	}
	return json.Marshal(out)
}
