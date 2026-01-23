package main

import (
	"log"
	"net/http"
	"os"
	api "plenartrend/crud/src/openAPI"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	db, err := sqlx.Connect("postgres", os.Getenv("DATABASE_URL"))
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
