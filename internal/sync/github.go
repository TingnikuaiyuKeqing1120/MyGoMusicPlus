package sync

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// GitHub is a Store over the contents API of a private repository under the
// token's account. It needs no git on the device, so it works in a browser.
type GitHub struct {
	Token  string
	Repo   string // the repository's name, under the token's account
	Client *http.Client

	owner string
}

// GitHubError preserves the API's status and reason so callers can distinguish
// a rejected sign-in from permissions, rate limits and server failures.
type GitHubError struct {
	StatusCode int
	Message    string
}

func (e *GitHubError) Error() string {
	return fmt.Sprintf("github: %s (%d)", e.Message, e.StatusCode)
}

// NewGitHub is a store for a token and a repository name.
func NewGitHub(token, repo string) *GitHub {
	return &GitHub{Token: token, Repo: repo, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (g *GitHub) ensureOwner() error {
	if g.owner != "" {
		return nil
	}
	var u struct {
		Login string `json:"login"`
	}
	st, err := g.api("GET", "/user", nil, &u)
	if err != nil {
		return err
	}
	if st != 200 {
		return fmt.Errorf("github: unexpected account response (%d)", st)
	}
	g.owner = u.Login
	return nil
}

// EnsureRepo makes the private repository if it isn't there yet.
func (g *GitHub) EnsureRepo() error {
	if err := g.ensureOwner(); err != nil {
		return err
	}
	st, err := g.api("GET", "/repos/"+g.owner+"/"+g.Repo, nil, nil, http.StatusNotFound)
	if err != nil {
		return err
	}
	if st == 200 {
		return nil
	}
	if st != 404 {
		return fmt.Errorf("github: looking up the repository (%d)", st)
	}
	st, err = g.api("POST", "/user/repos", map[string]any{"name": g.Repo, "private": true, "auto_init": false}, nil)
	if err != nil {
		return err
	}
	if st != 201 && st != 200 {
		return fmt.Errorf("github: creating the repository (%d)", st)
	}
	return nil
}

// Get returns a file's bytes, or nil if it isn't there.
func (g *GitHub) Get(path string) ([]byte, error) {
	b, _, err := g.ReadVersion(path)
	return b, err
}

func (g *GitHub) RepositoryID() string { return g.owner + "/" + g.Repo }

func (g *GitHub) ReadVersion(path string) ([]byte, string, error) {
	if err := g.ensureOwner(); err != nil {
		return nil, "", err
	}
	var f struct{ Content, Encoding, Sha string }
	st, err := g.api("GET", g.contentsPath(path), nil, &f, http.StatusNotFound)
	if err != nil {
		return nil, "", err
	}
	if st == 404 {
		return nil, "", nil
	}
	if st != 200 {
		return nil, "", fmt.Errorf("github: reading %s (%d)", path, st)
	}
	rev := f.Sha
	if f.Encoding == "none" && rev != "" {
		st, err = g.api("GET", "/repos/"+g.owner+"/"+g.Repo+"/git/blobs/"+url.PathEscape(rev), nil, &f)
		if err != nil {
			return nil, "", err
		}
		if st != 200 {
			return nil, "", fmt.Errorf("github: reading blob for %s (%d)", path, st)
		}
	}
	if f.Encoding != "base64" || rev == "" {
		return nil, "", fmt.Errorf("github: invalid contents for %s", path)
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(f.Content, "\n", ""))
	return b, rev, err
}

func (g *GitHub) Put(path string, b []byte) error {
	current, rev, err := g.ReadVersion(path)
	if err != nil {
		return err
	}
	if rev != "" && bytes.Equal(current, b) {
		return nil
	}
	return g.PutVersion(path, b, rev)
}

// PutVersion never substitutes a newer revision for the one we merged.
func (g *GitHub) PutVersion(path string, b []byte, rev string) error {
	body := map[string]any{
		"message": "sync: " + path,
		"content": base64.StdEncoding.EncodeToString(b),
	}
	if rev != "" {
		body["sha"] = rev
	}
	st, err := g.api("PUT", g.contentsPath(path), body, nil, http.StatusConflict, http.StatusUnprocessableEntity)
	if err != nil {
		return err
	}
	if st == 409 || st == 422 {
		return fmt.Errorf("%w: %s (%d)", ErrConflict, path, st)
	}
	if st != 200 && st != 201 {
		return fmt.Errorf("github: writing %s (%d)", path, st)
	}
	return nil
}

func (g *GitHub) contentsPath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "/repos/" + url.PathEscape(g.owner) + "/" + url.PathEscape(g.Repo) + "/contents/" + strings.Join(parts, "/")
}

func (g *GitHub) api(method, path string, body any, out any, allowedStatuses ...int) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "https://api.github.com"+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		if slices.Contains(allowedStatuses, resp.StatusCode) {
			return resp.StatusCode, nil
		}
		var detail struct{ Message string }
		json.Unmarshal(b, &detail)
		if detail.Message == "" {
			detail.Message = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, &GitHubError{StatusCode: resp.StatusCode, Message: detail.Message}
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, fmt.Errorf("github: %w", err)
		}
	}
	return resp.StatusCode, nil
}
