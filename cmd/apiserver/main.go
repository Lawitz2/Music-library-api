package main

import (
	"ApiServer/internal/app/apiserver"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
)

var (
	envPath string
	debug   bool
)

func init() {
	flag.StringVar(&envPath, "p", `env\.env`, "Location of environment file")
	flag.BoolVar(&debug, "d", false, "Start service in debug")
}

func main() {
	flag.Parse()

	err := godotenv.Load(envPath)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	// перезаписывает log_level на debug даже если в .env указан другой уровень
	if debug {
		os.Setenv("LOG_LEVEL", "debug")
	}

	var level slog.Level
	err = level.UnmarshalText([]byte(os.Getenv("LOG_LEVEL")))
	if err != nil {
		slog.Error(err.Error())
		return
	}
	slog.SetLogLoggerLevel(level)

	config := apiserver.NewConfig()

	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
		<-sigchan
	}()

	server := apiserver.NewAPIServer(config)
	if err = server.Start(); err != nil {
		slog.Error(err.Error())
		return
	}
}
