package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort string
	DatabaseURL string
}

func Load() (Config, error)  {

	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using system environment variables")
	}
	
	appPort := os.Getenv("PORT")

	if appPort == "" {
		appPort = "8080"
	}

	datbaseUrl := os.Getenv("DATABASE_URL")
	
	if datbaseUrl == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return Config{
		AppPort: appPort,
		DatabaseURL: datbaseUrl,
	}, nil
}