package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

type fixtureStore struct {
	mu          sync.Mutex
	value       string
	unavailable bool
}

func (s *fixtureStore) Get(_, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable {
		return "", fmt.Errorf("fixture keyring unavailable")
	}
	if s.value == "" {
		return "", keyring.ErrNotFound
	}
	return s.value, nil
}
func (s *fixtureStore) Set(_, _, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable {
		return fmt.Errorf("fixture keyring unavailable")
	}
	s.value = value
	return nil
}
func (s *fixtureStore) Delete(_, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable {
		return fmt.Errorf("fixture keyring unavailable")
	}
	s.value = ""
	return nil
}

func credentialFixture(t *testing.T) *fixtureStore {
	t.Helper()
	store := &fixtureStore{}
	oldStore, oldDir, oldFactory := credentials, credentialDir, authClientFactory
	dir := t.TempDir()
	credentials = store
	credentialDir = func() string { return dir }
	t.Cleanup(func() { credentials, credentialDir, authClientFactory = oldStore, oldDir, oldFactory })
	return store
}

func authServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	authClientFactory = func() *authClient { return &authClient{server.URL, &http.Client{Timeout: time.Second}} }
}

func TestCallbackRejectsStateInjectionAndDuplicates(t *testing.T) {
	state := strings.Repeat("s", 43)
	code := strings.Repeat("c", 43)
	codes := make(chan string, 1)
	handler := callbackHandler(state, "127.0.0.1:41234", codes)
	for _, target := range []string{
		"http://127.0.0.1:41234/callback?state=wrong&code=" + code,
		"http://127.0.0.1:41234/callback?state=" + state + "&token=raw-token",
		"http://127.0.0.1:41234/callback?state=" + state + "&code=" + code + "&code=second",
		"http://attacker.example/callback?state=" + state + "&code=" + code,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
		if response.Code != 400 || len(codes) != 0 {
			t.Fatalf("callback accepted %s", target)
		}
	}
	target := "http://127.0.0.1:41234/callback?state=" + state + "&code=" + code
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
	if response.Code != 200 || len(codes) != 1 {
		t.Fatal("valid callback rejected")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
	if response.Code != 409 {
		t.Fatal("duplicate callback did not fail without blocking")
	}
}

func TestPKCEKnownVector(t *testing.T) {
	got := pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("PKCE challenge %s", got)
	}
}

func TestVerifiedTokenLoginRejectsBadCredentials(t *testing.T) {
	store := credentialFixture(t)
	authServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":"Invalid token"}`)
	})
	if err := LoginWithToken("invalid"); err == nil {
		t.Fatal("saved unverified token")
	}
	if store.value != "" {
		t.Fatal("invalid credentials persisted")
	}
}

func TestCredentialsUseKeyringAndRemoveLegacyCopy(t *testing.T) {
	credentialFixture(t)
	if err := os.WriteFile(authConfigPath(), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveAuth(&AuthConfig{Token: "fixture-access", RefreshToken: "fixture-refresh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(authConfigPath()); !os.IsNotExist(err) {
		t.Fatal("legacy bearer copy remains")
	}
	auth, err := LoadAuth()
	if err != nil || auth.RefreshToken != "fixture-refresh" {
		t.Fatalf("load %+v: %v", auth, err)
	}
}

func TestHeadlessFallbackIsPrivateAndRejectsSymlinks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("permission-restricted fallback is Linux-only")
	}
	store := credentialFixture(t)
	store.unavailable = true
	if err := SaveAuth(&AuthConfig{Token: "fixture-access"}); err != nil {
		t.Fatal(err)
	}
	file, _ := os.Stat(authConfigPath())
	dir, _ := os.Stat(credentialDir())
	if file.Mode().Perm() != 0600 || dir.Mode().Perm() != 0700 {
		t.Fatal("unsafe fallback permissions")
	}
	if err := os.Chmod(authConfigPath(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAuth(); err == nil {
		t.Fatal("accepted readable credential file")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	credentialDir = func() string { return link }
	if err := SaveAuth(&AuthConfig{Token: "fixture-access"}); err == nil {
		t.Fatal("accepted symlink credential directory")
	}
}

func TestConcurrentCommandsRefreshOnlyOnce(t *testing.T) {
	credentialFixture(t)
	if err := SaveAuth(&AuthConfig{Token: "old-access", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	authServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/cli/refresh" {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		calls.Add(1)
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["refreshToken"] != "old-refresh" {
			t.Error("incorrect refresh secret")
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": tokenResponse{
			AccessToken: "new-access", RefreshToken: "new-refresh", WalletAddress: "fixture-wallet", ExpiresIn: 900,
			AccessExpiresAt: time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		}})
	})
	var group sync.WaitGroup
	for i := 0; i < 5; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			token, err := RequireAuth()
			if err != nil || token != "new-access" {
				t.Errorf("refresh %q: %v", token, err)
			}
		}()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls %d; old secret replayed", calls.Load())
	}
}

func TestExpiredManualTokenIsNotAccepted(t *testing.T) {
	credentialFixture(t)
	if err := SaveAuth(&AuthConfig{Token: "expired-access", ExpiresAt: time.Now().Add(-time.Minute).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireAuth(); err == nil {
		t.Fatal("accepted expired saved token")
	}
}

func TestAuthAPIErrorCannotBecomeSuccess(t *testing.T) {
	credentialFixture(t)
	for _, body := range []string{`{"success":false}`, `{"success":true,"data":null}`, `not-json`} {
		authServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if err := authClientFactory().request(context.Background(), "GET", "me", "fixture", nil, &authProfile{}); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestNativeCredentialStoreRoundTrip(t *testing.T) {
	if os.Getenv("FRAME_KEYRING_TEST") != "1" {
		t.Skip("native credential integration runs on isolated desktop CI jobs")
	}
	account, err := randomSecret()
	if err != nil {
		t.Fatal(err)
	}
	store := osCredentialStore{}
	const service = "quikdb-frame-validation"
	if err := store.Set(service, account, "fixture-not-a-user-token"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Delete(service, account) })
	value, err := store.Get(service, account)
	if err != nil || value != "fixture-not-a-user-token" {
		t.Fatalf("native storage round trip failed: %v", err)
	}
	if err := store.Delete(service, account); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(service, account); err == nil {
		t.Fatal("native credential remains after deletion")
	}
}
