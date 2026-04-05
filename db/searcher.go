package db

import (
	"context"
	"database/sql"
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
