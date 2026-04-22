package tools

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// rawLineDetailResponse wraps /line/{id} upstream, keyed by the requested id.
type rawLineDetailResponse map[string]rawLineDetailBody

type rawLineDetailBody struct {
	Line       rawLineInfo                  `json:"Line"`
	Actuals    map[string]rawLineActualPass `json:"Actuals"`
	ServerTime string                       `json:"ServerTime"`
	// Network is keyed by JourneyPatternCode (string); each value is a map
	// from UserStopOrderNumber (as a string) to a stop snapshot.
	Network json.RawMessage `json:"Network"`
}

type rawLineInfo struct {
	LinePublicNumber         string `json:"LinePublicNumber"`
	LineName                 string `json:"LineName"`
	TransportType            string `json:"TransportType"`
	DataOwnerCode            string `json:"DataOwnerCode"`
	DestinationName50        string `json:"DestinationName50"`
	LineDirection            int    `json:"LineDirection"`
	LineWheelchairAccessible string `json:"LineWheelchairAccessible"`
}

// rawLineActualPass is the per-journey "currently running" snapshot — each
// Actuals entry carries one stop (the vehicle's current stop) rather than the
// full route.
type rawLineActualPass struct {
	TimingPointName       string  `json:"TimingPointName"`
	TimingPointCode       string  `json:"TimingPointCode"`
	UserStopOrderNumber   int     `json:"UserStopOrderNumber"`
	TripStopStatus        string  `json:"TripStopStatus"`
	ExpectedDepartureTime string  `json:"ExpectedDepartureTime"`
	TargetDepartureTime   string  `json:"TargetDepartureTime"`
	JourneyPatternCode    int64   `json:"JourneyPatternCode"`
	WheelChairAccessible  string  `json:"WheelChairAccessible"`
	NumberOfCoaches       int     `json:"NumberOfCoaches"`
	Latitude              float64 `json:"Latitude"`
	Longitude             float64 `json:"Longitude"`
}

type rawNetworkStop struct {
	UserStopOrderNumber             int     `json:"UserStopOrderNumber"`
	TimingPointName                 string  `json:"TimingPointName"`
	TimingPointCode                 string  `json:"TimingPointCode"`
	TimingPointTown                 string  `json:"TimingPointTown"`
	Latitude                        float64 `json:"Latitude"`
	Longitude                       float64 `json:"Longitude"`
	IsTimingStop                    bool    `json:"IsTimingStop"`
	TimingPointWheelChairAccessible string  `json:"TimingPointWheelChairAccessible"`
	StopAreaCode                    *string `json:"StopAreaCode"`
}

// LeanLineDetail is the trimmed shape returned by lines(line_id=...).
type LeanLineDetail struct {
	ID             string              `json:"id"`
	Line           LeanLineSummary     `json:"line"`
	ServerTime     string              `json:"server_time,omitempty"`
	ActiveJourneys []LeanActiveJourney `json:"active_journeys"`
	Route          []LeanRouteStop     `json:"route"`
}

type LeanActiveJourney struct {
	JourneyID    string `json:"journey_id"`
	CurrentStop  string `json:"current_stop,omitempty"`
	CurrentOrder int    `json:"current_order,omitempty"`
	Status       string `json:"status,omitempty"`
	Expected     string `json:"expected,omitempty"`
}

type LeanRouteStop struct {
	Order                int         `json:"order"`
	Name                 string      `json:"name"`
	TPCCode              string      `json:"tpc_code"`
	Town                 string      `json:"town,omitempty"`
	Coord                *[2]float64 `json:"coord"`
	IsTimingStop         bool        `json:"is_timing_stop"`
	WheelchairAccessible string      `json:"wheelchair_accessible,omitempty"`
	StopAreaCode         string      `json:"stop_area_code,omitempty"`
}

func transformLineDetail(body []byte, lineID string) (LeanLineDetail, error) {
	var raw rawLineDetailResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return LeanLineDetail{}, err
	}
	entry, key, ok := pickLineDetailEntry(raw, lineID)
	out := LeanLineDetail{
		ID:             key,
		ActiveJourneys: []LeanActiveJourney{},
		Route:          []LeanRouteStop{},
	}
	if !ok {
		out.ID = lineID
		return out, nil
	}

	out.ServerTime = entry.ServerTime
	out.Line = LeanLineSummary{
		PublicNumber: entry.Line.LinePublicNumber,
		Name:         entry.Line.LineName,
		Mode:         strings.ToLower(entry.Line.TransportType),
		Owner:        entry.Line.DataOwnerCode,
		Destination:  entry.Line.DestinationName50,
		Direction:    entry.Line.LineDirection,
	}

	out.ActiveJourneys = buildActiveJourneys(entry.Actuals)
	out.Route = buildRoute(entry.Network, entry.Actuals)
	return out, nil
}

