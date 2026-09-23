package nsclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeDoer answers every request with the configured status and body and
// records the requests it saw.
type fakeDoer struct {
	status int
	body   string
	// bodies overrides body per URL path when set.
	bodies map[string]string
	reqs   []*http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, req)
	body := f.body
	if b, ok := f.bodies[req.URL.Path]; ok {
		body = b
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

// stationsFixture is the trimmed live station list in testdata.
func stationsFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../testdata/ns_stations.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClient_SendsSubscriptionKeyAndBuildsURL(t *testing.T) {
	doer := &fakeDoer{body: `{}`}
	c := New("secret-key", doer)
	params := url.Values{}
	params.Set("station", "UT")
	params.Set("maxJourneys", "5")
	if _, err := c.Get(context.Background(), "/api/v2/departures", params); err != nil {
		t.Fatal(err)
	}
	if len(doer.reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(doer.reqs))
	}
	req := doer.reqs[0]
	if got := req.Header.Get("Ocp-Apim-Subscription-Key"); got != "secret-key" {
		t.Errorf("subscription key header = %q", got)
	}
	if want := DefaultBase + "/api/v2/departures?maxJourneys=5&station=UT"; req.URL.String() != want {
		t.Errorf("url = %s, want %s", req.URL.String(), want)
	}
	if req.Method != http.MethodGet {
		t.Errorf("method = %s", req.Method)
	}
}

