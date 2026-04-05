package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/henrrrik/ovapi-mcp-server/db"
	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

const ovapiBase = "https://v0.ovapi.nl"
const batchSize = 500
const maxResponseSize = 10 * 1024 * 1024 // 10 MB

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	conn, err := db.Open(dbURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx, conn); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	log.Println("fetching TPC code list...")
	codes, err := fetchTPCList(ctx, client)
	if err != nil {
		log.Fatalf("failed to fetch TPC list: %v", err)
	}
	log.Printf("found %d TPC codes", len(codes))

	total := 0
	batches := (len(codes) + batchSize - 1) / batchSize
	for i := 0; i < len(codes); i += batchSize {
		end := i + batchSize
		if end > len(codes) {
			end = len(codes)
		}
		batch := codes[i:end]
		batchNum := i/batchSize + 1

		stops, err := fetchTPCBatch(ctx, client, batch)
		if err != nil {
			log.Printf("batch %d/%d failed: %v", batchNum, batches, err)
			continue
		}

		if err := db.UpsertStops(ctx, conn, stops); err != nil {
			log.Printf("batch %d/%d upsert failed: %v", batchNum, batches, err)
			continue
		}

		total += len(stops)
		log.Printf("batch %d/%d: upserted %d stops (total: %d)", batchNum, batches, len(stops), total)
	}

	log.Printf("done: %d stops upserted", total)
}

func fetchTPCList(ctx context.Context, client ovapiclient.HTTPDoer) ([]string, error) {
	u := ovapiclient.BuildURL(ovapiBase, "tpc")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"/", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OVapi returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	codes := make([]string, 0, len(data))
	for k := range data {
		codes = append(codes, k)
	}
	return codes, nil
}

func fetchTPCBatch(ctx context.Context, client ovapiclient.HTTPDoer, codes []string) ([]db.Stop, error) {
	joined := strings.Join(codes, ",")
	u := ovapiclient.BuildURL(ovapiBase, "tpc", joined)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OVapi returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}

	var data map[string]struct {
		Stop struct {
			TimingPointCode string  `json:"TimingPointCode"`
			TimingPointName string  `json:"TimingPointName"`
			TimingPointTown string  `json:"TimingPointTown"`
			Latitude        float64 `json:"Latitude"`
			Longitude       float64 `json:"Longitude"`
			StopAreaCode    *string `json:"StopAreaCode"`
		} `json:"Stop"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	stops := make([]db.Stop, 0, len(data))
	for _, v := range data {
		s := v.Stop
		if s.TimingPointCode == "" {
			continue
		}
		town := s.TimingPointTown
		if town == "" {
			town = "unknown"
		}
		stops = append(stops, db.Stop{
			TPCCode:      s.TimingPointCode,
			Name:         s.TimingPointName,
			Town:         town,
			Latitude:     s.Latitude,
			Longitude:    s.Longitude,
			StopAreaCode: s.StopAreaCode,
		})
	}

	return stops, nil
}
