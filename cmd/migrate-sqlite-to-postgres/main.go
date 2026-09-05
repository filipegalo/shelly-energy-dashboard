package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
)

func main() {
	source := flag.String("source", "", "path to the source SQLite .db file")
	flag.Parse()
	if *source == "" {
		log.Fatal("usage: migrate-sqlite-to-postgres --source /data/shelly-inventory.db")
	}
	options := database.PostgresOptions{Username: os.Getenv("DATABASE_USERNAME"), Password: os.Getenv("DATABASE_PASSWORD"), Hostname: os.Getenv("DATABASE_HOSTNAME"), Port: os.Getenv("DATABASE_PORT"), Database: os.Getenv("DATABASE_DB"), SSLMode: os.Getenv("DATABASE_SSLMODE")}
	if options.Port == "" {
		options.Port = "5432"
	}
	if err := database.MigrateSQLiteToPostgres(context.Background(), *source, options); err != nil {
		log.Fatal(err)
	}
	fmt.Println("SQLite to PostgreSQL migration completed successfully")
}
