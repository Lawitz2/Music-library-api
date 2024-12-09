package apiserver

import (
	"ApiServer/internal/app/db"
	"os"
)

type Config struct {
	BindAddr string
	LogLevel string
	Database *db.Config
}

func NewConfig() *Config {
	return &Config{
		BindAddr: os.Getenv("BIND_PORT"),
		LogLevel: os.Getenv("LOG_LEVEL"),
		Database: db.NewConfig(),
	}
}
