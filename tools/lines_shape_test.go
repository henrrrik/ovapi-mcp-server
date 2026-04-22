package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseLinesIndex(t *testing.T, body string, f linesIndexFilters) LeanLinesIndexResponse {
	t.Helper()
	var raw rawLinesIndex
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("parse raw: %v", err)
	}
	return transformLinesIndex(raw, f)
}

func TestLinesIndex_NoFilters_ShapeAndSize(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{})

	if len(resp.Lines) != DefaultLinesIndexLimit {
		t.Fatalf("expected %d lines under default cap, got %d",
			DefaultLinesIndexLimit, len(resp.Lines))
	}
	if !resp.Truncated {
		t.Error("expected truncated=true on full production data")
	}
	if resp.Total < 1000 {
		t.Errorf("expected total match count in the thousands, got %d", resp.Total)
	}
	assertLinesIndexSize(t, resp, len(body))
	assertLinesIndexSparseness(t, resp.Lines[:5])
}

func assertLinesIndexSize(t *testing.T, resp LeanLinesIndexResponse, rawLen int) {
	t.Helper()
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(out) > 150_000 {
		t.Errorf("lean lines index too large: %d bytes (want <150KB)", len(out))
	}
	if len(out) >= rawLen {
		t.Errorf("lean index not smaller than raw: lean=%d raw=%d", len(out), rawLen)
	}
}

func assertLinesIndexSparseness(t *testing.T, lines []LeanLineIndexEntry) {
	t.Helper()
	for _, l := range lines {
		if l.ID == "" || l.PublicNumber == "" || l.Owner == "" {
			t.Errorf("sparse entry: %+v", l)
		}
		if strings.ToLower(l.Mode) != l.Mode {
			t.Errorf("mode not lowercase: %q", l.Mode)
		}
	}
}

func TestLinesIndex_LimitRespected(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{limit: 10})

	if len(resp.Lines) != 10 {
		t.Fatalf("expected 10 lines with limit=10, got %d", len(resp.Lines))
	}
	if !resp.Truncated {
		t.Error("expected truncated=true when total > limit")
	}
}

func TestLinesIndex_NoTruncationOnSmallFilteredSet(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"GVB"}, modes: []string{"tram"}})

	if resp.Truncated {
		t.Errorf("did not expect truncation for small filtered set (%d lines)", resp.Total)
	}
	if resp.Total != len(resp.Lines) {
		t.Errorf("total=%d but lines=%d", resp.Total, len(resp.Lines))
	}
}

func TestLinesIndex_MultiModeFilter(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{modes: []string{"tram", "metro"}})

	if len(resp.Lines) == 0 {
		t.Fatal("expected some tram+metro lines")
	}
	sawTram, sawMetro := false, false
	for _, l := range resp.Lines {
		switch l.Mode {
		case "tram":
			sawTram = true
		case "metro":
			sawMetro = true
		default:
			t.Errorf("unexpected mode %q in tram+metro filter", l.Mode)
		}
	}
	if !sawTram || !sawMetro {
		t.Errorf("expected both tram and metro in results, tram=%v metro=%v", sawTram, sawMetro)
	}
}

func TestLinesIndex_MultiOwnerFilter(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"GVB", "HTM"}})

	if len(resp.Lines) == 0 {
		t.Fatal("expected some GVB+HTM lines")
	}
	for _, l := range resp.Lines {
		if !strings.EqualFold(l.Owner, "GVB") && !strings.EqualFold(l.Owner, "HTM") {
			t.Errorf("unexpected owner %q", l.Owner)
		}
	}
}

func TestLinesIndex_FerryAliasMapsToBoat(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	got := parseLinesIndex(t, body, linesIndexFilters{
		modes: normalizeModeFilters([]string{"ferry"}),
	})
	want := parseLinesIndex(t, body, linesIndexFilters{modes: []string{"boat"}})
	if got.Total != want.Total {
		t.Errorf("ferry alias: total=%d, boat total=%d", got.Total, want.Total)
	}
	if got.Total == 0 {
		t.Error("expected some BOAT lines in fixture")
	}
}

func TestLinesIndex_TrainAcceptedButEmpty(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{
		modes: normalizeModeFilters([]string{"train"}),
	})
	if resp.Total != 0 {
		t.Errorf("expected 0 train lines in KV78 feed, got %d", resp.Total)
	}
	if resp.Truncated {
		t.Error("empty result should not be marked truncated")
	}
}

func TestLinesIndex_FilterBeforeTruncation_FindsMetros(t *testing.T) {
	// Metros are far down the alphabetical sort when unfiltered; without
	// filter-before-truncate, mode=metro would return an empty list from
	// the top-500 slice.
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{modes: []string{"metro"}})

	if resp.Total == 0 {
		t.Fatal("expected metros in production fixture")
	}
	for _, l := range resp.Lines {
		if l.Mode != "metro" {
			t.Errorf("expected metro only, got %q", l.Mode)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,,c,", []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		got := splitCSV(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i, v := range got {
			if v != tc.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", tc.in, i, v, tc.want[i])
			}
		}
	}
}

