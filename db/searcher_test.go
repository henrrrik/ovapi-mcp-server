package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
)

// These tests run against a real Postgres with pg_trgm. They are skipped
// unless OVAPI_TEST_DATABASE_URL points at a throwaway database: the stops
// table is truncated and re-seeded on every run.
//
//	docker run -d --name ovapi-pg -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:16
//	OVAPI_TEST_DATABASE_URL='postgres://postgres:pg@localhost:55432/postgres?sslmode=disable' go test ./db/

func ptr(s string) *string { return &s }

func seededSearcher(t *testing.T, stops []Stop) *PgStopSearcher {
	t.Helper()
	url := os.Getenv("OVAPI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("OVAPI_TEST_DATABASE_URL not set")
	}
	conn, err := Open(url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "TRUNCATE stops"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := UpsertStops(ctx, conn, stops); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return &PgStopSearcher{DB: conn}
}

// sameSpot returns n stops sharing a name and location, with codes
// prefix+"01".. so pairing tests can count them.
func sameSpot(prefix, name string, n int, lat, lng float64, area *string) []Stop {
	out := make([]Stop, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, Stop{
			TPCCode: fmt.Sprintf("%s%02d", prefix, i), Name: name, Town: "unknown",
			Latitude: lat, Longitude: lng, StopAreaCode: area,
		})
	}
	return out
}

func searchFixture() []Stop {
	var s []Stop
	s = append(s, sameSpot("air", "Schiphol, Airport", 3, 52.3086, 4.7639, ptr("schns"))...)
	s = append(s, sameSpot("weg", "Schipholweg", 2, 52.1600, 4.4900, nil)...)
	s = append(s, Stop{TPCCode: "plaza1", Name: "Schiphol, Plaza", Town: "unknown", Latitude: 52.309, Longitude: 4.762})
	s = append(s, Stop{TPCCode: "knoop1", Name: "Knooppunt Schiphol Nrd", Town: "unknown", Latitude: 52.32, Longitude: 4.75})
	s = append(s, Stop{TPCCode: "school1", Name: "School", Town: "unknown", Latitude: 51.5, Longitude: 5.5})
	// Real Utrecht data: the hub name is longer than dozens of minor stops
	// that trigram similarity prefers. Seed more short names than the pool
	// holds so the hub only gets in if the query ranks it deliberately.
	s = append(s, sameSpot("ucs", "Utrecht, CS Centrumzijde", 2, 52.0894, 5.1100, ptr("utrbus"))...)
	for i, name := range []string{"Neude", "Texel", "Ondiep", "Viaduct", "Kernweg", "Draaiweg", "De Gaard", "Lanslaan", "De Woerd", "Balijebrug"} {
		s = append(s, Stop{TPCCode: fmt.Sprintf("umin%02d", i), Name: "Utrecht, " + name, Town: "unknown",
			Latitude: 52.09 + float64(i)/100, Longitude: 5.11, StopAreaCode: ptr(fmt.Sprintf("utr%02d", i))})
	}
	return s
}

func names(stops []Stop) []string {
	out := make([]string, len(stops))
	for i, s := range stops {
		out[i] = s.Name
	}
	return out
}

