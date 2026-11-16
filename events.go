package seabird_webhook_receiver

import (
	"fmt"
	"slices"
	"strings"

	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
)

var mainBranches = []string{"main", "master"}

// event is a webhook event normalized across forges. block returns the
// message to send, or nil if the event should be skipped.
type event interface {
	block() *pb.Block
}

type repo struct {
	FullName string
	HTMLURL  string
}

type pingEvent struct {
	Repo   repo
	Sender string
}

type issueEvent struct {
	Repo   repo
	Sender string
	Action string
	Number int64
	Title  string
	URL    string
}

type pullRequestEvent struct {
	Repo   repo
	Sender string
	Action string
	Merged bool
	Number int64
	Title  string
	URL    string
	Head   string
	Base   string
}

type commentEvent struct {
	Repo        repo
	Sender      string
	Action      string
	PullRequest bool
	Number      int64
	Title       string
	URL         string
}

// reviewState values shared by all forges.
const (
	reviewApproved         = "approved"
	reviewChangesRequested = "changes_requested"
	reviewCommented        = "commented"
)

type reviewEvent struct {
	Repo   repo
	Sender string
	State  string
	Number int64
	Title  string
	URL    string
}

type pushEvent struct {
	Repo       repo
	Pusher     string
	Ref        string
	Deleted    bool
	Forced     bool
	CompareURL string
	TagURL     string

	// TotalCommits is the number of commits pushed, which may be more than
	// the forge included in Commits.
	TotalCommits int
	Commits      []commit
}

type commit struct {
	ID      string
	Message string
	Author  string
}

// message builds a block message one piece at a time.
type message struct {
	blocks []*pb.Block
}

// newMessage starts a message with the "owner/repo: " prefix.
func newMessage(r repo) *message {
	m := &message{}
	if r.FullName != "" {
		m.text("%s: ", r.FullName)
	}
	return m
}

func (m *message) text(format string, args ...any) {
	m.blocks = append(m.blocks, seabird.NewTextBlockf(format, args...))
}

func (m *message) code(text string) {
	m.blocks = append(m.blocks, seabird.NewInlineCodeBlock(text))
}

// link adds inner as a link to url, or as-is if there is no url.
func (m *message) link(url string, inner *pb.Block) {
	if url == "" {
		m.blocks = append(m.blocks, inner)
		return
	}
	m.blocks = append(m.blocks, seabird.NewLinkBlock(url, inner))
}

func (m *message) add(b *pb.Block) {
	m.blocks = append(m.blocks, b)
}

func (m *message) block() *pb.Block {
	return seabird.NewContainerBlock(m.blocks...)
}

func (e pingEvent) block() *pb.Block {
	m := newMessage(e.Repo)
	m.text("%s added a webhook", e.Sender)
	return m.block()
}

func (e issueEvent) block() *pb.Block {
	if !slices.Contains([]string{"opened", "closed", "reopened"}, e.Action) {
		return nil
	}

	m := newMessage(e.Repo)
	m.text("%s %s issue #%d - ", e.Sender, e.Action, e.Number)
	m.link(e.URL, seabird.NewTextBlock(e.Title))
	return m.block()
}

func (e pullRequestEvent) block() *pb.Block {
	action := e.Action
	if action == "closed" && e.Merged {
		action = "merged"
	}
	if !slices.Contains([]string{"opened", "closed", "reopened", "merged"}, action) {
		return nil
	}

	m := newMessage(e.Repo)
	m.text("%s %s pull request #%d (", e.Sender, action, e.Number)
	m.code(e.Head)
	m.text(" -> ")
	m.code(e.Base)
	m.text(") - ")
	m.link(e.URL, seabird.NewTextBlock(e.Title))
	return m.block()
}

func (e commentEvent) block() *pb.Block {
	if e.Action != "created" {
		return nil
	}

	kind := "issue"
	if e.PullRequest {
		kind = "pull request"
	}

	m := newMessage(e.Repo)
	m.text("%s commented on %s #%d - ", e.Sender, kind, e.Number)
	m.link(e.URL, seabird.NewTextBlock(e.Title))
	return m.block()
}

func (e reviewEvent) block() *pb.Block {
	var verb string
	switch e.State {
	case reviewApproved:
		verb = "approved"
	case reviewChangesRequested:
		verb = "requested changes on"
	case reviewCommented:
		verb = "reviewed"
	default:
		return nil
	}

	m := newMessage(e.Repo)
	m.text("%s %s pull request #%d - ", e.Sender, verb, e.Number)
	m.link(e.URL, seabird.NewTextBlock(e.Title))
	return m.block()
}

func (e pushEvent) block() *pb.Block {
	verb := "pushed"
	if e.Forced {
		verb = "force pushed"
	}

	m := newMessage(e.Repo)

	if tag, ok := strings.CutPrefix(e.Ref, "refs/tags/"); ok {
		if e.Deleted {
			m.text("%s deleted tag ", e.Pusher)
			m.code(tag)
		} else {
			m.text("%s %s tag ", e.Pusher, verb)
			m.link(e.TagURL, seabird.NewInlineCodeBlock(tag))
		}
		return m.block()
	}

	branch, ok := strings.CutPrefix(e.Ref, "refs/heads/")
	if !ok || !slices.Contains(mainBranches, branch) {
		return nil
	}

	if e.Deleted {
		m.text("%s deleted branch ", e.Pusher)
		m.code(branch)
		return m.block()
	}

	total := max(e.TotalCommits, len(e.Commits))
	if total == 0 {
		m.text("%s %s ", e.Pusher, verb)
		m.link(e.CompareURL, seabird.NewInlineCodeBlock(branch))
		return m.block()
	}

	m.text("%s %s ", e.Pusher, verb)
	m.link(e.CompareURL, seabird.NewTextBlock(pluralize(total, "commit")))
	m.text(" to ")
	m.code(branch)

	// The newline keeps the commits off the header line in core's plain
	// text version of the message.
	m.text("\n")

	items := make([]*pb.Block, 0, len(e.Commits)+1)
	for _, c := range e.Commits {
		items = append(items, c.block(e.Pusher))
	}
	if more := total - len(e.Commits); more > 0 {
		items = append(items, seabird.NewTextBlockf("... and %d more", more))
	}
	m.add(seabird.NewListBlock(items...))

	return m.block()
}

// block renders a commit list item. The author is only shown when it
// differs from the pusher.
func (c commit) block(pusher string) *pb.Block {
	text := " " + subject(c.Message)
	if c.Author != "" && !strings.EqualFold(c.Author, pusher) {
		text = " " + c.Author + " -" + text
	}

	return seabird.NewContainerBlock(
		seabird.NewInlineCodeBlock(shortSHA(c.ID)),
		seabird.NewTextBlock(text),
	)
}

// subject returns the first non-empty line of a commit message.
func subject(msg string) string {
	for line := range strings.SplitSeq(msg, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func shortSHA(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// headLabel names the head branch of a pull request, adding the owner when
// it comes from a fork.
func headLabel(ref, headRepo, baseRepo string) string {
	if headRepo == "" || headRepo == baseRepo {
		return ref
	}
	owner, _, _ := strings.Cut(headRepo, "/")
	return owner + ":" + ref
}
