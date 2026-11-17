package config

import (
	"os"

	"github.com/joho/godotenv"
)

func InitEnv(approot string) {
	file, local, staging := approot+"/.env", approot+"/.env.local", approot+"/.env.staging"
	if _, err := os.Stat(local); err == nil {
		file = local
	}
	if _, err := os.Stat(staging); err == nil {
		hostname, err := os.Hostname()
		if err != nil {
			panic(err)
		}
		if hostname == "dev" {
			file = staging
		}
	}
	if err := godotenv.Load(file); err != nil {
		// In Docker, env vars are injected via docker-compose environment
		// block, so .env file is not required. Only panic when not in a
		// container (i.e. ROOT env var is not already set).
		if os.Getenv("ROOT") == "" {
			panic(err.Error())
		}
	}
}
