package seabird_webhook_receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// Forgejo payload structs, covering only the fields we use.

type forgejoUser struct {
	Login string `json:"login"`
}

// forgejoPayloadUser is the user attached to a commit.
type forgejoPayloadUser struct {
	Name     string `json:"name"`
	UserName string `json:"username"`
}

type forgejoRepository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

func (r forgejoRepository) repo() repo {
	return repo{r.FullName, r.HTMLURL}
}

type forgejoCommit struct {
	ID      string             `json:"id"`
	Message string             `json:"message"`
	Author  forgejoPayloadUser `json:"author"`
}

type forgejoPushPayload struct {
	Ref          string            `json:"ref"`
	Before       string            `json:"before"`
	After        string            `json:"after"`
	CompareURL   string            `json:"compare_url"`
	Commits      []forgejoCommit   `json:"commits"`
	TotalCommits int               `json:"total_commits"`
	Pusher       forgejoUser       `json:"pusher"`
	Repository   forgejoRepository `json:"repository"`
}

type forgejoIssue struct {
	Number  int64  `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
}

type forgejoIssuePayload struct {
	Action     string            `json:"action"`
	Issue      forgejoIssue      `json:"issue"`
	Repository forgejoRepository `json:"repository"`
	Sender     forgejoUser       `json:"sender"`
}

type forgejoBranch struct {
	Ref  string             `json:"ref"`
	Repo *forgejoRepository `json:"repo"`
}

func (b forgejoBranch) repoName() string {
	if b.Repo == nil {
		return ""
	}
	return b.Repo.FullName
}

type forgejoPR struct {
	Number  int64         `json:"number"`
	Title   string        `json:"title"`
	HTMLURL string        `json:"html_url"`
	Merged  bool          `json:"merged"`
	Base    forgejoBranch `json:"base"`
	Head    forgejoBranch `json:"head"`
}

type forgejoPullRequestPayload struct {
	Action      string            `json:"action"`
	PullRequest forgejoPR         `json:"pull_request"`
	Repository  forgejoRepository `json:"repository"`
	Sender      forgejoUser       `json:"sender"`
}

type forgejoCommentPayload struct {
	Action  string       `json:"action"`
	Issue   forgejoIssue `json:"issue"`
	Comment struct {
		HTMLURL string `json:"html_url"`
	} `json:"comment"`
	IsPull     bool              `json:"is_pull"`
	Repository forgejoRepository `json:"repository"`
	Sender     forgejoUser       `json:"sender"`
}

// forgejoReviewStates maps the X-Forgejo-Event header of review events to
// review states.
var forgejoReviewStates = map[string]string{
	"pull_request_approved": reviewApproved,
	"pull_request_rejected": reviewChangesRequested,
	"pull_request_comment":  reviewCommented,
}

// forgejoEvent decodes a Forgejo payload by its X-Forgejo-Event header,
// returning nil for events we don't handle.
func forgejoEvent(name string, body []byte) (event, error) {
	switch name {
	case "push":
		var p forgejoPushPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}

		ev := pushEvent{
			Repo:         p.Repository.repo(),
			Pusher:       p.Pusher.Login,
			Ref:          p.Ref,
			Deleted:      isZeroSHA(p.After),
			CompareURL:   p.CompareURL,
			TotalCommits: p.TotalCommits,
		}
		if tag, ok := strings.CutPrefix(p.Ref, "refs/tags/"); ok {
			ev.TagURL = p.Repository.HTMLURL + "/src/tag/" + escapePath(tag)
		}
		for _, c := range p.Commits {
			author := c.Author.UserName
			if author == "" {
				author = c.Author.Name
			}
			ev.Commits = append(ev.Commits, commit{ID: c.ID, Message: c.Message, Author: author})
		}
		return ev, nil

	case "issues":
		var p forgejoIssuePayload
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}

		return issueEvent{
			Repo:   p.Repository.repo(),
			Sender: p.Sender.Login,
			Action: p.Action,
			Number: p.Issue.Number,
			Title:  p.Issue.Title,
			URL:    p.Issue.HTMLURL,
		}, nil

	case "issue_comment":
		var p forgejoCommentPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}

		return commentEvent{
			Repo:        p.Repository.repo(),
			Sender:      p.Sender.Login,
			Action:      p.Action,
			PullRequest: p.IsPull,
			Number:      p.Issue.Number,
			Title:       p.Issue.Title,
			URL:         p.Comment.HTMLURL,
		}, nil

	case "pull_request":
		var p forgejoPullRequestPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}

		pr := p.PullRequest
		return pullRequestEvent{
			Repo:   p.Repository.repo(),
			Sender: p.Sender.Login,
			Action: p.Action,
			Merged: pr.Merged,
			Number: pr.Number,
			Title:  pr.Title,
			URL:    pr.HTMLURL,
			Head:   headLabel(pr.Head.Ref, pr.Head.repoName(), pr.Base.repoName()),
			Base:   pr.Base.Ref,
		}, nil

	case "pull_request_approved", "pull_request_rejected", "pull_request_comment":
		var p forgejoPullRequestPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}

		// Review payloads don't link the review itself.
		return reviewEvent{
			Repo:   p.Repository.repo(),
			Sender: p.Sender.Login,
			State:  forgejoReviewStates[name],
			Number: p.PullRequest.Number,
			Title:  p.PullRequest.Title,
			URL:    p.PullRequest.HTMLURL,
		}, nil
	}

	return nil, nil
}

// verifyForgejoSignature checks the HMAC-SHA256 signature from the X-Forgejo-Signature header.
// Returns true if signature is valid, or if no secret is configured.
func verifyForgejoSignature(secret string, body []byte, r *http.Request) bool {
	if secret == "" {
		return true
	}
	sig := r.Header.Get("X-Forgejo-Signature")
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}
