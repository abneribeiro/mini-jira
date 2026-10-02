package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/abneribeiro/internal/config"
	"github.com/abneribeiro/internal/database"
	"github.com/abneribeiro/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	db, err := database.New(ctx, cfg.DatabaseURL)

	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}

	defer db.Close()
	
	srv := server.New(db)

	log.Printf("server running on port %s", cfg.AppPort)
	if err := http.ListenAndServe(":"+cfg.AppPort, srv); err != nil {
		log.Fatalf("server failed to start: %v", err)
		os.Exit(1)
	}
}
