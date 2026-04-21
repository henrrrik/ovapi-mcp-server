package main

import (
	"context"
	"crypto/tls"
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

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // OVapi has a misconfigured TLS certificate
		},
	}

	log.Println("fetching stop-area town map...")
	areaTowns, err := fetchStopAreaTowns(ctx, client)
	if err != nil {
		log.Printf("warning: stop-area town enrichment disabled: %v", err)
		areaTowns = map[string]string{}
	}
	log.Printf("loaded %d stop-area towns", len(areaTowns))

	log.Println("fetching TPC code list...")
	codes, err := fetchTPCList(ctx, client)
	if err != nil {
		log.Fatalf("failed to fetch TPC list: %v", err)
	}
	log.Printf("found %d TPC codes", len(codes))

	total := 0
	enriched := 0
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

		enriched += enrichTowns(stops, areaTowns)

		if err := db.UpsertStops(ctx, conn, stops); err != nil {
			log.Printf("batch %d/%d upsert failed: %v", batchNum, batches, err)
			continue
		}

		total += len(stops)
		log.Printf("batch %d/%d: upserted %d stops (total: %d)", batchNum, batches, len(stops), total)
	}

	log.Printf("done: %d stops upserted, %d towns enriched from stop-area data", total, enriched)
}

// enrichTowns fills in Town for stops whose upstream town is empty or "unknown"
// using the stop-area-code map. Returns the number of stops enriched.
func enrichTowns(stops []db.Stop, areaTowns map[string]string) int {
	n := 0
	for i := range stops {
		s := &stops[i]
		if s.Town != "" && s.Town != "unknown" {
			continue
		}
		if s.StopAreaCode == nil || *s.StopAreaCode == "" {
			continue
		}
		if town, ok := areaTowns[*s.StopAreaCode]; ok && town != "" {
			s.Town = town
			n++
		}
	}
	return n
}

// fetchStopAreaTowns pulls the master /stopareacode/ listing and returns a map
// of stop-area-code → TimingPointTown.
func fetchStopAreaTowns(ctx context.Context, client ovapiclient.HTTPDoer) (map[string]string, error) {
	u := ovapiclient.BuildURL(ovapiBase, "stopareacode") + "/"
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

	var raw map[string]struct {
		TimingPointTown string `json:"TimingPointTown"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	out := make(map[string]string, len(raw))
	for code, v := range raw {
		if v.TimingPointTown != "" && v.TimingPointTown != "unknown" {
			out[code] = v.TimingPointTown
		}
	}
	return out, nil
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
