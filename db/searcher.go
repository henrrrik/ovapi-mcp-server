package db

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// proxLatDeg is the bounding-box tolerance for paired-stop detection.
// At Dutch latitudes (~52°N), 0.001° lat ≈ 111 m and 0.0015° lon ≈ 103 m.
const (
	proxLatDeg = 0.001
	proxLonDeg = 0.0015

	// maxPairsPerStop caps the partners returned per source stop. The
	// largest real hub (Arnhem Centraal) has 14 same-named platforms; the
	// ranker's pair boost saturates at 5.
	maxPairsPerStop = 20

	// Upstream stores stops without a real location at this sentinel (it
	// lands in central France). Roughly one stop in eight carries it, so
	// same-named stops there ("Kerk", "Centrum") would otherwise pair with
	// hundreds of unrelated stops nationwide.
	sentinelLat = 47.974766
	sentinelLng = 3.3135424
	sentinelEps = 1e-4
)

// searchStopsSQL returns the candidate pool the Go ranker scores.
//
// Trigram similarity alone favours names close to the query's length, so a
// bare "Utrecht" filled the pool with "Utrecht, Neude"-style minor stops
// while "Utrecht, CS Centrumzijde" sat at rank 213 and never reached the
// ranker. Candidates are therefore ordered by the better of whole-string
// similarity and word similarity (does the query appear as a word or a
// prefix of the name), then by canonical hub name so the interchange enters
// a small pool ahead of same-scored minor stops, then by plain similarity.
// The % / <% operators are what the pg_trgm GIN index can serve; a
// similarity(...) > x predicate forced a sequential scan on every search.
// Thresholds are pg_trgm's defaults (0.3 / 0.6): a misspelling such as
// "Schipol" still reaches "Schiphol, Airport" through word similarity,
// while random strings match nothing.
const searchStopsSQL = `
	SELECT tpc_code, name, town, latitude, longitude, stop_area_code
	FROM stops
	WHERE name % $1 OR $1 <% name
	ORDER BY GREATEST(similarity(name, $1), word_similarity($1, name)) DESC,
	         (name ~* '\m(centraal|cs|station|airport)\M') DESC,
	         similarity(name, $1) DESC,
	         tpc_code
	LIMIT $2`

type PgStopSearcher struct {
	DB *sql.DB
}

func (s *PgStopSearcher) SearchStops(ctx context.Context, query string, limit int) ([]Stop, error) {
	rows, err := s.DB.QueryContext(ctx, searchStopsSQL, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stops []Stop
	for rows.Next() {
		var st Stop
		if err := rows.Scan(&st.TPCCode, &st.Name, &st.Town, &st.Latitude, &st.Longitude, &st.StopAreaCode); err != nil {
			return nil, err
		}
		stops = append(stops, st)
	}
	return stops, rows.Err()
}

// StopsInBBox returns stops whose coordinates fall inside the given
// axis-aligned bounding box. The caller is expected to compute Haversine
// distance from a reference point and sort themselves, since a bounding
// box doesn't give us a centroid to rank by.
func (s *PgStopSearcher) StopsInBBox(ctx context.Context, minLat, maxLat, minLng, maxLng float64, limit int) ([]Stop, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT tpc_code, name, town, latitude, longitude, stop_area_code
		FROM stops
		WHERE latitude BETWEEN $1 AND $2
		  AND longitude BETWEEN $3 AND $4
		LIMIT $5`,
		minLat, maxLat, minLng, maxLng, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stops []Stop
	for rows.Next() {
		var st Stop
		if err := rows.Scan(&st.TPCCode, &st.Name, &st.Town, &st.Latitude, &st.Longitude, &st.StopAreaCode); err != nil {
			return nil, err
		}
		stops = append(stops, st)
	}
	return stops, rows.Err()
}

// PairedStopsByCode returns a map from input tpc_code to the tpc_codes of stops
// that share the same name and sit within a small lat/lon bounding box.
// Stops without pairs are omitted from the map. Partners are capped per
// source (not globally: a single LIMIT over the whole join silently dropped
// every pair of the lexically later sources once hub-heavy candidate sets
// produced more rows than the cap), and stops at the upstream sentinel
// coordinate never pair.
func (s *PgStopSearcher) PairedStopsByCode(ctx context.Context, codes []string) (map[string][]string, error) {
	if len(codes) == 0 {
		return map[string][]string{}, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT s.tpc_code, p.tpc_code
		FROM stops s
		JOIN LATERAL (
		  SELECT p.tpc_code
		  FROM stops p
		  WHERE p.name = s.name
		    AND p.tpc_code <> s.tpc_code
		    AND ABS(p.latitude - s.latitude) < $2
		    AND ABS(p.longitude - s.longitude) < $3
		  ORDER BY p.tpc_code
		  LIMIT $4
		) p ON TRUE
		WHERE s.tpc_code = ANY($1)
		  AND NOT (ABS(s.latitude - $5) < $7 AND ABS(s.longitude - $6) < $7)
		ORDER BY s.tpc_code, p.tpc_code`,
		pq.Array(codes), proxLatDeg, proxLonDeg, maxPairsPerStop, sentinelLat, sentinelLng, sentinelEps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var src, pair string
		if err := rows.Scan(&src, &pair); err != nil {
			return nil, err
		}
		out[src] = append(out[src], pair)
	}
	return out, rows.Err()
}
