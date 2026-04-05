package main

import (
	"testing"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

func TestNewOVapiServer(t *testing.T) {
	client := ovapiclient.NewClient()
	s := NewOVapiServer(client, nil)
	if s == nil {
		t.Fatal("NewOVapiServer returned nil")
	}
}
