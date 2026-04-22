package tools

import (
	"fmt"
	"strings"
	"time"
)

// rawTPCResponse models the subset of /tpc/{codes} we consume.
type rawTPCResponse map[string]rawStopEntry

type rawStopEntry struct {
	Stop            rawStop               `json:"Stop"`
	Passes          map[string]rawPass    `json:"Passes"`
	GeneralMessages map[string]rawMessage `json:"GeneralMessages"`
}

type rawStop struct {
	TimingPointCode string  `json:"TimingPointCode"`
	TimingPointName string  `json:"TimingPointName"`
	TimingPointTown string  `json:"TimingPointTown"`
	Latitude        float64 `json:"Latitude"`
	Longitude       float64 `json:"Longitude"`
	StopAreaCode    *string `json:"StopAreaCode"`
}

type rawPass struct {
	LinePublicNumber      string `json:"LinePublicNumber"`
	LineName              string `json:"LineName"`
	DestinationName50     string `json:"DestinationName50"`
	TransportType         string `json:"TransportType"`
	TargetDepartureTime   string `json:"TargetDepartureTime"`
	ExpectedDepartureTime string `json:"ExpectedDepartureTime"`
	TripStopStatus        string `json:"TripStopStatus"`
	LastUpdateTimeStamp   string `json:"LastUpdateTimeStamp"`
	SideCode              string `json:"SideCode"`
	WheelChairAccessible  string `json:"WheelChairAccessible"`
	NumberOfCoaches       int    `json:"NumberOfCoaches"`
}

type rawMessage struct {
	MessageContent string `json:"MessageContent"`
}

// LeanResponse is the trimmed shape returned by get_departures.
type LeanResponse struct {
	Stops []LeanStop `json:"stops"`
}

type LeanStop struct {
	TPCCode    string          `json:"tpc_code"`
	Name       string          `json:"name"`
	Town       string          `json:"town,omitempty"`
	Coord      *[2]float64     `json:"coord"`
	PairedWith []string        `json:"paired_with,omitempty"`
	Departures []LeanDeparture `json:"departures"`
	Messages   []string        `json:"messages"`
}

type LeanDeparture struct {
	Line                 string  `json:"line"`
	Mode                 string  `json:"mode"`
	Destination          string  `json:"destination"`
	Planned              string  `json:"planned"`
	Expected             string  `json:"expected"`
	DelaySeconds         int     `json:"delay_seconds"`
	Status               string  `json:"status"`
	Realtime             bool    `json:"realtime"`
	Display              string  `json:"display,omitempty"`
	Platform             *string `json:"platform"`
	WheelchairAccessible *string `json:"wheelchair_accessible"`
	NumberOfCoaches      *int    `json:"number_of_coaches"`
	JourneyID            string  `json:"journey_id"`
}

// departureFilters are the post-fetch filters applied to each stop's passes.
type departureFilters struct {
	line              string
	direction         string
	timeWindowMinutes int
	maxDepartures     int
}

// amsterdamLoc is the local TZ OVapi times are expressed in.
var amsterdamLoc = mustLoadLocation("Europe/Amsterdam")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return loc
}

// timeNow is overridable by tests.
var timeNow = time.Now

// transformTPC converts the upstream response into the lean shape, applying filters.
func transformTPC(raw rawTPCResponse, filters departureFilters) LeanResponse {
	now := timeNow().In(amsterdamLoc)
	out := LeanResponse{Stops: make([]LeanStop, 0, len(raw))}
	for _, entry := range raw {
		out.Stops = append(out.Stops, transformStop(entry, filters, now))
	}
	return out
}

func transformStop(entry rawStopEntry, filters departureFilters, now time.Time) LeanStop {
	stop := LeanStop{
		TPCCode:    entry.Stop.TimingPointCode,
		Name:       entry.Stop.TimingPointName,
		Town:       townOrEmpty(entry.Stop.TimingPointTown, entry.Stop.TimingPointName),
		Coord:      cleanCoord(entry.Stop.Latitude, entry.Stop.Longitude),
		Departures: []LeanDeparture{},
		Messages:   collectMessages(entry.GeneralMessages),
	}

	for id, pass := range entry.Passes {
		dep, ok := transformPass(id, pass, filters, now)
		if !ok {
			continue
		}
		stop.Departures = append(stop.Departures, dep)
	}

	sortDeparturesByPlanned(stop.Departures)

	if filters.maxDepartures > 0 && len(stop.Departures) > filters.maxDepartures {
		stop.Departures = stop.Departures[:filters.maxDepartures]
	}
	return stop
}

