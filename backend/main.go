package main

import (
	"log"
	"net/http"
	"os"

	"github.com/learn/kuber-crd/backend/db"
	"github.com/learn/kuber-crd/backend/server"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/urls.db"
	}
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	store, err := db.NewStore(dbPath)
	if err != nil {
		log.Fatalf("failed to initialize db: %v", err)
	}
	defer store.Close()

	srv := server.NewServer(store, baseURL)
	log.Printf("Starting URL Shortener Backend on :%s (Base URL: %s, DB: %s)...", port, baseURL, dbPath)
	if err := http.ListenAndServe(":"+port, srv.Router()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
