// Command migrate applies the embedded goose migrations.
//
//	migrate            apply every pending migration (same as "up")
//	migrate up         apply every pending migration
//	migrate down       roll back the most recent migration
//	migrate status     list applied and pending migrations
//	migrate version    print the current schema version
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/abneribeiro/internal/config"
	"github.com/abneribeiro/internal/migrations"
)

var allowedCommands = map[string]bool{
	"up":      true,
	"down":    true,
	"status":  true,
	"version": true,
}

func main() {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	if !allowedCommands[command] {
		log.Fatalf("unknown command %q: expected up, down, status or version", command)
	}

	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatal(err)
	}

	if err := goose.RunContext(ctx, command, db, "."); err != nil {
		log.Fatalf("migrate %s: %v", command, err)
	}

	if command == "up" {
		log.Println("migrations applied successfully")
	}
}
