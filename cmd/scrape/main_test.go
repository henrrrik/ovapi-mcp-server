package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/henrrrik/ovapi-mcp-server/db"
)

func strPtr(s string) *string { return &s }

func TestEnrichTowns(t *testing.T) {
	stops := []db.Stop{
		// has unknown + area code → enriched
		{TPCCode: "1", Name: "A", Town: "unknown", StopAreaCode: strPtr("AREA1")},
		// already has town → untouched even if area would override
		{TPCCode: "2", Name: "B", Town: "Haarlem", StopAreaCode: strPtr("AREA1")},
		// empty town + known area → enriched
		{TPCCode: "3", Name: "C", Town: "", StopAreaCode: strPtr("AREA2")},
		// no area code → not enriched
		{TPCCode: "4", Name: "D", Town: "unknown", StopAreaCode: nil},
		// area not in map → not enriched
		{TPCCode: "5", Name: "E", Town: "unknown", StopAreaCode: strPtr("UNKNOWN_AREA")},
	}
	areas := map[string]string{"AREA1": "Amsterdam", "AREA2": "Utrecht"}

	n := enrichTowns(stops, areas)
	if n != 2 {
		t.Errorf("expected 2 enrichments, got %d", n)
	}
	if stops[0].Town != "Amsterdam" {
		t.Errorf("stop 1: town = %q", stops[0].Town)
	}
	if stops[1].Town != "Haarlem" {
		t.Errorf("stop 2 town was overwritten: %q", stops[1].Town)
	}
	if stops[2].Town != "Utrecht" {
		t.Errorf("stop 3: town = %q", stops[2].Town)
	}
	if stops[3].Town != "unknown" {
		t.Errorf("stop 4: town = %q (should be unchanged)", stops[3].Town)
	}
	if stops[4].Town != "unknown" {
		t.Errorf("stop 5: town = %q (should be unchanged)", stops[4].Town)
	}
}

type stubDoer struct {
	body   string
	status int
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

func TestFetchStopAreaTowns(t *testing.T) {
	body := `{
		"AREA1": {"TimingPointTown": "Amsterdam", "TimingPointName": "X"},
		"AREA2": {"TimingPointTown": "unknown",   "TimingPointName": "Y"},
		"AREA3": {"TimingPointTown": "",          "TimingPointName": "Z"},
		"AREA4": {"TimingPointTown": "Rotterdam", "TimingPointName": "W"}
	}`
	got, err := fetchStopAreaTowns(context.Background(), &stubDoer{body: body, status: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"AREA1": "Amsterdam", "AREA4": "Rotterdam"}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d: %v", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("area %s: got %q, want %q", k, got[k], v)
		}
	}
}

func TestFetchStopAreaTowns_HTTPError(t *testing.T) {
	_, err := fetchStopAreaTowns(context.Background(), &stubDoer{body: "nope", status: 500})
	if err == nil {
		t.Error("expected error on HTTP 500")
	}
}
