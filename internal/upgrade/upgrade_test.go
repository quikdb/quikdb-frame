package upgrade

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
