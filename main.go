package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/henrrrik/ovapi-mcp-server/db"
	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
	"github.com/henrrrik/ovapi-mcp-server/tools"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}

	client := ovapiclient.NewClient()

	var searcher tools.StopSearcher
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		conn, err := db.Open(dbURL)
		if err != nil {
			log.Fatalf("database connection failed: %v", err)
		}
		defer conn.Close()
		if err := db.Migrate(context.Background(), conn); err != nil {
			log.Fatalf("database migration failed: %v", err)
		}
		searcher = &db.PgStopSearcher{DB: conn}
		log.Println("database connected, stop search enabled")
	} else {
		log.Println("DATABASE_URL not set, stop search disabled")
	}

	mcpServer := NewOVapiServer(client, searcher)

	sseServer := server.NewSSEServer(mcpServer,
		server.WithKeepAlive(true),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"ovapi-mcp-server","sse_endpoint":"/sse"}`))
	})
	mux.Handle("/", sseServer)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("OVapi MCP server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
