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
)

type PgStopSearcher struct {
	DB *sql.DB
}

func (s *PgStopSearcher) SearchStops(ctx context.Context, query string, limit int) ([]Stop, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT tpc_code, name, town, latitude, longitude, stop_area_code
		FROM stops
		WHERE similarity(name, $1) > 0.1
		ORDER BY similarity(name, $1) DESC
		LIMIT $2`,
		query, limit)
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
// Stops without pairs are omitted from the map.
func (s *PgStopSearcher) PairedStopsByCode(ctx context.Context, codes []string) (map[string][]string, error) {
	if len(codes) == 0 {
		return map[string][]string{}, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT s.tpc_code, p.tpc_code
		FROM stops s
		JOIN stops p ON p.tpc_code <> s.tpc_code
		  AND p.name = s.name
		  AND ABS(p.latitude - s.latitude) < $2
		  AND ABS(p.longitude - s.longitude) < $3
		WHERE s.tpc_code = ANY($1)
		ORDER BY s.tpc_code, p.tpc_code
		LIMIT 500`,
		pq.Array(codes), proxLatDeg, proxLonDeg)
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
