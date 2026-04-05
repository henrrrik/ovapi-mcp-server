package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

const migrationSQL = `
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS stops (
    tpc_code       TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    town           TEXT NOT NULL DEFAULT 'unknown',
    latitude       DOUBLE PRECISION,
    longitude      DOUBLE PRECISION,
    stop_area_code TEXT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stops_name_trgm ON stops USING GIN (name gin_trgm_ops);
`

func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, migrationSQL)
	return err
}

func UpsertStops(ctx context.Context, db *sql.DB, stops []Stop) error {
	if len(stops) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO stops (tpc_code, name, town, latitude, longitude, stop_area_code, updated_at)
VALUES `)

	args := make([]any, 0, len(stops)*6)
	for i, s := range stops {
		if i > 0 {
			b.WriteString(", ")
		}
		n := i * 6
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, $%d, NOW())",
			n+1, n+2, n+3, n+4, n+5, n+6)
		args = append(args, s.TPCCode, s.Name, s.Town, s.Latitude, s.Longitude, s.StopAreaCode)
	}

	b.WriteString(` ON CONFLICT (tpc_code) DO UPDATE SET
		name = EXCLUDED.name,
		town = EXCLUDED.town,
		latitude = EXCLUDED.latitude,
		longitude = EXCLUDED.longitude,
		stop_area_code = EXCLUDED.stop_area_code,
		updated_at = NOW()`)

	_, err := db.ExecContext(ctx, b.String(), args...)
	return err
}
