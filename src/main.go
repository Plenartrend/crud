package main

import (
	"log"
	"net/http"
	api "plenartrend/crud/src/openAPI"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	server := NewServer()
	r := http.NewServeMux()
	h := api.HandlerFromMux(server, r)

	s := &http.Server{
		Handler: h,
		Addr:    ":8080",
	}

	log.Println("Server starting on :8080")
	log.Fatal(s.ListenAndServe())
}
