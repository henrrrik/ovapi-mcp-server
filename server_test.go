package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/nsclient"
	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

func TestNewOVapiServer(t *testing.T) {
	client := ovapiclient.NewClient()
	s := NewOVapiServer(client, nil, nil)
	if s == nil {
		t.Fatal("NewOVapiServer returned nil")
	}
}

func listToolNames(t *testing.T, s *server.MCPServer) []string {
	t.Helper()
	msg := s.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(out.Result.Tools))
	for i, tl := range out.Result.Tools {
		names[i] = tl.Name
	}
	return names
}

// The NS tools follow the same nil-safe pattern as the database-backed
// search: no NS_API_KEY, no train_* tools.
func TestNewOVapiServer_TrainToolsOnlyWithNSClient(t *testing.T) {
	client := ovapiclient.NewClient()
	without := listToolNames(t, NewOVapiServer(client, nil, nil))
	for _, n := range without {
		if strings.HasPrefix(n, "train_") {
			t.Errorf("train tool %s registered without an NS client", n)
		}
	}
	with := listToolNames(t, NewOVapiServer(client, nil, nsclient.NewTrains("k", client)))
	trainTools := 0
	for _, n := range with {
		if strings.HasPrefix(n, "train_") {
			trainTools++
		}
	}
	if trainTools != 4 {
		t.Errorf("expected 4 train_* tools, got %d in %v", trainTools, with)
	}
}
