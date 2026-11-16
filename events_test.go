package seabird_webhook_receiver

import (
	"strings"
	"testing"

	"github.com/seabird-chat/seabird-go/pb"
)

// render turns a block into the markdown-style notation used in the
// expected messages: `code`, **bold**, *italics*, [label](url), and "- " list items.
func render(b *pb.Block) string {
	var sb strings.Builder
	renderTo(&sb, b)
	return strings.TrimRight(sb.String(), "\n")
}

func renderTo(sb *strings.Builder, b *pb.Block) {
	switch v := b.GetInner().(type) {
	case *pb.Block_Text:
		sb.WriteString(v.Text.Text)
	case *pb.Block_InlineCode:
		sb.WriteString("`" + v.InlineCode.Text + "`")
	case *pb.Block_Bold:
		sb.WriteString("**" + render(v.Bold.Inner) + "**")
	case *pb.Block_Italics:
		sb.WriteString("*" + render(v.Italics.Inner) + "*")
	case *pb.Block_Link:
		sb.WriteString("[" + render(v.Link.Inner) + "](" + v.Link.Url + ")")
	case *pb.Block_Container:
		for _, inner := range v.Container.Inner {
			renderTo(sb, inner)
		}
	case *pb.Block_List:
		for _, item := range v.List.Inner {
			sb.WriteString("- " + render(item) + "\n")
		}
	default:
		sb.WriteString("<unexpected block>")
	}
}

var testRepo = repo{"seabird-chat/seabird-core", "https://example.com/seabird-chat/seabird-core"}