func TestLinesIndex_ModeFilter(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{modes: []string{"tram"}})

	if len(resp.Lines) == 0 {
		t.Fatal("expected some tram lines")
	}
	for _, l := range resp.Lines {
		if l.Mode != "tram" {
			t.Errorf("expected all trams, got %q", l.Mode)
		}
	}
}

func TestLinesIndex_OwnerFilter(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"GVB"}})

	if len(resp.Lines) == 0 {
		t.Fatal("expected some GVB lines")
	}
	for _, l := range resp.Lines {
		if !strings.EqualFold(l.Owner, "GVB") {
			t.Errorf("expected owner GVB, got %q", l.Owner)
		}
	}
}

func TestLinesIndex_OwnerAndModeCombined(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"GVB"}, modes: []string{"tram"}})

	if len(resp.Lines) == 0 {
		t.Fatal("expected some GVB tram lines")
	}
	if len(resp.Lines) > 50 {
		t.Errorf("GVB trams should be a small set, got %d", len(resp.Lines))
	}
	for _, l := range resp.Lines {
		if !strings.EqualFold(l.Owner, "GVB") || l.Mode != "tram" {
			t.Errorf("failed filter: %+v", l)
		}
	}
}

func TestLinesIndex_NameContainsFilter(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{nameContains: "Centraal"})

	if len(resp.Lines) == 0 {
		t.Fatal("expected some matches for 'Centraal'")
	}
}

func TestLinesIndex_NameContains_PublicNumberMatch(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"GVB"}, nameContains: "17"})

	if len(resp.Lines) == 0 {
		t.Fatal("expected GVB line 17 to match")
	}
	// Ensure at least one entry has public number 17.
	found := false
	for _, l := range resp.Lines {
		if l.PublicNumber == "17" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected public_number=17 among results")
	}
}

func TestLinesIndex_PublicNumberExact(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	// name_contains='1' would match "1", "10", "11", "17", "N1", etc.
	// public_number='1' must match only the exact string "1".
	resp := parseLinesIndex(t, body, linesIndexFilters{publicNumber: "1"})

	if len(resp.Lines) == 0 {
		t.Fatal("expected at least one line with public_number=1")
	}
	for _, l := range resp.Lines {
		if l.PublicNumber != "1" {
			t.Errorf("public_number filter leaked %q", l.PublicNumber)
		}
	}
}

func TestLinesIndex_PublicNumberCaseInsensitive(t *testing.T) {
	body := `{"GVB_N1_1":{"LinePublicNumber":"N1","DataOwnerCode":"GVB","LineName":"Night","TransportType":"BUS","LineDirection":1}}`
	resp := parseLinesIndex(t, body, linesIndexFilters{publicNumber: "n1"})

	if resp.Total != 1 {
		t.Errorf("expected 1 match for case-insensitive public_number, got %d", resp.Total)
	}
}

func TestLinesIndex_PublicNumberAndOwnerCompose(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{
		owners:       []string{"GVB"},
		publicNumber: "17",
	})
	if len(resp.Lines) == 0 {
		t.Fatal("expected GVB line 17 entries")
	}
	for _, l := range resp.Lines {
		if !strings.EqualFold(l.Owner, "GVB") || l.PublicNumber != "17" {
			t.Errorf("filter leak: %+v", l)
		}
	}
}

func TestLinesIndex_EmptyFilter_ReturnsEmptyArray(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"NOSUCHOWNER"}})

	if len(resp.Lines) != 0 {
		t.Errorf("expected empty list, got %d", len(resp.Lines))
	}
	if resp.Total != 0 {
		t.Errorf("expected total=0, got %d", resp.Total)
	}
	if resp.Truncated {
		t.Error("expected truncated=false for empty result")
	}
	out, _ := json.Marshal(resp)
	if !strings.Contains(string(out), `"lines":[]`) {
		t.Errorf("expected empty array, got %s", out)
	}
	// Acceptance: empty match returns {"lines": [], "total": 0, "truncated": false}
	// — serialization must show total:0 and must NOT show truncated:true. With
	// omitempty, truncated:false is elided entirely.
	if !strings.Contains(string(out), `"total":0`) {
		t.Errorf("expected total:0 in JSON, got %s", out)
	}
	if strings.Contains(string(out), `"truncated":true`) {
		t.Errorf("expected truncated absent or false, got %s", out)
	}
}

func TestLinesIndex_SortedStable(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owners: []string{"GVB"}, modes: []string{"tram"}})

	prevOwner, prevNum, prevDir := "", -1, -1
	for _, l := range resp.Lines {
		if l.Owner < prevOwner {
			t.Fatalf("owner out of order: %+v", l)
		}
		num, ok := parseLeadingInt(l.PublicNumber)
		if !ok {
			continue
		}
		if l.Owner == prevOwner {
			if num < prevNum {
				t.Fatalf("public_number out of order: prev=%d got=%d", prevNum, num)
			}
			if num == prevNum && l.Direction < prevDir {
				t.Fatalf("direction out of order for line %s", l.PublicNumber)
			}
		}
		prevOwner = l.Owner
		prevNum = num
		prevDir = l.Direction
	}
}