func pickLineDetailEntry(raw rawLineDetailResponse, lineID string) (rawLineDetailBody, string, bool) {
	if entry, ok := raw[lineID]; ok {
		return entry, lineID, true
	}
	if len(raw) == 1 {
		for k, v := range raw {
			return v, k, true
		}
	}
	return rawLineDetailBody{}, "", false
}

func buildActiveJourneys(actuals map[string]rawLineActualPass) []LeanActiveJourney {
	out := make([]LeanActiveJourney, 0, len(actuals))
	for id, a := range actuals {
		expected := a.ExpectedDepartureTime
		if expected == "" {
			expected = a.TargetDepartureTime
		}
		out = append(out, LeanActiveJourney{
			JourneyID:    id,
			CurrentStop:  a.TimingPointName,
			CurrentOrder: a.UserStopOrderNumber,
			Status:       a.TripStopStatus,
			Expected:     expected,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CurrentOrder != out[j].CurrentOrder {
			return out[i].CurrentOrder < out[j].CurrentOrder
		}
		return out[i].JourneyID < out[j].JourneyID
	})
	return out
}

// buildRoute picks the most-used journey pattern from Network and expands it
// into a sorted list of stops. If Network is missing (or unparseable), it
// falls back to deriving a partial route from the Actuals map — better than
// nothing, but each Actual only reports one stop per active vehicle.
func buildRoute(networkRaw json.RawMessage, actuals map[string]rawLineActualPass) []LeanRouteStop {
	var patterns map[string]map[string]rawNetworkStop
	if len(networkRaw) > 0 {
		if err := json.Unmarshal(networkRaw, &patterns); err != nil {
			patterns = nil
		}
	}

	patternID := pickDominantPattern(patterns, actuals)
	if patternID != "" {
		if pattern, ok := patterns[patternID]; ok {
			return routeFromNetworkPattern(pattern)
		}
	}
	return routeFromActuals(actuals)
}

// pickDominantPattern returns the JourneyPatternCode seen in the most active
// journeys. Ties fall back to the longest pattern in Network (most stops),
// then lexical. Returns "" when there are no actuals and no network.
func pickDominantPattern(patterns map[string]map[string]rawNetworkStop, actuals map[string]rawLineActualPass) string {
	if id := mostUsedPatternInActuals(actuals); id != "" {
		return id
	}
	return longestPattern(patterns)
}

func mostUsedPatternInActuals(actuals map[string]rawLineActualPass) string {
	counts := map[string]int{}
	for _, a := range actuals {
		if a.JourneyPatternCode == 0 {
			continue
		}
		counts[strconv.FormatInt(a.JourneyPatternCode, 10)]++
	}
	var best string
	bestCount := -1
	for k, c := range counts {
		if c > bestCount || (c == bestCount && k < best) {
			best, bestCount = k, c
		}
	}
	return best
}

func longestPattern(patterns map[string]map[string]rawNetworkStop) string {
	var best string
	bestLen := -1
	for k, p := range patterns {
		if len(p) > bestLen || (len(p) == bestLen && k < best) {
			best, bestLen = k, len(p)
		}
	}
	return best
}

func routeFromNetworkPattern(pattern map[string]rawNetworkStop) []LeanRouteStop {
	out := make([]LeanRouteStop, 0, len(pattern))
	for _, s := range pattern {
		stopAreaCode := ""
		if s.StopAreaCode != nil {
			stopAreaCode = *s.StopAreaCode
		}
		out = append(out, LeanRouteStop{
			Order:                s.UserStopOrderNumber,
			Name:                 s.TimingPointName,
			TPCCode:              s.TimingPointCode,
			Town:                 townOrEmpty(s.TimingPointTown, s.TimingPointName),
			Coord:                cleanCoord(s.Latitude, s.Longitude),
			IsTimingStop:         s.IsTimingStop,
			WheelchairAccessible: normalizeAccessibility(s.TimingPointWheelChairAccessible),
			StopAreaCode:         stopAreaCode,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func routeFromActuals(actuals map[string]rawLineActualPass) []LeanRouteStop {
	seen := make(map[int]bool)
	out := make([]LeanRouteStop, 0, len(actuals))
	for _, a := range actuals {
		if seen[a.UserStopOrderNumber] {
			continue
		}
		seen[a.UserStopOrderNumber] = true
		out = append(out, LeanRouteStop{
			Order:   a.UserStopOrderNumber,
			Name:    a.TimingPointName,
			TPCCode: a.TimingPointCode,
			Coord:   cleanCoord(a.Latitude, a.Longitude),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}
