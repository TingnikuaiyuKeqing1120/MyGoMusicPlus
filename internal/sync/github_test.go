package sync

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type githubTransport struct{ handler http.Handler }

func (tr githubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	tr.handler.ServeHTTP(w, req)
	return w.Result(), nil
}

// Account failures keep GitHub's reason and status instead of calling every
// failure a bad token or hiding an HTTP error behind a JSON decoding error.
func TestGitHubAccountErrors(t *testing.T) {
	for _, test := range []struct {
		status  int
		body    string
		message string
	}{
		{401, `{"message":"Bad credentials"}`, "Bad credentials"},
		{403, `{"message":"API rate limit exceeded"}`, "API rate limit exceeded"},
		{429, `{"message":"Too many requests"}`, "Too many requests"},
		{502, "<html>upstream failed</html>", "Bad Gateway"},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			calls := 0
			g := NewGitHub("fixture", "sync")
			g.Client.Transport = githubTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/user" {
					t.Errorf("request continued past authentication: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})}
			var apiErr *GitHubError
			if err := g.EnsureRepo(); !errors.As(err, &apiErr) || apiErr.StatusCode != test.status || apiErr.Message != test.message {
				t.Fatalf("lost API failure: %v", err)
			}
			if calls != 1 || g.owner != "" {
				t.Fatalf("calls=%d owner=%q", calls, g.owner)
			}
		})
	}
}

// Error reporting still permits repository creation and missing files, and
// exposes concurrent writes as conflicts so the merge engine can retry.
func TestGitHubExpectedStatuses(t *testing.T) {
	created := false
	writeStatus := http.StatusConflict
	g := NewGitHub("fixture", "sync")
	g.Client.Transport = githubTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"fixture"}`)
		case r.Method == "GET":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case r.Method == "POST" && r.URL.Path == "/user/repos":
			created = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == "PUT":
			w.WriteHeader(writeStatus)
			fmt.Fprint(w, `{"message":"write rejected"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	})}
	if err := g.EnsureRepo(); err != nil || !created {
		t.Fatalf("create repository: %v, created=%v", err, created)
	}
	if data, rev, err := g.ReadVersion("missing.json"); err != nil || data != nil || rev != "" {
		t.Fatalf("missing file: %q %q %v", data, rev, err)
	}
	for _, status := range []int{http.StatusConflict, http.StatusUnprocessableEntity} {
		writeStatus = status
		if err := g.PutVersion("file.json", []byte(`{}`), "previous"); !errors.Is(err, ErrConflict) {
			t.Fatalf("status %d lost conflict: %v", status, err)
		}
	}
	writeStatus = http.StatusForbidden
	var apiErr *GitHubError
	if err := g.PutVersion("file.json", []byte(`{}`), "previous"); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("lost write failure: %v", err)
	}
}