// NS answers errors as RFC 7807 problem documents; the detail is the useful
// part ("Station not found: XXXX") and must reach the caller.
func TestClient_ProblemDetailSurfaces(t *testing.T) {
	doer := &fakeDoer{status: 404, body: `{"title":"Not Found","detail":"Station not found: XXXX","type":"/problems/not-found","status":404}`}
	c := New("k", doer)
	_, err := c.Get(context.Background(), "/api/v2/departures", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T %v", err, err)
	}
	if apiErr.Status != 404 || apiErr.Detail != "Station not found: XXXX" {
		t.Errorf("unexpected APIError %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "Station not found: XXXX") {
		t.Errorf("error text should carry the detail, got %q", err.Error())
	}
}

func TestClient_NonProblemErrorStillReportsStatus(t *testing.T) {
	doer := &fakeDoer{status: 401, body: `{"message": "Access denied due to missing subscription key."}`}
	c := New("k", doer)
	_, err := c.Get(context.Background(), "/api/v2/stations", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Fatalf("expected APIError 401, got %v", err)
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "missing subscription key") {
		t.Errorf("error should carry status and upstream message, got %q", err.Error())
	}
}

func TestClient_NoKeyIsAnError(t *testing.T) {
	c := New("", &fakeDoer{body: `{}`})
	if _, err := c.Get(context.Background(), "/api/v2/stations", nil); err == nil {
		t.Fatal("expected an error when no key is configured")
	}
}

// The station list (755 entries, ~375 KB) changes rarely and counts against
// the daily quota, so it is fetched once and reused until the TTL expires.
func TestStationCache_FetchesOnceWithinTTL(t *testing.T) {
	doer := &fakeDoer{body: stationsFixture(t)}
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	cache := NewStationCache(New("k", doer), 24*time.Hour)
	cache.now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := cache.All(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(doer.reqs) != 1 {
		t.Fatalf("expected 1 upstream fetch, got %d", len(doer.reqs))
	}
	if doer.reqs[0].URL.Path != "/reisinformatie-api/api/v2/stations" {
		t.Errorf("unexpected path %s", doer.reqs[0].URL.Path)
	}
	now = now.Add(25 * time.Hour)
	if _, err := cache.All(ctx); err != nil {
		t.Fatal(err)
	}
	if len(doer.reqs) != 2 {
		t.Fatalf("expected a refresh after the TTL, got %d fetches", len(doer.reqs))
	}
}

func TestStationCache_UpstreamErrorIsReturnedNotCached(t *testing.T) {
	doer := &fakeDoer{status: 500, body: `oops`}
	cache := NewStationCache(New("k", doer), time.Hour)
	if _, err := cache.All(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	doer.status = 200
	doer.body = stationsFixture(t)
	all, err := cache.All(context.Background())
	if err != nil || len(all) == 0 {
		t.Fatalf("expected the next call to fetch successfully, got %d stations, %v", len(all), err)
	}
}

func TestStationCache_Search(t *testing.T) {
	cache := NewStationCache(New("k", &fakeDoer{body: stationsFixture(t)}), time.Hour)
	cases := []struct {
		query string
		want  string // code of the expected first result; "" for no results
	}{
		{"UT", "UT"},
		{"ut", "UT"},
		{"asdz", "ASDZ"},
		{"Utrecht Centraal", "UT"},
		{"utrecht centraal", "UT"},
		{"Utrecht", "UT"},          // synonym, and the biggest Utrecht station
		{"Amsterdam", "ASD"},       // synonym on the MEGA_STATION beats Amstel/Zuid/Sloterdijk
		{"Amsterdam Zuid", "ASDZ"}, // every token at a word boundary
		{"Den Bosch", "HT"},        // synonym only
		{"Vaartsche Rijn", "UTVR"}, // partial name, all tokens present
		{"Utrecht Ov", "UTO"},      // prefix of a longer name
		{"Schiphol", "SHL"},        // prefix of "Schiphol Airport"
		{"Dusseldorf", "DUSSEL"},   // ASCII synonym for a name with an umlaut
		{"Zwolle", "ZL"},
		{"Nowhere Special", ""},
	}
	for _, c := range cases {
		got, err := cache.Search(context.Background(), c.query, 5)
		if err != nil {
			t.Fatal(err)
		}
		if c.want == "" {
			if len(got) != 0 {
				t.Errorf("%q: expected no results, got %v", c.query, codes(got))
			}
			continue
		}
		if len(got) == 0 || got[0].Code != c.want {
			t.Errorf("%q: want %s first, got %v", c.query, c.want, codes(got))
		}
	}
}

func TestStationCache_SearchRanksAndLimits(t *testing.T) {
	cache := NewStationCache(New("k", &fakeDoer{body: stationsFixture(t)}), time.Hour)
	got, err := cache.Search(context.Background(), "Utrecht", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected the limit to apply, got %d", len(got))
	}
	for _, s := range got {
		if !strings.HasPrefix(s.Name, "Utrecht") {
			t.Errorf("unexpected station %s in Utrecht results", s.Name)
		}
	}
}

func TestStationCache_Nearest(t *testing.T) {
	cache := NewStationCache(New("k", &fakeDoer{body: stationsFixture(t)}), time.Hour)
	got, err := cache.Nearest(context.Background(), 52.0894, 5.1100, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Code != "UT" {
		t.Fatalf("expected Utrecht Centraal nearest, got %+v", got)
	}
	if got[0].DistanceM <= 0 || got[0].DistanceM > 1000 || got[1].DistanceM < got[0].DistanceM {
		t.Errorf("unexpected distances %d, %d", got[0].DistanceM, got[1].DistanceM)
	}
}

func TestStation_FieldsFromUpstream(t *testing.T) {
	cache := NewStationCache(New("k", &fakeDoer{body: stationsFixture(t)}), time.Hour)
	got, _ := cache.Search(context.Background(), "UT", 1)
	if len(got) != 1 {
		t.Fatal("expected UT")
	}
	s := got[0]
	if s.UICCode != "8400621" || s.Name != "Utrecht Centraal" || s.ShortName != "Utrecht C." || s.Country != "NL" ||
		s.Type != "MEGA_STATION" || s.Lat == 0 || s.Lng == 0 || len(s.Synonyms) != 2 {
		t.Errorf("unexpected station fields %+v", s)
	}
}

func codes(stations []Station) []string {
	out := make([]string, len(stations))
	for i, s := range stations {
		out[i] = s.Code
	}
	return out
}
