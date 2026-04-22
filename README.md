# OVapi MCP Server

[![Go Report Card](https://goreportcard.com/badge/github.com/henrrrik/ovapi-mcp-server)](https://goreportcard.com/report/github.com/henrrrik/ovapi-mcp-server)

An MCP (Model Context Protocol) server that proxies the Dutch [OVapi](https://www.ovapi.nl/) public transport APIs, with fuzzy stop search powered by Postgres.


## Tools

| Tool | Description |
|------|-------------|
| `get_departures` | Real-time departures by fuzzy stop name or one/more TPC codes. Lean shape by default (`verbose=true` for raw upstream); supports `line`, `direction`, `time_window_minutes`, `max_departures`, `include_paired`, `drop_empty` filters. |
| `search_stops` | Search for stops by name. Returns ranked matches with `score` (0–1000) and `paired_with` — opposite-direction platforms or adjacent quays at the same physical stop. Minimum query length 3. |
| `find_stops_near` | Nearest-first stops within a lat/lng radius (default 500 m, max 5 km). Haversine distance. |
| `lines` | Compact index of all lines, or details for a specific line (`line_id`, format `{owner}_{public_number}_{direction}`, e.g. `GVB_17_1`). Filters: `mode`, `owner`, `name_contains`. |
| `journey` | Lean journey shape with `stops` sorted by order. |

### Coverage

KV78turbo feed only: Dutch bus, tram, metro, ferry. **NS trains are not included** — they run on their own Reisinformatie API, which this server doesn't proxy.

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
