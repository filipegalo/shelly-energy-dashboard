package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	profile := env("MOCK_PROFILE", "gen2")
	address := env("MOCK_LISTEN_ADDRESS", "0.0.0.0:80")
	log.Printf("mock Shelly profile=%s listening=%s", profile, address)
	log.Fatal(http.ListenAndServe(address, newMockHandler(profile)))
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
