# OVapi MCP Server

[![Go Report Card](https://goreportcard.com/badge/github.com/henrrrik/ovapi-mcp-server)](https://goreportcard.com/report/github.com/henrrrik/ovapi-mcp-server)
An MCP (Model Context Protocol) server that proxies the Dutch [OVapi](https://www.ovapi.nl/) public transport APIs, with fuzzy stop search powered by Postgres.


## Tools

| Tool | Description |
|------|-------------|
| `get_departures` | Get real-time departures by stop name. Fuzzy-searches the database and fetches departures for matching stops. |
| `search_stops` | Search for stops by name (e.g. "Amsterdam Centraal", "Schiphol"). Returns TPC codes, names, and coordinates. |
| `lines` | List all public transport lines, or get details for a specific line. |
| `journey` | Get journey/vehicle information including location data. |

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
- "Find stops near Schiphol"
- "Show me tram line 17 details"

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
