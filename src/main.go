package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	api "plenartrend/crud/src/openAPI"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func buildDatabaseURL() string {
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

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		user, password, host, port, dbname)

	if sslmode := os.Getenv("DATABASE_SSLMODE"); sslmode != "" {
		connStr += fmt.Sprintf("?sslmode=%s", sslmode)
	}

	return connStr
}

func main() {
	_ = godotenv.Load() // Do not fail if .env is missing, as we set the environment variables directly in production

	dbURL := buildDatabaseURL()

	var db *sqlx.DB
	for true {
		db, err := sqlx.Connect("postgres", dbURL)
		if err == nil {
			defer db.Close()
			break
		}
		log.Printf("Failed to connect to database: %v", err)
		time.Sleep(time.Second)
	}

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
