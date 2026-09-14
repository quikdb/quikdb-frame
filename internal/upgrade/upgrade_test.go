package upgrade

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLatestReleaseRedirect(t *testing.T) {
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	for _, tc := range []struct {
		name, location string
		status         int
		wantError      bool
	}{
		{"absolute", "https://github.com/quikdb/quikdb-frame/releases/tag/v0.1.12", 302, false},
		{"relative", "/quikdb/quikdb-frame/releases/tag/v0.1.12", 302, false},
		{"other-host", "https://example.com/quikdb/quikdb-frame/releases/tag/v0.1.12", 302, true},
		{"other-repo", "https://github.com/other/repo/releases/tag/v0.1.12", 302, true},
		{"http", "http://github.com/quikdb/quikdb-frame/releases/tag/v0.1.12", 302, true},
		{"credentials", "https://user@github.com/quikdb/quikdb-frame/releases/tag/v0.1.12", 302, true},
		{"query", "/quikdb/quikdb-frame/releases/tag/v0.1.12?download=1", 302, true},
		{"fragment", "/quikdb/quikdb-frame/releases/tag/v0.1.12#asset", 302, true},
		{"prerelease", "/quikdb/quikdb-frame/releases/tag/v0.1.12-rc1", 302, true},
		{"encoded-path", "/quikdb/quikdb-frame/releases/tag/%760.1.12", 302, true},
		{"missing-location", "", 302, true},
		{"quota", "", 403, true},
		{"no-redirect", "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			httpClient = &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Method != http.MethodHead || r.URL.String() != latestReleaseURL {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{tc.location}}, Body: http.NoBody, Request: r}, nil
			})}
			got, err := latestTag()
			if (err != nil) != tc.wantError || (!tc.wantError && got != "v0.1.12") || requests != 1 {
				t.Fatalf("tag %q, error %v, requests %d", got, err, requests)
			}
		})
	}
}

func TestReleaseRedirectRejectsEmptyQuery(t *testing.T) {
	u, err := url.Parse("https://github.com/quikdb/quikdb-frame/releases/tag/v0.1.12?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releaseTagFromURL(u); err == nil {
		t.Fatal("accepted an unexpected empty query")
	}
}

func TestChecksumManifest(t *testing.T) {
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))
	for _, tc := range []struct {
		body      string
		wantError bool
	}{
		{checksum + "  quikdb-frame-linux-amd64\n", false},
		{checksum + " *quikdb-frame-linux-amd64\n", false},
		{checksum + "  another-binary\n", true},
		{"not-a-digest  quikdb-frame-linux-amd64\n", true},
		{strings.Repeat(checksum+"  quikdb-frame-linux-amd64\n", 2), true},
	} {
		got, err := checksumFor([]byte(tc.body), "quikdb-frame-linux-amd64")
		if (err != nil) != tc.wantError || (!tc.wantError && got != checksum) {
			t.Fatalf("got %q, err %v", got, err)
		}
	}
}

func TestDownloadVerification(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		wantError  bool
	}{
		{"valid", "fixture-binary", 200, false},
		{"tampered", "tampered-binary", 200, true},
		{"empty", "", 200, true},
		{"missing", "not-found", 404, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "candidate")
			checksum := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture-binary")))
			err := downloadVerified(server.URL, path, checksum)
			if (err != nil) != tc.wantError {
				t.Fatalf("verification error: %v", err)
			}
			if !tc.wantError {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != tc.body {
					t.Fatalf("downloaded %q, %v", body, err)
				}
			}
		})
	}
}

func TestDownloadCannotOverwriteExistingCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "new-binary") }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "candidate")
	if err := os.WriteFile(path, []byte("existing-binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := downloadVerified(server.URL, path, "unused"); err == nil {
		t.Fatal("overwrote candidate")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "existing-binary" {
		t.Fatalf("existing binary changed: %q, %v", body, err)
	}
}
