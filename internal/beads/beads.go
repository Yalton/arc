// Package beads wraps the bd CLI (SPEC.md, "Beads integration details").
// arc reaches beads only by running bd; it never opens the database.
// Beads is optional: Open reports a notice instead of failing when bd or
// a workspace is missing (SPEC.md, Principle 4).
package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// DefaultTimeout bounds every bd invocation.
const DefaultTimeout = 15 * time.Second

// Client runs bd in a repository. Construct it with Open, or set the fields
// directly in tests (Bin may point at a fake bd).
type Client struct {
	Bin     string        // bd executable
	Root    string        // repository root; bd runs with it as working directory
	Timeout time.Duration // per command; DefaultTimeout when zero
}

// Bead is the part of 'bd show --json' / 'bd list --json' arc reads
// (pinned against bd 1.3.0; absent text fields are omitted by bd).
type Bead struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Design             string   `json:"design"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Notes              string   `json:"notes"`
	Status             string   `json:"status"`
	Labels             []string `json:"labels"`
	CreatedAt          string   `json:"created_at"` // RFC 3339
}

// Created parses CreatedAt; ok is false when it is missing or malformed.
func (b Bead) Created() (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, b.CreatedAt)
	return t, err == nil
}

// HasLabel reports whether the bead carries label.
func (b Bead) HasLabel(label string) bool {
	for _, l := range b.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// HasLabelPrefix reports whether any label starts with prefix.
func (b Bead) HasLabelPrefix(prefix string) bool {
	for _, l := range b.Labels {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// Open returns a client for the beads workspace of root. When bd is not on
// PATH or 'bd where' fails there, it returns nil and a notice saying bead
// features are skipped.
func Open(root string) (*Client, string) {
	bin, err := exec.LookPath("bd")
	if err != nil {
		return nil, "beads: bd not found on PATH; bead features skipped"
	}
	c := &Client{Bin: bin, Root: root}
	if _, err := c.run(nil, "where"); err != nil {
		return nil, "beads: no beads workspace for " + root + " ('bd where' failed); bead features skipped"
	}
	return c, ""
}

// validID guards against ids that bd would parse as flags.
var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func checkID(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("invalid bead id %q", id)
	}
	return nil
}

// Show returns one bead ('bd show <id> --json', which prints an array).
func (c *Client) Show(id string) (Bead, error) {
	if err := checkID(id); err != nil {
		return Bead{}, err
	}
	out, err := c.run(nil, "show", id, "--json")
	if err != nil {
		return Bead{}, err
	}
	var bs []Bead
	if err := json.Unmarshal(out, &bs); err != nil {
		return Bead{}, fmt.Errorf("bd show %s: parsing output: %v", id, err)
	}
	if len(bs) == 0 || bs[0].ID == "" {
		return Bead{}, fmt.Errorf("bd show %s: bead not found", id)
	}
	return bs[0], nil
}

// List returns beads with the given status ('bd list --status=<s> --json'),
// leaving out those carrying any label that starts with withoutLabelPrefix
// (no filter when it is empty).
func (c *Client) List(status, withoutLabelPrefix string) ([]Bead, error) {
	out, err := c.run(nil, "list", "--status="+status, "--limit=0", "--json")
	if err != nil {
		return nil, err
	}
	var bs []Bead
	if err := json.Unmarshal(out, &bs); err != nil {
		return nil, fmt.Errorf("bd list: parsing output: %v", err)
	}
	if withoutLabelPrefix == "" {
		return bs, nil
	}
	var kept []Bead
	for _, b := range bs {
		if !b.HasLabelPrefix(withoutLabelPrefix) {
			kept = append(kept, b)
		}
	}
	return kept, nil
}

// AddLabel adds label to the bead ('bd label add <id> <label>').
func (c *Client) AddLabel(id, label string) error {
	if err := checkID(id); err != nil {
		return err
	}
	_, err := c.run(nil, "label", "add", id, label)
	return err
}

// Note appends text to the bead's notes ('bd note <id> --stdin').
func (c *Client) Note(id, text string) error {
	if err := checkID(id); err != nil {
		return err
	}
	_, err := c.run(strings.NewReader(text), "note", id, "--stdin")
	return err
}

// Create files a bead and returns its id
// ('bd create --title=<t> --stdin [--labels=a,b] --json').
func (c *Client) Create(title, description string, labels []string) (string, error) {
	args := []string{"create", "--title=" + title, "--stdin", "--json"}
	if len(labels) > 0 {
		args = append(args, "--labels="+strings.Join(labels, ","))
	}
	if description == "" {
		args = append(args, "--allow-empty-description")
	}
	out, err := c.run(strings.NewReader(description), args...)
	if err != nil {
		return "", err
	}
	var b Bead
	if err := json.Unmarshal(out, &b); err != nil || b.ID == "" {
		return "", fmt.Errorf("bd create: no id in output %q", strings.TrimSpace(string(out)))
	}
	return b.ID, nil
}

// run executes bd with args in Root and returns stdout. Errors carry bd's
// stderr.
func (c *Client) run(stdin *strings.Reader, args ...string) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Dir = c.Root
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("bd %s: timed out after %s", args[0], timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("bd %s: %v: %s", args[0], err, msg)
	}
	return stdout.Bytes(), nil
}
