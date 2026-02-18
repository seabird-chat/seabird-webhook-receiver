package seabird_webhook_receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// Forgejo payload structs

type ForgejoUser struct {
	Login string `json:"login"`
}

type ForgejoRepository struct {
	FullName string `json:"full_name"`
}

type ForgejoCommit struct {
	ID      string      `json:"id"`
	Message string      `json:"message"`
	Author  ForgejoUser `json:"author"`
}

type ForgejoPushPayload struct {
	Ref        string          `json:"ref"`
	Created    bool            `json:"created"`
	Deleted    bool            `json:"deleted"`
	Forced     bool            `json:"forced"`
	CompareURL string          `json:"compare_url"`
	Commits    []ForgejoCommit `json:"commits"`
	Pusher     ForgejoUser     `json:"pusher"`
	Repository ForgejoRepository `json:"repository"`
}

type ForgejoIssue struct {
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
}

type ForgejoIssuePayload struct {
	Action     string            `json:"action"`
	Issue      ForgejoIssue      `json:"issue"`
	Repository ForgejoRepository `json:"repository"`
	Sender     ForgejoUser       `json:"sender"`
}

type ForgejoPR struct {
	Number  int64  `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	Merged  bool   `json:"merged"`
	Base    struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

type ForgejoPullRequestPayload struct {
	Action      string            `json:"action"`
	PullRequest ForgejoPR         `json:"pull_request"`
	Repository  ForgejoRepository `json:"repository"`
	Sender      ForgejoUser       `json:"sender"`
}

type ForgejoCreatePayload struct {
	Ref        string            `json:"ref"`
	RefType    string            `json:"ref_type"`
	Repository ForgejoRepository `json:"repository"`
	Sender     ForgejoUser       `json:"sender"`
}

type ForgejoDeletePayload struct {
	Ref        string            `json:"ref"`
	RefType    string            `json:"ref_type"`
	Repository ForgejoRepository `json:"repository"`
	Sender     ForgejoUser       `json:"sender"`
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

