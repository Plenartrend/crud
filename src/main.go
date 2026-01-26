package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	api "plenartrend/crud/src/openAPI"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func buildDatabaseURL() string {

	// Otherwise, construct from separate variables (all required)
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		log.Fatal("DATABASE_HOST is required")
	}

	port := os.Getenv("DATABASE_PORT")
	if port == "" {
		log.Fatal("DATABASE_PORT is required")
	}

	user := os.Getenv("DATABASE_USER")
	if user == "" {
		log.Fatal("DATABASE_USER is required")
	}

	password := os.Getenv("DATABASE_PASSWORD")
	if password == "" {
		log.Fatal("DATABASE_PASSWORD is required")
	}

	dbname := os.Getenv("DATABASE_NAME")
	if dbname == "" {
		log.Fatal("DATABASE_NAME is required")
	}

	// Build connection string
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		user, password, host, port, dbname)

	// Add sslmode parameter only if DATABASE_SSLMODE is set
	if sslmode := os.Getenv("DATABASE_SSLMODE"); sslmode != "" {
		connStr += fmt.Sprintf("?sslmode=%s", sslmode)
	}

	return connStr
}

func main() {
	_ = godotenv.Load()

	dbURL := buildDatabaseURL()
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	server := NewServer(db)
	r := http.NewServeMux()
	h := api.HandlerFromMux(server, r)

	s := &http.Server{
		Handler: h,
		Addr:    ":8080",
	}

	log.Println("Server starting on :8080")
	log.Fatal(s.ListenAndServe())
}