func transformPass(id string, p rawPass, filters departureFilters, now time.Time) (LeanDeparture, bool) {
	planned := parseAmsterdamTime(p.TargetDepartureTime)
	if !passesFilters(p, filters, planned, now) {
		return LeanDeparture{}, false
	}
	expected := parseAmsterdamTime(p.ExpectedDepartureTime)

	var delay int
	if !planned.IsZero() && !expected.IsZero() {
		delay = int(expected.Sub(planned).Seconds())
	}

	dep := LeanDeparture{
		Line:                 p.LinePublicNumber,
		Mode:                 strings.ToLower(p.TransportType),
		Destination:          p.DestinationName50,
		Planned:              formatWithOffset(planned),
		Expected:             formatWithOffset(expected),
		DelaySeconds:         delay,
		Status:               p.TripStopStatus,
		Realtime:             p.TripStopStatus != "" && p.TripStopStatus != "PLANNED",
		Display:              computeDisplay(planned, expected, now),
		Platform:             optionalString(p.SideCode),
		WheelchairAccessible: optionalString(normalizeAccessibility(p.WheelChairAccessible)),
		NumberOfCoaches:      optionalCoaches(p.NumberOfCoaches),
		JourneyID:            id,
	}
	return dep, true
}

func passesFilters(p rawPass, f departureFilters, planned, now time.Time) bool {
	if f.line != "" && !strings.EqualFold(p.LinePublicNumber, f.line) {
		return false
	}
	if f.direction != "" && !strings.Contains(
		strings.ToLower(p.DestinationName50),
		strings.ToLower(f.direction),
	) {
		return false
	}
	if f.timeWindowMinutes > 0 && !planned.IsZero() {
		window := time.Duration(f.timeWindowMinutes) * time.Minute
		if planned.After(now.Add(window)) {
			return false
		}
	}
	return true
}

func parseAmsterdamTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// OVapi emits local wall-clock times without a TZ suffix, e.g. "2026-04-21T23:48:29".
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, amsterdamLoc); err == nil {
		return t
	}
	// Fall back to RFC3339-ish with offset (some fields include one).
	if t, err := time.Parse("2006-01-02T15:04:05-0700", s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func formatWithOffset(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(amsterdamLoc).Format("2006-01-02T15:04:05-07:00")
}

func collectMessages(m map[string]rawMessage) []string {
	msgs := make([]string, 0, len(m))
	for _, msg := range m {
		if msg.MessageContent != "" {
			msgs = append(msgs, msg.MessageContent)
		}
	}
	return msgs
}

func sortDeparturesByPlanned(deps []LeanDeparture) {
	// Insertion sort — fine for typical N~20.
	for i := 1; i < len(deps); i++ {
		for j := i; j > 0 && deps[j-1].Planned > deps[j].Planned; j-- {
			deps[j-1], deps[j] = deps[j], deps[j-1]
		}
	}
}

// computeDisplay renders a human-friendly countdown against 'now'. Guarantees
// a non-empty string whenever at least one of planned/expected is set, so
// callers never need to worry about nil display on PASSED departures.
//
//   - just-left (within 1 min past): "Net vertrokken"
//   - further in the past:           HH:MM of the scheduled time
//   - within 1 min future:           "Nu"
//   - up to 20 min future:           "N min"
//   - beyond 20 min future:          HH:MM
func computeDisplay(planned, expected, now time.Time) string {
	ref := expected
	if ref.IsZero() {
		ref = planned
	}
	if ref.IsZero() {
		return ""
	}
	delta := ref.Sub(now)
	if delta < 0 {
		if delta > -time.Minute {
			return "Net vertrokken"
		}
		fallback := planned
		if fallback.IsZero() {
			fallback = ref
		}
		return fallback.In(amsterdamLoc).Format("15:04")
	}
	mins := int(delta.Round(time.Minute) / time.Minute)
	switch {
	case mins < 1:
		return "Nu"
	case mins <= 20:
		return fmt.Sprintf("%d min", mins)
	default:
		return ref.In(amsterdamLoc).Format("15:04")
	}
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func optionalCoaches(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}
