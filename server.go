package seabird_webhook_receiver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"gopkg.in/go-playground/webhooks.v5/github"

	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
)

var mainBranches = []string{
	"refs/heads/main",
	"refs/heads/master",
}

var prActions = []string{
	"opened",
	"closed",
	"reopened",
	"merged",
}

type Server struct {
	logger          zerolog.Logger
	seabird         *seabird.Client
	github          *github.Webhook
	targetChannelId string
}

type Config struct {
	Logger         zerolog.Logger
	SeabirdHost    string
	SeabirdToken   string
	SeabirdChannel string
	GithubToken    string
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
		targetChannelId: config.SeabirdChannel,
	}, nil
}

func (s *Server) sendSeabirdMessagef(format string, args ...interface{}) error {
	_, err := s.seabird.Inner.SendMessage(context.TODO(), &pb.SendMessageRequest{
		ChannelId: s.targetChannelId,
		Text:      fmt.Sprintf(format, args...),
		Tags: map[string]string{
			"url/skip": "1",
		},
	})
	return err
}

func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := s.github.Parse(r,
		github.PingEvent,
		github.IssuesEvent,
		github.PullRequestEvent,
		github.PushEvent)

	if err != nil {
		s.logger.Error().Err(err).Msg("Got error when handling webhook")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.logger.Info().Msgf("Got %T payload", payload)

	//fmt.Printf("%+v\n", payload)

	switch event := payload.(type) {
	case github.PingPayload:
		err = s.sendSeabirdMessagef("Callback added by %s", event.Sender.Login)
	case github.IssuesPayload:
		err = s.sendSeabirdMessagef(
			"[%s] %s %s issue %q: %s",
			event.Repository.FullName,
			event.Sender.Login,
			event.Action,
			event.Issue.Title,
			event.Issue.HTMLURL,
		)
	case github.PullRequestPayload:
		if !contains(prActions, event.Action) {
			s.logger.Info().Msgf("Skipping pull request event of type %s for %q", event.Action, event.PullRequest.Title)
			break
		}

		action := event.Action
		if action == "closed" && event.PullRequest.Merged {
			action = "merged"
		}

		err = s.sendSeabirdMessagef(
			"[%s] %s %s pull request #%d: %q (%s...%s) %s",
			event.Repository.FullName,
			event.Sender.Login,
			action,
			event.PullRequest.Number,
			event.PullRequest.Title,
			event.PullRequest.Base.Ref,
			event.PullRequest.Head.Ref,
			event.PullRequest.HTMLURL,
		)
	case github.PushPayload:
		action := "pushed"
		if event.Deleted && !event.Created {
			action = "deleted"
		} else if event.Forced {
			action = "force pushed"
		}

		if strings.HasPrefix(event.Ref, "refs/tags/") {
			split := strings.SplitN(event.Ref, "/", 3)
			tag := "<unknown>"
			if len(split) == 3 {
				tag = split[2]
			}

			err = s.sendSeabirdMessagef(
				"[%s] %s %s tag %s: %s",
				event.Repository.FullName,
				event.Pusher.Name,
				action,
				tag,
				event.Compare,
			)

		} else {
			if !contains(mainBranches, event.Ref) {
				s.logger.Info().Msgf("Skipping push event for ref %s", event.Ref)
				break
			}

			split := strings.SplitN(event.Ref, "/", 3)
			branch := "<unknown>"
			if len(split) == 3 {
				branch = split[2]
			}

			err = s.sendSeabirdMessagef(
				"[%s] %s %s %d commit(s) to %s: %s",
				event.Repository.FullName,
				event.Pusher.Name,
				action,
				len(event.Commits),
				branch,
				event.Compare,
			)

			for _, commit := range event.Commits {
				err = s.sendSeabirdMessagef(
					"[%s] [%s] %s %s: %s",
					event.Repository.FullName,
					branch,
					commit.ID[:8],
					commit.Author.Username,
					commit.Message,
				)
			}
		}
	}

	if err != nil {
		s.logger.Error().Err(err).Msg("Got error when sending notification")
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