func TestEventBlocks(t *testing.T) {
	tests := []struct {
		name  string
		event event
		want  string // empty means skipped
	}{
		{
			name:  "ping",
			event: pingEvent{Repo: testRepo, Sender: "belak"},
			want:  "**seabird-chat/seabird-core**: belak added a webhook",
		},
		{
			name:  "ping without repo",
			event: pingEvent{Sender: "belak"},
			want:  "belak added a webhook",
		},
		{
			name: "issue opened",
			event: issueEvent{
				Repo: testRepo, Sender: "belak", Action: "opened",
				Number: 42, Title: "Crash on reconnect", URL: "https://example.com/issues/42",
			},
			want: "**seabird-chat/seabird-core**: belak opened issue **#42** - [Crash on reconnect](https://example.com/issues/42)",
		},
		{
			name:  "issue labeled",
			event: issueEvent{Repo: testRepo, Sender: "belak", Action: "labeled", Number: 42},
		},
		{
			name: "pull request opened",
			event: pullRequestEvent{
				Repo: testRepo, Sender: "belak", Action: "opened",
				Number: 17, Title: "Add block support", URL: "https://example.com/pull/17",
				Head: "belak/blocks", Base: "main",
			},
			want: "**seabird-chat/seabird-core**: belak opened pull request **#17** (`belak/blocks` -> `main`) - [Add block support](https://example.com/pull/17)",
		},
		{
			name: "pull request merged",
			event: pullRequestEvent{
				Repo: testRepo, Sender: "belak", Action: "closed", Merged: true,
				Number: 17, Title: "Add block support", URL: "https://example.com/pull/17",
				Head: "jsvana:blocks", Base: "main",
			},
			want: "**seabird-chat/seabird-core**: belak merged pull request **#17** (`jsvana:blocks` -> `main`) - [Add block support](https://example.com/pull/17)",
		},
		{
			name:  "pull request synchronized",
			event: pullRequestEvent{Repo: testRepo, Sender: "belak", Action: "synchronize", Number: 17},
		},
		{
			name: "issue comment",
			event: commentEvent{
				Repo: testRepo, Sender: "belak", Action: "created",
				Number: 42, Title: "Crash on reconnect", URL: "https://example.com/issues/42#comment-1",
			},
			want: "**seabird-chat/seabird-core**: belak commented on issue **#42** - [Crash on reconnect](https://example.com/issues/42#comment-1)",
		},
		{
			name: "pull request comment",
			event: commentEvent{
				Repo: testRepo, Sender: "belak", Action: "created", PullRequest: true,
				Number: 17, Title: "Add block support", URL: "https://example.com/pull/17#comment-1",
			},
			want: "**seabird-chat/seabird-core**: belak commented on pull request **#17** - [Add block support](https://example.com/pull/17#comment-1)",
		},
		{
			name:  "comment edited",
			event: commentEvent{Repo: testRepo, Sender: "belak", Action: "edited", Number: 42},
		},
		{
			name: "review approved",
			event: reviewEvent{
				Repo: testRepo, Sender: "jsvana", State: reviewApproved,
				Number: 17, Title: "Add block support", URL: "https://example.com/pull/17#review-1",
			},
			want: "**seabird-chat/seabird-core**: jsvana approved pull request **#17** - [Add block support](https://example.com/pull/17#review-1)",
		},
		{
			name: "review changes requested",
			event: reviewEvent{
				Repo: testRepo, Sender: "jsvana", State: reviewChangesRequested,
				Number: 17, Title: "Add block support", URL: "https://example.com/pull/17#review-1",
			},
			want: "**seabird-chat/seabird-core**: jsvana requested changes on pull request **#17** - [Add block support](https://example.com/pull/17#review-1)",
		},
		{
			name: "review commented",
			event: reviewEvent{
				Repo: testRepo, Sender: "jsvana", State: reviewCommented,
				Number: 17, Title: "Add block support", URL: "https://example.com/pull/17#review-1",
			},
			want: "**seabird-chat/seabird-core**: jsvana reviewed pull request **#17** - [Add block support](https://example.com/pull/17#review-1)",
		},
		{
			name:  "review dismissed",
			event: reviewEvent{Repo: testRepo, Sender: "jsvana", State: "", Number: 17},
		},
		{
			name: "push commits",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/main",
				CompareURL: "https://example.com/compare/a...b",
				Commits: []commit{
					{ID: "a1b2c3d4e5f6", Message: "render: send blocks\n\nLonger body.", Author: "belak"},
					{ID: "e5f6a7b8c9d0", Message: "chore: release v0.2.4 (#17)", Author: "jsvana"},
				},
			},
			want: "**seabird-chat/seabird-core**: belak pushed [2 commits](https://example.com/compare/a...b) to `main`\n" +
				"- `a1b2c3d4` render: send blocks\n" +
				"- `e5f6a7b8` *jsvana* - chore: release v0.2.4 (#17)",
		},
		{
			name: "push one commit",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/master",
				CompareURL: "https://example.com/compare/a...b",
				Commits:    []commit{{ID: "a1b2c3d4e5f6", Message: "\n  fix: thing\n", Author: "Belak"}},
			},
			want: "**seabird-chat/seabird-core**: belak pushed [1 commit](https://example.com/compare/a...b) to `master`\n" +
				"- `a1b2c3d4` fix: thing",
		},
		{
			name: "push truncated by forge",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/main",
				CompareURL:   "https://example.com/compare/a...b",
				TotalCommits: 3,
				Commits:      []commit{{ID: "a1b2c3d4e5f6", Message: "fix: thing", Author: "belak"}},
			},
			want: "**seabird-chat/seabird-core**: belak pushed [3 commits](https://example.com/compare/a...b) to `main`\n" +
				"- `a1b2c3d4` fix: thing\n" +
				"- *... and 2 more*",
		},
		{
			name: "force push commits",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/main", Forced: true,
				CompareURL: "https://example.com/compare/a...b",
				Commits:    []commit{{ID: "a1b2c3d4e5f6", Message: "fix: thing", Author: "belak"}},
			},
			want: "**seabird-chat/seabird-core**: belak force pushed [1 commit](https://example.com/compare/a...b) to `main`\n" +
				"- `a1b2c3d4` fix: thing",
		},
		{
			name: "force push without commits",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/main", Forced: true,
				CompareURL: "https://example.com/compare/a...b",
			},
			want: "**seabird-chat/seabird-core**: belak force pushed [`main`](https://example.com/compare/a...b)",
		},
		{
			name:  "push without commits or compare",
			event: pushEvent{Repo: testRepo, Pusher: "belak", Ref: "refs/heads/main"},
			want:  "**seabird-chat/seabird-core**: belak pushed `main`",
		},
		{
			name: "branch deleted",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/main", Deleted: true,
				CompareURL: "https://example.com/compare/a...b",
			},
			want: "**seabird-chat/seabird-core**: belak deleted branch `main`",
		},
		{
			name: "push to other branch",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/heads/belak/blocks",
				Commits: []commit{{ID: "a1b2c3d4e5f6", Message: "fix: thing"}},
			},
		},
		{
			name: "tag pushed",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/tags/v0.3.1",
				TagURL: "https://example.com/releases/tag/v0.3.1",
			},
			want: "**seabird-chat/seabird-core**: belak pushed tag [`v0.3.1`](https://example.com/releases/tag/v0.3.1)",
		},
		{
			name: "tag deleted",
			event: pushEvent{
				Repo: testRepo, Pusher: "belak", Ref: "refs/tags/v0.3.1", Deleted: true,
				TagURL: "https://example.com/releases/tag/v0.3.1",
			},
			want: "**seabird-chat/seabird-core**: belak deleted tag `v0.3.1`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.event.block()
			if tt.want == "" {
				if b != nil {
					t.Fatalf("expected skip, got %q", render(b))
				}
				return
			}
			if b == nil {
				t.Fatal("unexpected skip")
			}
			if got := render(b); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestHeadLabel(t *testing.T) {
	tests := []struct {
		ref, head, base, want string
	}{
		{"blocks", "seabird-chat/seabird-core", "seabird-chat/seabird-core", "blocks"},
		{"blocks", "jsvana/seabird-core", "seabird-chat/seabird-core", "jsvana:blocks"},
		{"blocks", "", "seabird-chat/seabird-core", "blocks"},
	}

	for _, tt := range tests {
		if got := headLabel(tt.ref, tt.head, tt.base); got != tt.want {
			t.Errorf("headLabel(%q, %q, %q) = %q, want %q", tt.ref, tt.head, tt.base, got, tt.want)
		}
	}
}