// pg_trgm similarity favours names close to the query's length, so a bare
// "Schiphol" used to fill a small pool with "Schipholweg" while the airport
// never made it in. A query that appears as a whole word must rank first.
func TestSearchStops_WordMatchBeatsShortSimilarName(t *testing.T) {
	s := seededSearcher(t, searchFixture())
	got, err := s.SearchStops(context.Background(), "Schiphol", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Name != "Schiphol, Airport" {
		t.Fatalf("expected 'Schiphol, Airport' first, got %v", names(got))
	}
	for _, st := range got {
		if st.Name == "Schipholweg" {
			t.Errorf("'Schipholweg' should not enter a pool of 4 ahead of whole-word matches: %v", names(got))
		}
	}
}

// Among stops that all contain the query as a word, canonical hub names
// (Centraal, CS, Station, Airport) must enter the pool before minor stops,
// otherwise the Go re-ranker never sees the hub for small limits.
func TestSearchStops_CanonicalHubEntersSmallPool(t *testing.T) {
	s := seededSearcher(t, searchFixture())
	got, err := s.SearchStops(context.Background(), "Utrecht", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected a pool of 4, got %d", len(got))
	}
	if got[0].Name != "Utrecht, CS Centrumzijde" {
		t.Errorf("expected the CS stop to lead the pool, got %v", names(got))
	}
}

func TestSearchStops_TypoStillFindsStop(t *testing.T) {
	s := seededSearcher(t, searchFixture())
	got, err := s.SearchStops(context.Background(), "Schipol", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || !strings.HasPrefix(got[0].Name, "Schiphol") {
		t.Errorf("expected a Schiphol stop for the misspelling, got %v", names(got))
	}
	for _, st := range got {
		if st.Name == "School" {
			t.Errorf("'School' outranked Schiphol for 'Schipol': %v", names(got))
		}
	}
}

func TestSearchStops_NonsenseReturnsNothing(t *testing.T) {
	s := seededSearcher(t, searchFixture())
	got, err := s.SearchStops(context.Background(), "asldkfjalsdkfj", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected no candidates, got %v", names(got))
	}
}

func TestSearchStops_DeterministicOrder(t *testing.T) {
	s := seededSearcher(t, searchFixture())
	var first []string
	for i := 0; i < 5; i++ {
		got, err := s.SearchStops(context.Background(), "Utrecht", 6)
		if err != nil {
			t.Fatal(err)
		}
		codes := make([]string, len(got))
		for j, st := range got {
			codes[j] = st.TPCCode
		}
		if first == nil {
			first = codes
			continue
		}
		if strings.Join(codes, ",") != strings.Join(first, ",") {
			t.Fatalf("run %d returned a different order: %v vs %v", i, codes, first)
		}
	}
}

// The predicate must be one the pg_trgm GIN index can serve. A function
// call like similarity(name, $1) > 0.1 forces a sequential scan of every
// row on every search. The table here is tiny, so the planner is told not
// to seq-scan; it then has to find an index path or fail.
func TestSearchStops_PredicateUsesTrigramIndex(t *testing.T) {
	s := seededSearcher(t, searchFixture())
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+searchStopsSQL, "Utrecht", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "idx_stops_name_trgm") {
		t.Errorf("search should use the trigram index; plan:\n%s", text)
	}
}

func pairsFixture() []Stop {
	var s []Stop
	// 30 platforms of one hub: 30 sources x 29 partners = 870 join rows,
	// well past the old global LIMIT 500.
	s = append(s, sameSpot("hub", "Arnhem, Centraal Station", 30, 51.985, 5.900, ptr("ah"))...)
	// Stops with no real location are stored at the upstream sentinel; they
	// must never pair with each other by "proximity".
	s = append(s, sameSpot("kerk", "Kerk", 3, sentinelLat, sentinelLng, nil)...)
	s = append(s, sameSpot("nb", "Nicolaas Beetsstraat", 2, 52.3654, 4.8653, nil)...)
	return s
}

func TestPairedStopsByCode_NoGlobalTruncation(t *testing.T) {
	s := seededSearcher(t, pairsFixture())
	codes := make([]string, 0, 30)
	for i := 1; i <= 30; i++ {
		codes = append(codes, fmt.Sprintf("hub%02d", i))
	}
	got, err := s.PairedStopsByCode(context.Background(), codes)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range codes {
		if len(got[c]) == 0 {
			t.Errorf("%s lost its pairs (only %d of %d sources came back)", c, len(got), len(codes))
		}
	}
}

func TestPairedStopsByCode_PerSourceCap(t *testing.T) {
	s := seededSearcher(t, pairsFixture())
	got, err := s.PairedStopsByCode(context.Background(), []string{"hub01"})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got["hub01"]); n == 0 || n > maxPairsPerStop {
		t.Errorf("expected 1..%d pairs for hub01, got %d", maxPairsPerStop, n)
	}
	for i := 1; i < len(got["hub01"]); i++ {
		if got["hub01"][i-1] >= got["hub01"][i] {
			t.Errorf("pairs should be sorted: %v", got["hub01"])
		}
	}
}

func TestPairedStopsByCode_SentinelCoordinatesNeverPair(t *testing.T) {
	s := seededSearcher(t, pairsFixture())
	got, err := s.PairedStopsByCode(context.Background(), []string{"kerk01", "nb01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["kerk01"]) != 0 {
		t.Errorf("sentinel-located stops must not pair, got %v", got["kerk01"])
	}
	if len(got["nb01"]) != 1 || got["nb01"][0] != "nb02" {
		t.Errorf("real stops still pair, got %v", got["nb01"])
	}
}

func TestPairedStopsByCode_EmptyInput(t *testing.T) {
	s := &PgStopSearcher{DB: (*sql.DB)(nil)}
	got, err := s.PairedStopsByCode(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Errorf("expected empty map without touching the DB, got %v %v", got, err)
	}
}
