# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

OVapi MCP Server is an MCP (Model Context Protocol) server proxy for the OVapi public transport APIs.


## Build & Test
- `go test -v -race ./...` — run tests (matches CI)
- `go fmt ./...` — run before committing
- `go vet ./...` — run before committing

## Workflow
- Use Red/Green TDD
- Create a PR for all changes — do not push directly to main
- CI runs tests on push and PR via GitHub Actions

