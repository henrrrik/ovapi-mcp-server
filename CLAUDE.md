# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

OVapi MCP Server is an MCP (Model Context Protocol) server that proxies the Dutch OVapi public transport APIs, with fuzzy stop search backed by Postgres and pg_trgm.

Deployed on Runway at https://ovapi-mcp-server.pqapp.dev

## Architecture

- `ovapiclient/` — HTTP client abstraction (`HTTPDoer` interface, `BuildURL`)
- `tools/` — MCP tool definitions (factory functions returning `(mcp.Tool, server.ToolHandlerFunc)`)
- `db/` — Postgres schema, upsert, and `PgStopSearcher` (implements `StopSearcher` interface)
- `cmd/scrape/` — CLI to populate the stops database from OVapi's `/tpc/` endpoint
- `server.go` — wires tools into MCP server; search tool is optional (nil-safe if no DATABASE_URL)
- `main.go` — SSE transport entry point with graceful shutdown

## Build & Test
- `go test -v -race ./...` — run tests (matches CI)
- `go fmt ./...` — run before committing
- `go vet ./...` — run before committing
- `go build ./...` — build all packages including cmd/scrape

## Environment Variables
- `PORT` — server port (default 5000)
- `DATABASE_URL` — Postgres connection string; if unset, search tool is disabled

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
