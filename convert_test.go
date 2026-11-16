package seabird_webhook_receiver

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/go-playground/webhooks.v5/github"
)

const zeroSHA = "0000000000000000000000000000000000000000"

func TestForgejoEvent(t *testing.T) {
	forgejoRepo := repo{"belak/seabird", "https://forgejo.example.com/belak/seabird"}
	repoJSON := `"repository": {"full_name": "belak/seabird", "html_url": "https://forgejo.example.com/belak/seabird"}`

	tests := []struct {
		name  string
		event string
		body  string
		want  event
	}{
		{
			name:  "push",
			event: "push",
			body: `{
				"ref": "refs/heads/main", "before": "aaaa", "after": "bbbb",
				"compare_url": "https://forgejo.example.com/compare", "total_commits": 1,
				"commits": [{"id": "bbbb", "message": "fix: thing\n", "author": {"name": "Jay", "username": "jsvana"}}],
				"pusher": {"login": "belak"}, ` + repoJSON + `
			}`,
			want: pushEvent{
				Repo: forgejoRepo, Pusher: "belak", Ref: "refs/heads/main",
				CompareURL: "https://forgejo.example.com/compare", TotalCommits: 1,
				Commits: []commit{{ID: "bbbb", Message: "fix: thing\n", Author: "jsvana"}},
			},
		},
		{
			name:  "push author without username",
			event: "push",
			body: `{
				"ref": "refs/heads/main", "before": "aaaa", "after": "bbbb",
				"commits": [{"id": "bbbb", "message": "fix", "author": {"name": "Jay", "username": ""}}],
				"pusher": {"login": "belak"}, ` + repoJSON + `
			}`,
			want: pushEvent{
				Repo: forgejoRepo, Pusher: "belak", Ref: "refs/heads/main",
				Commits: []commit{{ID: "bbbb", Message: "fix", Author: "Jay"}},
			},
		},
		{
			name:  "tag deleted",
			event: "push",
			body: `{
				"ref": "refs/tags/v1.0/rc 1", "before": "aaaa", "after": "` + zeroSHA + `",
				"commits": [], "pusher": {"login": "belak"}, ` + repoJSON + `
			}`,
			want: pushEvent{
				Repo: forgejoRepo, Pusher: "belak", Ref: "refs/tags/v1.0/rc 1", Deleted: true,
				TagURL: "https://forgejo.example.com/belak/seabird/src/tag/v1.0/rc%201",
			},
		},
		{
			name:  "issue",
			event: "issues",
			body: `{
				"action": "opened", "sender": {"login": "belak"}, ` + repoJSON + `,
				"issue": {"number": 3, "title": "Broken", "html_url": "https://forgejo.example.com/issues/3"}
			}`,
			want: issueEvent{
				Repo: forgejoRepo, Sender: "belak", Action: "opened",
				Number: 3, Title: "Broken", URL: "https://forgejo.example.com/issues/3",
			},
		},
		{
			name:  "pull request comment",
			event: "issue_comment",
			body: `{
				"action": "created", "is_pull": true, "sender": {"login": "belak"}, ` + repoJSON + `,
				"issue": {"number": 4, "title": "Blocks", "html_url": "https://forgejo.example.com/pulls/4"},
				"comment": {"html_url": "https://forgejo.example.com/pulls/4#issuecomment-1"}
			}`,
			want: commentEvent{
				Repo: forgejoRepo, Sender: "belak", Action: "created", PullRequest: true,
				Number: 4, Title: "Blocks", URL: "https://forgejo.example.com/pulls/4#issuecomment-1",
			},
		},
		{
			name:  "pull request from fork",
			event: "pull_request",
			body: `{
				"action": "closed", "sender": {"login": "belak"}, ` + repoJSON + `,
				"pull_request": {
					"number": 4, "title": "Blocks", "html_url": "https://forgejo.example.com/pulls/4", "merged": true,
					"base": {"ref": "main", "repo": {"full_name": "belak/seabird"}},
					"head": {"ref": "blocks", "repo": {"full_name": "jsvana/seabird"}}
				}
			}`,
			want: pullRequestEvent{
				Repo: forgejoRepo, Sender: "belak", Action: "closed", Merged: true,
				Number: 4, Title: "Blocks", URL: "https://forgejo.example.com/pulls/4",
				Head: "jsvana:blocks", Base: "main",
			},
		},
		{
			name:  "review rejected",
			event: "pull_request_rejected",
			body: `{
				"action": "reviewed", "sender": {"login": "jsvana"}, ` + repoJSON + `,
				"pull_request": {"number": 4, "title": "Blocks", "html_url": "https://forgejo.example.com/pulls/4"},
				"review": {"type": "pull_request_review_rejected", "content": "no"}
			}`,
			want: reviewEvent{
				Repo: forgejoRepo, Sender: "jsvana", State: reviewChangesRequested,
				Number: 4, Title: "Blocks", URL: "https://forgejo.example.com/pulls/4",
			},
		},
		{
			name:  "create",
			event: "create",
			body:  `{"ref": "v1.0", "ref_type": "tag"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := forgejoEvent(tt.event, []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

func TestGithubEvent(t *testing.T) {
	githubRepo := repo{"seabird-chat/seabird-core", "https://github.com/seabird-chat/seabird-core"}
	repoJSON := `"repository": {"full_name": "seabird-chat/seabird-core", "html_url": "https://github.com/seabird-chat/seabird-core"}`

	tests := []struct {
		name    string
		payload any
		body    string
		want    event
	}{
		{
			name:    "push tag",
			payload: &github.PushPayload{},
			body: `{
				"ref": "refs/tags/v0.3.1", "created": true, "compare": "https://github.com/compare",
				"commits": [], "pusher": {"name": "belak"}, ` + repoJSON + `
			}`,
			want: pushEvent{
				Repo: githubRepo, Pusher: "belak", Ref: "refs/tags/v0.3.1",
				CompareURL: "https://github.com/compare",
				TagURL:     "https://github.com/seabird-chat/seabird-core/releases/tag/v0.3.1",
			},
		},
		{
			name:    "push commits",
			payload: &github.PushPayload{},
			body: `{
				"ref": "refs/heads/main", "forced": true, "compare": "https://github.com/compare",
				"commits": [
					{"id": "aaaa", "message": "one", "author": {"name": "Jay", "username": "jsvana"}},
					{"id": "bbbb", "message": "two", "author": {"name": "Not On GitHub", "username": ""}}
				],
				"pusher": {"name": "belak"}, ` + repoJSON + `
			}`,
			want: pushEvent{
				Repo: githubRepo, Pusher: "belak", Ref: "refs/heads/main", Forced: true,
				CompareURL: "https://github.com/compare",
				Commits: []commit{
					{ID: "aaaa", Message: "one", Author: "jsvana"},
					{ID: "bbbb", Message: "two", Author: "Not On GitHub"},
				},
			},
		},
		{
			name:    "issue comment on pull request",
			payload: &github.IssueCommentPayload{},
			body: `{
				"action": "created", "sender": {"login": "belak"}, ` + repoJSON + `,
				"issue": {"number": 17, "title": "Blocks", "pull_request": {"html_url": "https://github.com/pull/17"}},
				"comment": {"html_url": "https://github.com/pull/17#issuecomment-1"}
			}`,
			want: commentEvent{
				Repo: githubRepo, Sender: "belak", Action: "created", PullRequest: true,
				Number: 17, Title: "Blocks", URL: "https://github.com/pull/17#issuecomment-1",
			},
		},
		{
			name:    "issue comment on issue",
			payload: &github.IssueCommentPayload{},
			body: `{
				"action": "created", "sender": {"login": "belak"}, ` + repoJSON + `,
				"issue": {"number": 42, "title": "Crash"},
				"comment": {"html_url": "https://github.com/issues/42#issuecomment-1"}
			}`,
			want: commentEvent{
				Repo: githubRepo, Sender: "belak", Action: "created",
				Number: 42, Title: "Crash", URL: "https://github.com/issues/42#issuecomment-1",
			},
		},
		{
			name:    "review submitted",
			payload: &github.PullRequestReviewPayload{},
			body: `{
				"action": "submitted", "sender": {"login": "jsvana"}, ` + repoJSON + `,
				"review": {"state": "CHANGES_REQUESTED", "html_url": "https://github.com/pull/17#pullrequestreview-1"},
				"pull_request": {"number": 17, "title": "Blocks"}
			}`,
			want: reviewEvent{
				Repo: githubRepo, Sender: "jsvana", State: reviewChangesRequested,
				Number: 17, Title: "Blocks", URL: "https://github.com/pull/17#pullrequestreview-1",
			},
		},
		{
			name:    "review dismissed",
			payload: &github.PullRequestReviewPayload{},
			body: `{
				"action": "dismissed", "sender": {"login": "jsvana"}, ` + repoJSON + `,
				"review": {"state": "dismissed"}, "pull_request": {"number": 17, "title": "Blocks"}
			}`,
			want: reviewEvent{Repo: githubRepo, Sender: "jsvana", Number: 17, Title: "Blocks"},
		},
		{
			name:    "pull request from fork",
			payload: &github.PullRequestPayload{},
			body: `{
				"action": "opened", "sender": {"login": "jsvana"}, ` + repoJSON + `,
				"pull_request": {
					"number": 17, "title": "Blocks", "html_url": "https://github.com/pull/17",
					"head": {"ref": "blocks", "repo": {"full_name": "jsvana/seabird-core"}},
					"base": {"ref": "main", "repo": {"full_name": "seabird-chat/seabird-core"}}
				}
			}`,
			want: pullRequestEvent{
				Repo: githubRepo, Sender: "jsvana", Action: "opened",
				Number: 17, Title: "Blocks", URL: "https://github.com/pull/17",
				Head: "jsvana:blocks", Base: "main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tt.body), tt.payload); err != nil {
				t.Fatal(err)
			}
			// The webhook library hands out payloads by value.
			got := githubEvent(reflect.ValueOf(tt.payload).Elem().Interface())
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}
