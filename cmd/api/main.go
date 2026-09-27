package main

import (
	"log"
	"net/http"
	"os"

	"github.com/abneribeiro/internal/server"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using system environment variables")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	srv := server.New()

	log.Printf("server running on port %s", port)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		log.Fatalf("server failed to start: %v", err)
	}
}
