package seabird_webhook_receiver

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"gopkg.in/go-playground/webhooks.v5/github"

	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
)

type Server struct {
	logger          zerolog.Logger
	seabird         *seabird.Client
	github          *github.Webhook
	forgejoSecret   string
	targetChannelId string
}

type Config struct {
	Logger         zerolog.Logger
	SeabirdHost    string
	SeabirdToken   string
	SeabirdChannel string
	GithubToken    string
	ForgejoSecret  string
}

func NewServer(config Config) (*Server, error) {
	github, err := github.New(github.Options.Secret(config.GithubToken))
	if err != nil {
		return nil, err
	}

	seabird, err := seabird.NewClient(config.SeabirdHost, config.SeabirdToken)
	if err != nil {
		return nil, err
	}

	return &Server{
		logger:          config.Logger,
		seabird:         seabird,
		github:          github,
		forgejoSecret:   config.ForgejoSecret,
		targetChannelId: config.SeabirdChannel,
	}, nil
}

// send posts an event's message to the target channel, skipping events that
// have no message.
func (s *Server) send(ctx context.Context, ev event) error {
	block := ev.block()
	if block == nil {
		s.logger.Info().Msgf("Skipping %T", ev)
		return nil
	}

	_, err := s.seabird.Inner.SendMessage(ctx, &pb.SendMessageRequest{
		ChannelId: s.targetChannelId,
		RootBlock: block,
		Tags: map[string]string{
			"url/skip": "1",
		},
	})
	return err
}

func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := s.github.Parse(r, githubEvents...)
	if errors.Is(err, github.ErrEventNotFound) {
		s.logger.Info().Msgf("Skipping GitHub %q event", r.Header.Get("X-GitHub-Event"))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		s.logger.Error().Err(err).Msg("Got error when handling webhook")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.logger.Info().Msgf("Got %T payload", payload)

	ev := githubEvent(payload)
	if ev == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.send(r.Context(), ev); err != nil {
		s.logger.Error().Err(err).Msg("Got error when sending notification")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleForgejoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to read Forgejo webhook body")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !verifyForgejoSignature(s.forgejoSecret, body, r) {
		s.logger.Error().Msg("Forgejo webhook signature verification failed")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	name := r.Header.Get("X-Forgejo-Event")
	s.logger.Info().Msgf("Got Forgejo %q event", name)

	ev, err := forgejoEvent(name, body)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to parse Forgejo webhook")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if ev == nil {
		s.logger.Info().Msgf("Skipping Forgejo %q event", name)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.send(r.Context(), ev); err != nil {
		s.logger.Error().Err(err).Msg("Got error when sending Forgejo notification")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runHttp() error {
	r := chi.NewRouter()

	// The recommended middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// TODO: write custom Logger to work with zerolog

	r.Post("/webhooks/github", s.handleGithubWebhook)
	r.Post("/webhooks/forgejo", s.handleForgejoWebhook)

	return http.ListenAndServe(":3000", r)
}

func (s *Server) runSeabird() error {
	stream, err := s.seabird.StreamEvents(map[string]*pb.CommandMetadata{})
	if err != nil {
		return err
	}

	for event := range stream.C {
		s.logger.Debug().Msgf("Got seabird event: %T", event.Inner)
	}

	return errors.New("seabird stream ended")
}

func (s *Server) Run(ctx context.Context) error {
	// TODO: use the ctx Luke
	group, _ := errgroup.WithContext(ctx)

	group.Go(s.runHttp)
	group.Go(s.runSeabird)

	return group.Wait()
}
