package tools

import (
	"encoding/json"
	"sort"
	"strings"
)

// rawJourneyResponse wraps the upstream /journey/{id} shape,
// keyed by journey_id.
type rawJourneyResponse map[string]rawJourneyBody

type rawJourneyBody struct {
	ServerTime string                    `json:"ServerTime"`
	Stops      map[string]rawJourneyStop `json:"Stops"`
}

type rawJourneyStop struct {
	UserStopOrderNumber  int     `json:"UserStopOrderNumber"`
	TimingPointName      string  `json:"TimingPointName"`
	TimingPointCode      string  `json:"TimingPointCode"`
	TimingPointTown      string  `json:"TimingPointTown"`
	Latitude             float64 `json:"Latitude"`
	Longitude            float64 `json:"Longitude"`
	IsTimingStop         bool    `json:"IsTimingStop"`
	JourneyStopType      string  `json:"JourneyStopType"`
	TargetArrivalTime    string  `json:"TargetArrivalTime"`
	TargetDepartureTime  string  `json:"TargetDepartureTime"`
	ExpectedArrivalTime  string  `json:"ExpectedArrivalTime"`
	ExpectedDeparture    string  `json:"ExpectedDepartureTime"`
	TripStopStatus       string  `json:"TripStopStatus"`
	WheelChairAccessible string  `json:"WheelChairAccessible"`
	SideCode             string  `json:"SideCode"`
	NumberOfCoaches      int     `json:"NumberOfCoaches"`

	LinePublicNumber  string `json:"LinePublicNumber"`
	LineName          string `json:"LineName"`
	TransportType     string `json:"TransportType"`
	DataOwnerCode     string `json:"DataOwnerCode"`
	DestinationName50 string `json:"DestinationName50"`
	LineDirection     int    `json:"LineDirection"`
}

// LeanJourney is the trimmed shape returned by journey().
type LeanJourney struct {
	JourneyID   string            `json:"journey_id"`
	Line        LeanLineSummary   `json:"line"`
	Destination string            `json:"destination,omitempty"`
	ServerTime  string            `json:"server_time,omitempty"`
	Stops       []LeanJourneyStop `json:"stops"`
}

// LeanLineSummary is a compact description of a line, embedded in journey and
// line-detail responses.
type LeanLineSummary struct {
	PublicNumber string `json:"public_number,omitempty"`
	Name         string `json:"name,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Owner        string `json:"owner,omitempty"`
	Direction    int    `json:"direction,omitempty"`
	Destination  string `json:"destination,omitempty"`
}

// LeanJourneyStop is one entry on the route; stops are sorted ascending by order.
type LeanJourneyStop struct {
	Order                int         `json:"order"`
	Name                 string      `json:"name"`
	TPCCode              string      `json:"tpc_code"`
	Town                 string      `json:"town,omitempty"`
	Coord                *[2]float64 `json:"coord"`
	IsTimingStop         bool        `json:"is_timing_stop"`
	StopType             string      `json:"stop_type,omitempty"`
	TargetArrival        string      `json:"target_arrival,omitempty"`
	TargetDeparture      string      `json:"target_departure,omitempty"`
	ExpectedArrival      string      `json:"expected_arrival,omitempty"`
	ExpectedDeparture    string      `json:"expected_departure,omitempty"`
	Status               string      `json:"status,omitempty"`
	WheelchairAccessible string      `json:"wheelchair_accessible,omitempty"`
	Platform             string      `json:"platform,omitempty"`
	NumberOfCoaches      *int        `json:"number_of_coaches,omitempty"`
}

// transformJourney parses the raw upstream body and returns a lean shape.
// It prefers the journey body keyed by the requested journey_id, but falls
// back to the first/only entry if the key is missing or there's exactly one
// entry (upstream occasionally returns a slightly different key case).
func transformJourney(body []byte, journeyID string) (LeanJourney, error) {
	var raw rawJourneyResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return LeanJourney{}, err
	}
	entry, key, ok := pickJourneyEntry(raw, journeyID)
	if !ok {
		return LeanJourney{JourneyID: journeyID, Stops: []LeanJourneyStop{}}, nil
	}

	orders := make([]string, 0, len(entry.Stops))
	for k := range entry.Stops {
		orders = append(orders, k)
	}
	sort.Slice(orders, func(i, j int) bool {
		ai, _ := parseLeadingInt(orders[i])
		aj, _ := parseLeadingInt(orders[j])
		if ai != aj {
			return ai < aj
		}
		return orders[i] < orders[j]
	})

	var lineSummary LeanLineSummary
	var destination string
	stops := make([]LeanJourneyStop, 0, len(orders))
	for _, o := range orders {
		s := entry.Stops[o]
		if lineSummary.PublicNumber == "" {
			lineSummary = LeanLineSummary{
				PublicNumber: s.LinePublicNumber,
				Name:         s.LineName,
				Mode:         strings.ToLower(s.TransportType),
				Owner:        s.DataOwnerCode,
				Direction:    s.LineDirection,
			}
			destination = s.DestinationName50
		}
		stops = append(stops, leanJourneyStopFrom(s))
	}

	return LeanJourney{
		JourneyID:   key,
		Line:        lineSummary,
		Destination: destination,
		ServerTime:  entry.ServerTime,
		Stops:       stops,
	}, nil
}

func pickJourneyEntry(raw rawJourneyResponse, journeyID string) (rawJourneyBody, string, bool) {
	if entry, ok := raw[journeyID]; ok {
		return entry, journeyID, true
	}
	if len(raw) == 1 {
		for k, v := range raw {
			return v, k, true
		}
	}
	return rawJourneyBody{}, "", false
}

func leanJourneyStopFrom(s rawJourneyStop) LeanJourneyStop {
	stop := LeanJourneyStop{
		Order:                s.UserStopOrderNumber,
		Name:                 s.TimingPointName,
		TPCCode:              s.TimingPointCode,
		Town:                 townOrEmpty(s.TimingPointTown, s.TimingPointName),
		Coord:                cleanCoord(s.Latitude, s.Longitude),
		IsTimingStop:         s.IsTimingStop,
		StopType:             s.JourneyStopType,
		TargetArrival:        s.TargetArrivalTime,
		TargetDeparture:      s.TargetDepartureTime,
		ExpectedArrival:      s.ExpectedArrivalTime,
		ExpectedDeparture:    s.ExpectedDeparture,
		Status:               s.TripStopStatus,
		WheelchairAccessible: normalizeAccessibility(s.WheelChairAccessible),
		Platform:             s.SideCode,
	}
	if s.NumberOfCoaches > 0 {
		v := s.NumberOfCoaches
		stop.NumberOfCoaches = &v
	}
	return stop
}

// normalizeAccessibility collapses upstream's "UNKNOWN" (and empty) to empty,
// so the omitempty tag drops it from output.
func normalizeAccessibility(v string) string {
	if v == "" || strings.EqualFold(v, "UNKNOWN") {
		return ""
	}
	return v
}
