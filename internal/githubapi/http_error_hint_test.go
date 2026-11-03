package githubapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnonymous404NamesTheToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer server.Close()
	for _, token := range []string{"", "secret"} {
		client, err := NewClient(server.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		u, err := client.endpoint("o/r", 1, false)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = client.request(context.Background(), http.MethodGet, u, nil, http.StatusOK)
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != 404 {
			t.Fatalf("token %q: err = %v, want an HTTP 404", token, err)
		}
		hinted := strings.Contains(err.Error(), "HN_GITHUB_TOKEN")
		if hinted != (token == "") {
			t.Fatalf("token %q: error %q, hint present %v", token, err, hinted)
		}
	}
	if (&HTTPError{StatusCode: 500, Anonymous: true}).Error() != "GitHub API returned HTTP 500" {
		t.Fatal("a non-404 error gained the token hint")
	}
}
