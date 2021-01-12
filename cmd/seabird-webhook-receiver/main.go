package main

import (
	"context"
	"os"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"

	seabird_webhook_receiver "github.com/seabird-chat/seabird-webhook-receiver"
)

func EnvDefault(key string, def string) string {
	if ret, ok := os.LookupEnv(key); ok {
		return ret
	}
	return def
}

func Env(logger zerolog.Logger, key string) string {
	ret, ok := os.LookupEnv(key)

	if !ok {
		logger.Fatal().Str("var", key).Msg("Required environment variable not found")
	}

	return ret
}

func main() {
	// Attempt to load from .env if it exists
	_ = godotenv.Load()

	var logger zerolog.Logger

	if isatty.IsTerminal(os.Stdout.Fd()) {
		logger = zerolog.New(zerolog.NewConsoleWriter())
	} else {
		logger = zerolog.New(os.Stdout)
	}

	logger = logger.With().Timestamp().Logger()
	logger.Level(zerolog.DebugLevel)

	webhookReceiver, err := seabird_webhook_receiver.NewServer(seabird_webhook_receiver.Config{
		Logger:         logger,
		SeabirdHost:    Env(logger, "SEABIRD_HOST"),
		SeabirdToken:   Env(logger, "SEABIRD_TOKEN"),
		SeabirdChannel: Env(logger, "SEABIRD_CHANNEL"),
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start webhook receiver")
	}

	err = webhookReceiver.Run(context.Background())
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to run webhook receiver")
	}
}
