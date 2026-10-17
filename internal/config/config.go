package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv      string
	LogLevel    string
	LogFile     string
	HTTPAddr    string
	WorkerCount int
	DatabaseURL string
	RedisAddr   string
}

func Load() (Config, error) {
	// _ = godotenv.Load()

	cfg := Config{
		AppEnv:      getEnv("APP_ENV", "development"),
		LogLevel:    getEnv("LOG_LEVEL", "DEBUG"),
		LogFile:     getEnv("LOG_FILE", "logs/hookrelay.log"),
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		RedisAddr:   getEnv("REDIS_ADDR", "localhost:6379"),
	}

	workerCount, err := strconv.Atoi(getEnv("WORKER_COUNT", "5"))
	if err != nil || workerCount < 1 {
		return Config{}, fmt.Errorf("WORKER_COUNT must be a positive integer")
	}

	cfg.WorkerCount = workerCount

	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return Config{}, fmt.Errorf("HTTP_ADDR cannot be empty")
	}

	return cfg, nil
}

func getEnv(key string, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	return value
}
