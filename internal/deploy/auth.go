package deploy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	apiBase      = "https://device.quikdb.net"
	computeBase  = "https://compute.quikdb.com"
	configDir    = ".quikdb-frame"
	configFile   = "auth.json"
)

type AuthConfig struct {
	Token     string `json:"token"`
	Email     string `json:"email,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// Login opens the browser for QuikDB auth and captures the token via local callback.
func Login() error {
	existing, _ := LoadAuth()
	if existing != nil && existing.Token != "" {
		fmt.Println("Already logged in.")
		fmt.Println("Run 'quikdb-frame logout' to switch accounts.")
		return nil
	}

	// Start a local HTTP server to receive the auth callback
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("could not start local auth server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			w.WriteHeader(400)
			fmt.Fprint(w, "No token received. Please try again.")
			errCh <- fmt.Errorf("no token in callback")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0">
			<div style="text-align:center">
				<h2>Logged in to QuikDB</h2>
				<p>You can close this window and return to your terminal.</p>
			</div>
		</body></html>`)
		tokenCh <- token
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	// Open browser to QuikDB Compute login with callback
	loginURL := fmt.Sprintf("%s/cli-auth?callback=%s", computeBase, callbackURL)
	fmt.Printf("Opening browser to log in...\n")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", loginURL)
	openBrowser(loginURL)

	fmt.Println("Waiting for authentication...")

	// Wait for token or timeout
	select {
	case token := <-tokenCh:
		auth := &AuthConfig{
			Token:     token,
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
		}
		if err := SaveAuth(auth); err != nil {
			return fmt.Errorf("failed to save auth: %w", err)
		}
		fmt.Println("Logged in successfully.")
		return nil

	case err := <-errCh:
		return err

	case <-time.After(2 * time.Minute):
		return fmt.Errorf("login timed out after 2 minutes")
	}
}

// LoginWithToken saves a manually provided API token.
func LoginWithToken(token string) error {
	auth := &AuthConfig{
		Token:     token,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
	}
	if err := SaveAuth(auth); err != nil {
		return fmt.Errorf("failed to save auth: %w", err)
	}
	fmt.Println("Token saved. You are now logged in.")
	return nil
}

func Logout() error {
	configPath := authConfigPath()
	os.Remove(configPath)
	fmt.Println("Logged out.")
	return nil
}

func LoadAuth() (*AuthConfig, error) {
	data, err := os.ReadFile(authConfigPath())
	if err != nil {
		return nil, err
	}
	var auth AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}
	return &auth, nil
}

func SaveAuth(auth *AuthConfig) error {
	dir := filepath.Join(homeDir(), configDir)
	os.MkdirAll(dir, 0700)
	data, _ := json.MarshalIndent(auth, "", "  ")
	return os.WriteFile(filepath.Join(dir, configFile), data, 0600)
}

func RequireAuth() (string, error) {
	auth, err := LoadAuth()
	if err != nil || auth.Token == "" {
		return "", fmt.Errorf("not logged in. Run: quikdb-frame login")
	}
	return auth.Token, nil
}

func authConfigPath() string {
	return filepath.Join(homeDir(), configDir, configFile)
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}
