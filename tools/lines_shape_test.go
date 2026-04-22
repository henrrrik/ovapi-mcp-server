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
	resp := parseLinesIndex(t, body, linesIndexFilters{owner: "GVB", mode: "tram"})

	if resp.Truncated {
		t.Errorf("did not expect truncation for small filtered set (%d lines)", resp.Total)
	}
	if resp.Total != len(resp.Lines) {
		t.Errorf("total=%d but lines=%d", resp.Total, len(resp.Lines))
	}
}

func TestLinesIndex_ModeFilter(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{mode: "tram"})

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
	resp := parseLinesIndex(t, body, linesIndexFilters{owner: "GVB"})

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
	resp := parseLinesIndex(t, body, linesIndexFilters{owner: "GVB", mode: "tram"})

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
	resp := parseLinesIndex(t, body, linesIndexFilters{owner: "GVB", nameContains: "17"})

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

func TestLinesIndex_EmptyFilter_ReturnsEmptyArray(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owner: "NOSUCHOWNER"})

	if len(resp.Lines) != 0 {
		t.Errorf("expected empty list, got %d", len(resp.Lines))
	}
	out, _ := json.Marshal(resp)
	if !strings.Contains(string(out), `"lines":[]`) {
		t.Errorf("expected empty array, got %s", out)
	}
}

func TestLinesIndex_SortedStable(t *testing.T) {
	body := loadTestData(t, "lines_live.json")
	resp := parseLinesIndex(t, body, linesIndexFilters{owner: "GVB", mode: "tram"})

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
