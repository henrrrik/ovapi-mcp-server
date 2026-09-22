# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

OVapi MCP Server is an MCP (Model Context Protocol) server for Dutch public transport: it proxies the OVapi feed (bus, tram, metro, ferry) with fuzzy stop search backed by Postgres and pg_trgm, and the NS Reisinformatie API for trains (`train_*` tools, enabled by `NS_API_KEY`).

Deployed on Runway at https://ovapi-mcp-server.pqapp.dev

## Architecture

- `ovapiclient/` — HTTP client abstraction (`HTTPDoer` interface, `BuildURL`)
- `nsclient/` — NS Reisinformatie API client (`Ocp-Apim-Subscription-Key` header, RFC 7807 errors) and the in-memory station cache with name/code resolution; `Trains` bundles both for the tools
- `tools/` — MCP tool definitions (factory functions returning `(mcp.Tool, server.ToolHandlerFunc)`)
- `db/` — Postgres schema, upsert, and `PgStopSearcher` (implements `StopSearcher` interface)
- `cmd/scrape/` — CLI to populate the stops database from OVapi's `/tpc/` endpoint
- `server.go` — wires tools into MCP server; search tools are optional (nil-safe if no DATABASE_URL), train tools are optional (nil-safe if no NS_API_KEY)
- `main.go` — HTTP entry point: Streamable HTTP at `/mcp` (stateless), SSE at `/sse`, access log, graceful shutdown that closes SSE streams

## Build & Test
- `go test -v -race ./...` — run tests (matches CI)
- `gofmt -s -w .` — run before committing
- `go vet ./...` — run before committing
- `golangci-lint run ./...` — run before committing (CI enforces it via `.golangci.yml`; includes the gocyclo limit of 10 for non-test code)
- `go build ./...` — build all packages including cmd/scrape
- `OVAPI_TEST_DATABASE_URL=postgres://... go test ./db/` — Postgres integration tests for the search and pairing SQL (skipped when unset; the database is truncated and re-seeded, so point it at a throwaway one, e.g. `docker run -d -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:16`)

## Environment Variables
- `PORT` — server port (default 5000)
- `DATABASE_URL` — Postgres connection string; if unset, search tool is disabled
- `NS_API_KEY` — NS API subscription key (product "Ns-App" on apiportal.ns.nl); if unset, the `train_*` tools are disabled. Locally it lives in `.env.local` (git-ignored): `set -a; . ./.env.local; set +a`

## Scrape CLI
Populates the stops table by fetching all ~38K timing point codes from OVapi:
```
DATABASE_URL=postgres://... go run ./cmd/scrape
```
On Runway: `runway app exec -a ovapi-mcp-server -- /layers/paketo-buildpacks_go-build/targets/bin/scrape`

## Workflow
- Use Red/Green TDD
- Create a PR for all changes — do not push directly to main
- CI runs tests on push and PR via GitHub Actions
- Deploy to Runway is automatic on push to main (after tests pass)
