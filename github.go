package seabird_webhook_receiver

import (
	"strings"

	"gopkg.in/go-playground/webhooks.v5/github"
)

// githubEvents are the GitHub events we parse. Others are skipped.
var githubEvents = []github.Event{
	github.PingEvent,
	github.IssuesEvent,
	github.IssueCommentEvent,
	github.PullRequestEvent,
	github.PullRequestReviewEvent,
	github.PushEvent,
}

// githubEvent converts a parsed GitHub payload, returning nil for payloads
// we don't handle.
func githubEvent(payload any) event {
	switch p := payload.(type) {
	case github.PingPayload:
		return pingEvent{
			Repo:   repo{p.Repository.FullName, p.Repository.HTMLURL},
			Sender: p.Sender.Login,
		}

	case github.IssuesPayload:
		return issueEvent{
			Repo:   repo{p.Repository.FullName, p.Repository.HTMLURL},
			Sender: p.Sender.Login,
			Action: p.Action,
			Number: p.Issue.Number,
			Title:  p.Issue.Title,
			URL:    p.Issue.HTMLURL,
		}

	case github.IssueCommentPayload:
		return commentEvent{
			Repo:        repo{p.Repository.FullName, p.Repository.HTMLURL},
			Sender:      p.Sender.Login,
			Action:      p.Action,
			PullRequest: p.Issue.PullRequest != nil,
			Number:      p.Issue.Number,
			Title:       p.Issue.Title,
			URL:         p.Comment.HTMLURL,
		}

	case github.PullRequestPayload:
		pr := p.PullRequest
		return pullRequestEvent{
			Repo:   repo{p.Repository.FullName, p.Repository.HTMLURL},
			Sender: p.Sender.Login,
			Action: p.Action,
			Merged: pr.Merged,
			Number: pr.Number,
			Title:  pr.Title,
			URL:    pr.HTMLURL,
			Head:   headLabel(pr.Head.Ref, pr.Head.Repo.FullName, pr.Base.Repo.FullName),
			Base:   pr.Base.Ref,
		}

	case github.PullRequestReviewPayload:
		state := ""
		if p.Action == "submitted" {
			state = strings.ToLower(p.Review.State)
		}
		return reviewEvent{
			Repo:   repo{p.Repository.FullName, p.Repository.HTMLURL},
			Sender: p.Sender.Login,
			State:  state,
			Number: p.PullRequest.Number,
			Title:  p.PullRequest.Title,
			URL:    p.Review.HTMLURL,
		}

	case github.PushPayload:
		ev := pushEvent{
			Repo:       repo{p.Repository.FullName, p.Repository.HTMLURL},
			Pusher:     p.Pusher.Name,
			Ref:        p.Ref,
			Deleted:    p.Deleted,
			Forced:     p.Forced,
			CompareURL: p.Compare,
		}
		if tag, ok := strings.CutPrefix(p.Ref, "refs/tags/"); ok {
			ev.TagURL = p.Repository.HTMLURL + "/releases/tag/" + escapePath(tag)
		}
		for _, c := range p.Commits {
			author := c.Author.Username
			if author == "" {
				author = c.Author.Name
			}
			ev.Commits = append(ev.Commits, commit{ID: c.ID, Message: c.Message, Author: author})
		}
		return ev
	}

	return nil
}
