package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

// minJWTSecretLen is the floor for an HS256 signing key. A short secret is
// recoverable offline from a single captured token, which lets an attacker
// mint a token for any user_id.
const minJWTSecretLen = 32

type Config struct {
	AppPort     string
	MetricsPort string
	AppEnv      string
	DatabaseURL string
	JWTSecret   string
}

// LoadDatabaseURL is for tools that only talk to the database, such as the
// migration runner. Making them go through Load would force them to carry a
// JWT secret they never use.
func LoadDatabaseURL() (string, error) {
	loadDotEnv()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return "", fmt.Errorf("DATABASE_URL is required")
	}

	return databaseURL, nil
}

func loadDotEnv() {
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file found, using system environment variables")
	}
}

func Load() (Config, error) {
	loadDotEnv()

	appPort := os.Getenv("PORT")
	if appPort == "" {
		appPort = "8080"
	}

	// Served on its own listener so /metrics is never reachable through the
	// public port. Compose does not publish this one to the host.
	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9091"
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "local"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}
	if len(jwtSecret) < minJWTSecretLen {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least %d characters, got %d", minJWTSecretLen, len(jwtSecret))
	}

	return Config{
		AppPort:     appPort,
		MetricsPort: metricsPort,
		AppEnv:      appEnv,
		DatabaseURL: databaseURL,
		JWTSecret:   jwtSecret,
	}, nil
}
