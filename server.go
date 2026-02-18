package seabird_webhook_receiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
				if err != nil {
					break
				}
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

	event := r.Header.Get("X-Forgejo-Event")
	s.logger.Info().Msgf("Got Forgejo %q event", event)

	switch event {
	case "push":
		var p ForgejoPushPayload
		if err = json.Unmarshal(body, &p); err != nil {
			break
		}

		action := "pushed"
		if p.Deleted && !p.Created {
			action = "deleted"
		} else if p.Forced {
			action = "force pushed"
		}

		if strings.HasPrefix(p.Ref, "refs/tags/") {
			split := strings.SplitN(p.Ref, "/", 3)
			tag := "<unknown>"
			if len(split) == 3 {
				tag = split[2]
			}
			err = s.sendSeabirdMessagef(
				"[%s] %s %s tag %s: %s",
				p.Repository.FullName,
				p.Pusher.Login,
				action,
				tag,
				p.CompareURL,
			)
		} else {
			if !contains(mainBranches, p.Ref) {
				s.logger.Info().Msgf("Skipping Forgejo push event for ref %s", p.Ref)
				break
			}

			split := strings.SplitN(p.Ref, "/", 3)
			branch := "<unknown>"
			if len(split) == 3 {
				branch = split[2]
			}

			err = s.sendSeabirdMessagef(
				"[%s] %s %s %d commit(s) to %s: %s",
				p.Repository.FullName,
				p.Pusher.Login,
				action,
				len(p.Commits),
				branch,
				p.CompareURL,
			)
			if err != nil {
				break
			}

			for _, commit := range p.Commits {
				shortID := commit.ID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				err = s.sendSeabirdMessagef(
					"[%s] [%s] %s %s: %s",
					p.Repository.FullName,
					branch,
					shortID,
					commit.Author.Login,
					commit.Message,
				)
				if err != nil {
					break
				}
			}
		}
	case "issues":
		var p ForgejoIssuePayload
		if err = json.Unmarshal(body, &p); err != nil {
			break
		}
		err = s.sendSeabirdMessagef(
			"[%s] %s %s issue %q: %s",
			p.Repository.FullName,
			p.Sender.Login,
			p.Action,
			p.Issue.Title,
			p.Issue.HTMLURL,
		)
	case "pull_request":
		var p ForgejoPullRequestPayload
		if err = json.Unmarshal(body, &p); err != nil {
			break
		}
		if !contains(prActions, p.Action) {
			s.logger.Info().Msgf("Skipping Forgejo pull request event of type %s for %q", p.Action, p.PullRequest.Title)
			break
		}

		action := p.Action
		if action == "closed" && p.PullRequest.Merged {
			action = "merged"
		}

		err = s.sendSeabirdMessagef(
			"[%s] %s %s pull request #%d: %q (%s...%s) %s",
			p.Repository.FullName,
			p.Sender.Login,
			action,
			p.PullRequest.Number,
			p.PullRequest.Title,
			p.PullRequest.Base.Ref,
			p.PullRequest.Head.Ref,
			p.PullRequest.HTMLURL,
		)
	case "create":
		var p ForgejoCreatePayload
		if err = json.Unmarshal(body, &p); err != nil {
			break
		}
		err = s.sendSeabirdMessagef(
			"[%s] %s created %s %s",
			p.Repository.FullName,
			p.Sender.Login,
			p.RefType,
			p.Ref,
		)
	case "delete":
		var p ForgejoDeletePayload
		if err = json.Unmarshal(body, &p); err != nil {
			break
		}
		err = s.sendSeabirdMessagef(
			"[%s] %s deleted %s %s",
			p.Repository.FullName,
			p.Sender.Login,
			p.RefType,
			p.Ref,
		)
	default:
		s.logger.Info().Msgf("Skipping unknown Forgejo event type %q", event)
	}

	if err != nil {
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
