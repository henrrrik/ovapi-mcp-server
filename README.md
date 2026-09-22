# OVapi MCP Server

[![Go Report Card](https://goreportcard.com/badge/github.com/henrrrik/ovapi-mcp-server)](https://goreportcard.com/report/github.com/henrrrik/ovapi-mcp-server)

An MCP (Model Context Protocol) server that proxies the Dutch [OVapi](https://www.ovapi.nl/) public transport APIs, with fuzzy stop search powered by Postgres.


## Tools

| Tool | Description |
|------|-------------|
| `get_departures` | Real-time departures by fuzzy stop name or one/more TPC codes. Lean shape by default (`verbose=true` for raw upstream); supports `line`, `direction`, `time_window_minutes`, `max_departures`, `drop_empty` filters, plus `include_paired` with `tpc_code`. Each departure carries a `journey_id` for `journey` and a `line_id` for `lines`; a `line` filter adds `line_served_here` per stop. `stop_name` goes through the same ranker as `search_stops`, so `"Schiphol"` resolves to the airport rather than `Schipholweg`. |
| `search_stops` | Search for stops by name. Returns ranked matches with `score` (0–1000) and `paired_with` — opposite-direction platforms or adjacent quays at the same physical stop. Minimum query length 3. |
| `find_stops_near` | Nearest-first stops within a lat/lng radius (default 500 m, max 5 km). Haversine distance. |
| `lines` | Compact index of all lines, or details for a specific line by `line_id` (format `{operator}_{planning_number}_{direction}`, e.g. `GVB_17_1`; take it from a departure or journey, since the index's own `NL_...` keys do not resolve). Detail includes `route[]` and `active_journeys[]` with `current_tpc_code`. Filters: `mode`, `owner`, `public_number` (exact), `name_contains`. |
| `journey` | Lean journey shape with `line_id` and `stops[]` in travel order (`target_*` scheduled, `expected_*` realtime-adjusted, `stop_type` ∈ {FIRST, INTERMEDIATE, LAST}). |

### Coverage

KV78turbo feed only: Dutch bus, tram, metro, ferry. Operators include GVB (Amsterdam), HTM (The Hague), RET (Rotterdam), Qbuzz, Connexxion (CXX), Arriva (ARR), EBS, Keolis, and regional concessions.

**NS intercity/sprinter trains are not included** — they run on their own Reisinformatie API, which this server doesn't proxy.

### Data freshness & nulls

- **Realtime vs planned.** `status = PLANNED` means the vehicle hasn't yet reported — `expected` equals `planned`, `delay_seconds` is 0. `DRIVING`/`ARRIVED`/`PASSED` carry live updates; these are typically within 30 seconds of the vehicle's actual position. `OFFROUTE` and `CANCEL` surface operational disruptions.
- **Commonly-null fields.** `platform`, `wheelchair_accessible`, and `number_of_coaches` are realtime-enriched and often null for `PLANNED` departures. `wheelchair_accessible="UNKNOWN"` upstream is collapsed to `null` in the lean shape; `number_of_coaches=0` is likewise reported as `null`. `coord` is `null` when upstream emits its sentinel coordinate (a stop lacks a real location) — roughly 12% of stops.
- **Status enum.** Upstream emits `PLANNED | DRIVING | ARRIVED | PASSED | CANCEL | OFFROUTE` verbatim (note: `CANCEL`, not `CANCELLED`). `mode` is lowercase upstream TransportType: `bus | tram | metro | boat`. Ferries appear as `"boat"`; `lines` accepts `"ferry"` as an input alias only.
- **Line-filter ambiguity.** When `get_departures` is called with a `line` filter, each stop carries `line_served_here` (lean shape only): `true` means the line appears in the upstream feed for that stop and other filters emptied the list; `false` means no pass for that line is in the current upstream window, which still cannot distinguish "never serves this stop" from "no passes right now". Use the `lines` tool's `route[]` to check static coverage if that matters. `drop_empty` removes exactly the stops this field describes.
- **Line and journey ids.** Take `line_id` and `journey_id` from `get_departures` (or `line_id` from `journey`) and pass them on verbatim. Upstream keys its `/line` index and its journeys by data owner (`NL_...`), but the detail endpoints only resolve operator-prefixed ids (`GVB_17_1`, `CXX_20260922_M300_167_0`); the server emits the resolvable form. The planning number in a line id is often not the public line number.
- **Timestamps.** Every time in a lean shape carries the Europe/Amsterdam offset (`2026-04-22T14:23:30+02:00`), including `server_time`, which upstream reports in UTC.
- **Rate limits.** None are documented by upstream; stops and lines are effectively static, so callers can cache `search_stops` and `lines` results. `get_departures` and `journey` should be treated as live.
- **Verbose escape hatch.** `get_departures`, `lines` and `journey` accept `verbose: true` to return the upstream shape with its original field names — useful when debugging field mapping or pulling upstream fields we don't surface in the lean shape. Filters, `drop_empty` and the index `limit` still apply; lean-only additions (`line_served_here`, `line_id`, id and timestamp normalisation) do not. `search_stops` and `find_stops_near` are served from the local database and have no verbose form.

## Connecting

The hosted instance serves two transports:

- **Streamable HTTP** (recommended) at `https://ovapi-mcp-server.pqapp.dev/mcp` — stateless, so it survives server restarts and works behind multiple replicas.
- **SSE** at `https://ovapi-mcp-server.pqapp.dev/sse` — for clients that only speak the older transport. Sessions live in one process and are dropped on restart. A Streamable HTTP client that was configured with this URL (it `POST`s here instead of opening the stream) is served as Streamable HTTP, so existing connectors keep working.

### Claude Desktop

Add to your Claude Desktop MCP config:

```json
{
  "mcpServers": {
    "ovapi": {
      "type": "http",
      "url": "https://ovapi-mcp-server.pqapp.dev/mcp"
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
go test -v -race ./...   # run tests (matches CI)
gofmt -s -w .            # format
go vet ./...             # lint
gocyclo -over 10 .       # complexity (github.com/fzipp/gocyclo)
go build ./...           # build all packages including cmd/scrape
```

The search and pairing SQL in `db/` has Postgres integration tests that run only when `OVAPI_TEST_DATABASE_URL` is set. Point it at a throwaway database, since the `stops` table is truncated and re-seeded:

```bash
docker run -d --name ovapi-pg -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:16
OVAPI_TEST_DATABASE_URL='postgres://postgres:pg@localhost:55432/postgres?sslmode=disable' go test ./db/
```

### Scrape CLI

The stop search database is populated by scraping all ~38K timing point codes from OVapi's `/tpc/` endpoint:

```bash
DATABASE_URL=postgres://... go run ./cmd/scrape
```

## Architecture

- **Go** with [mcp-go](https://github.com/mark3labs/mcp-go) for the MCP server
- **Postgres** with `pg_trgm` for fuzzy stop name search
- **Streamable HTTP** (`/mcp`, stateless) and **SSE** (`/sse`) transports
- Deployed on [Runway](https://www.runway.horse/)

## License

MIT
