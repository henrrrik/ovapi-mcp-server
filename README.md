# OVapi MCP Server

[![Go Report Card](https://goreportcard.com/badge/github.com/henrrrik/ovapi-mcp-server)](https://goreportcard.com/report/github.com/henrrrik/ovapi-mcp-server)

An MCP (Model Context Protocol) server that proxies the Dutch [OVapi](https://www.ovapi.nl/) public transport APIs, with fuzzy stop search powered by Postgres.


## Tools

| Tool | Description |
|------|-------------|
| `get_departures` | Real-time departures by fuzzy stop name or one/more TPC codes. Lean shape by default (`verbose=true` for raw upstream); supports `line`, `direction`, `time_window_minutes`, `max_departures`, `include_paired`, `drop_empty` filters. `stop_name` goes through the same ranker as `search_stops`, so `"Schiphol"` resolves to the airport rather than `Schipholweg`. |
| `search_stops` | Search for stops by name. Returns ranked matches with `score` (0–1000) and `paired_with` — opposite-direction platforms or adjacent quays at the same physical stop. Minimum query length 3. |
| `find_stops_near` | Nearest-first stops within a lat/lng radius (default 500 m, max 5 km). Haversine distance. |
| `lines` | Compact index of all lines, or details for a specific line (`line_id`, format `{owner}_{public_number}_{direction}`, e.g. `GVB_17_1`). Filters: `mode`, `owner`, `public_number` (exact), `name_contains`. |
| `journey` | Lean journey shape with `stops[]` in travel order (`target_*` scheduled, `expected_*` realtime-adjusted, `stop_type` ∈ {FIRST, INTERMEDIATE, LAST}). |

### Coverage

KV78turbo feed only: Dutch bus, tram, metro, ferry. Operators include GVB (Amsterdam), HTM (The Hague), RET (Rotterdam), Qbuzz, Connexxion (CXX), Arriva (ARR), EBS, Keolis, and regional concessions.

**NS intercity/sprinter trains are not included** — they run on their own Reisinformatie API, which this server doesn't proxy.

### Data freshness & nulls

- **Realtime vs planned.** `status = PLANNED` means the vehicle hasn't yet reported — `expected` equals `planned`, `delay_seconds` is 0. `DRIVING`/`ARRIVED`/`PASSED` carry live updates; these are typically within 30 seconds of the vehicle's actual position. `OFFROUTE` and `CANCEL` surface operational disruptions.
- **Commonly-null fields.** `platform`, `wheelchair_accessible`, and `number_of_coaches` are realtime-enriched and often null for `PLANNED` departures. `wheelchair_accessible="UNKNOWN"` upstream is collapsed to `null` in the lean shape; `number_of_coaches=0` is likewise reported as `null`. `coord` is `null` when upstream emits its sentinel coordinate (a stop lacks a real location) — roughly 20% of stops.
- **Status enum.** Upstream emits `PLANNED | DRIVING | ARRIVED | PASSED | CANCEL | OFFROUTE` verbatim (note: `CANCEL`, not `CANCELLED`). `mode` is lowercase upstream TransportType: `bus | tram | metro | boat`. Ferries appear as `"boat"`; `lines` accepts `"ferry"` as an input alias only.
- **Line-filter ambiguity.** When `get_departures` is called with a `line` filter and a stop returns an empty list, the response cannot distinguish "line does not serve this stop" from "line serves it but has no upcoming departures". Use `search_stops` + `lines` to check static coverage if that matters.
- **Rate limits.** None are documented by upstream; stops and lines are effectively static, so callers can cache `search_stops` and `lines` results. `get_departures` and `journey` should be treated as live.
- **Verbose escape hatch.** Every tool that returns a lean shape accepts `verbose: true` to pass the raw upstream body through unchanged — useful when debugging field mapping or pulling upstream fields we don't surface in the lean shape. Filters still apply in verbose mode.

## Usage with Claude Desktop

Add to your Claude Desktop MCP config:

```json
{
  "mcpServers": {
    "ovapi": {
      "type": "sse",
      "url": "https://ovapi-mcp-server.pqapp.dev/sse"
    }
  }
}
```

Then ask Claude things like:
- "What are the next departures from Amsterdam Centraal?"
- "Find stops within 400 m of 52.3676, 4.9041"
- "Show me tram line 17 details" (i.e. `GVB_17_1`)

## Development

```bash
go test -v -race ./...   # run tests
go fmt ./...             # format
go vet ./...             # lint
```

### Scrape CLI

The stop search database is populated by scraping all ~38K timing point codes from OVapi's `/tpc/` endpoint:

```bash
DATABASE_URL=postgres://... go run ./cmd/scrape
```

## Architecture

- **Go** with [mcp-go](https://github.com/mark3labs/mcp-go) for the MCP server
- **Postgres** with `pg_trgm` for fuzzy stop name search
- **SSE transport** for MCP communication
- Deployed on [Runway](https://www.runway.horse/)

## License

MIT
