package nsclient

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Station is one entry of the NS station list.
type Station struct {
	Code      string   // short station code, e.g. "UT"
	UICCode   string   // e.g. "8400621"
	Name      string   // long name, e.g. "Utrecht Centraal"
	ShortName string   // e.g. "Utrecht C."
	Synonyms  []string // e.g. "Utrecht CS", "Utrecht"
	Country   string   // "NL", "D", "B", ...
	Lat, Lng  float64
	Type      string // MEGA_STATION, KNOOPPUNT_INTERCITY_STATION, ...
}

// StationDistance is a Station with its distance from a query point.
type StationDistance struct {
	Station
	DistanceM int
}

// StationCache holds the full station list in memory. It changes rarely
// and the daily quota is small, so it is fetched once per TTL.
type StationCache struct {
	client *Client
	ttl    time.Duration
	now    func() time.Time

	mu       sync.Mutex
	stations []Station
	fetched  time.Time
}

// NewStationCache returns a cache that refreshes after ttl.
func NewStationCache(client *Client, ttl time.Duration) *StationCache {
	return &StationCache{client: client, ttl: ttl, now: time.Now}
}

type rawStation struct {
	UICCode     string   `json:"UICCode"`
	Code        string   `json:"code"`
	Land        string   `json:"land"`
	Lat         float64  `json:"lat"`
	Lng         float64  `json:"lng"`
	StationType string   `json:"stationType"`
	Synoniemen  []string `json:"synoniemen"`
	Namen       struct {
		Lang   string `json:"lang"`
		Middel string `json:"middel"`
		Kort   string `json:"kort"`
	} `json:"namen"`
}

// All returns every station, fetching the list when it is missing or stale.
func (c *StationCache) All(ctx context.Context) ([]Station, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stations != nil && c.now().Sub(c.fetched) < c.ttl {
		return c.stations, nil
	}
	body, err := c.client.Get(ctx, "/api/v2/stations", nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Payload []rawStation `json:"payload"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	stations := make([]Station, 0, len(raw.Payload))
	for _, r := range raw.Payload {
		stations = append(stations, Station{
			Code: r.Code, UICCode: r.UICCode, Name: r.Namen.Lang, ShortName: r.Namen.Middel,
			Synonyms: r.Synoniemen, Country: r.Land, Lat: r.Lat, Lng: r.Lng, Type: r.StationType,
		})
	}
	c.stations, c.fetched = stations, c.now()
	return stations, nil
}

// Match tiers for Search. Ties are broken by station importance, then
// Dutch stations first, then name.
const (
	matchCode     = 1000
	matchName     = 900
	matchAllWords = 700
	matchPrefix   = 600
	matchContains = 400
)

// Search ranks stations against a name or code and returns the best limit
// matches: exact code, then an exact name or synonym, then every query
// word present in a name, then a name prefix, then a substring.
func (c *StationCache) Search(ctx context.Context, query string, limit int) ([]Station, error) {
	all, err := c.All(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	type scored struct {
		Station
		score int
	}
	var hits []scored
	for _, s := range all {
		if sc := stationScore(q, s); sc > 0 {
			hits = append(hits, scored{s, sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if ra, rb := stationTypeRank(a.Type), stationTypeRank(b.Type); ra != rb {
			return ra < rb
		}
		if (a.Country == "NL") != (b.Country == "NL") {
			return a.Country == "NL"
		}
		return a.Name < b.Name
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Station, len(hits))
	for i, h := range hits {
		out[i] = h.Station
	}
	return out, nil
}

func stationScore(q string, s Station) int {
	if strings.EqualFold(q, s.Code) {
		return matchCode
	}
	names := append([]string{s.Name, s.ShortName}, s.Synonyms...)
	for _, n := range names {
		if strings.EqualFold(q, n) {
			return matchName
		}
	}
	qTokens := stationTokens(q)
	for _, n := range append([]string{s.Name}, s.Synonyms...) {
		if allTokensIn(qTokens, stationTokens(n)) {
			return matchAllWords
		}
	}
	for _, n := range names {
		if strings.HasPrefix(strings.ToLower(n), q) {
			return matchPrefix
		}
	}
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), q) {
			return matchContains
		}
	}
	return 0
}

func stationTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		switch r {
		case ' ', '-', '/', ',', '.', '(', ')', '\'':
			return true
		}
		return false
	})
}

func allTokensIn(query, name []string) bool {
	if len(query) == 0 {
		return false
	}
	set := make(map[string]bool, len(name))
	for _, n := range name {
		set[n] = true
	}
	for _, q := range query {
		if !set[q] {
			return false
		}
	}
	return true
}

// stationTypeRank orders NS station types from largest interchange to
// smallest halt, so a bare city name resolves to its main station.
func stationTypeRank(t string) int {
	switch t {
	case "MEGA_STATION":
		return 0
	case "KNOOPPUNT_INTERCITY_STATION":
		return 1
	case "INTERCITY_STATION":
		return 2
	case "KNOOPPUNT_SNELTREIN_STATION":
		return 3
	case "SNELTREIN_STATION":
		return 4
	case "KNOOPPUNT_STOPTREIN_STATION":
		return 5
	case "STOPTREIN_STATION":
		return 6
	}
	return 7
}

// Nearest returns the limit stations closest to a point, nearest first.
func (c *StationCache) Nearest(ctx context.Context, lat, lng float64, limit int) ([]StationDistance, error) {
	all, err := c.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StationDistance, 0, len(all))
	for _, s := range all {
		if s.Lat == 0 && s.Lng == 0 {
			continue
		}
		out = append(out, StationDistance{Station: s, DistanceM: int(math.Round(haversineMeters(lat, lng, s.Lat, s.Lng)))})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DistanceM != out[j].DistanceM {
			return out[i].DistanceM < out[j].DistanceM
		}
		return out[i].Code < out[j].Code
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6_371_000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
