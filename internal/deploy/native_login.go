package deploy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"time"
)

type loginGrant struct {
	GrantID          string `json:"grantId"`
	AuthorizationURL string `json:"authorizationUrl"`
	DeviceCode       string `json:"deviceCode"`
	UserCode         string `json:"userCode"`
	Interval         int    `json:"interval"`
}

func randomSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
func pkceChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func callbackHandler(state, host string, codes chan<- string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		query := r.URL.Query()
		if r.Method != http.MethodGet || r.Host != host || r.URL.Path != "/callback" ||
			len(query["state"]) != 1 || len(query["code"]) != 1 || query.Has("token") ||
			subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 ||
			!regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(query.Get("code")) {
			http.Error(w, "Invalid login callback. Return to QuikDB and try again.", http.StatusBadRequest)
			return
		}
		select {
		case codes <- query.Get("code"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<html><body><h2>Authorization received</h2><p>Return to your terminal to confirm login.</p></body></html>")
		default:
			http.Error(w, "Authorization already received.", http.StatusConflict)
		}
	})
}

func startGrant(ctx context.Context, payload interface{}) (*loginGrant, error) {
	var grant loginGrant
	if err := authClientFactory().request(ctx, "POST", "start", "", payload, &grant); err != nil {
		return nil, err
	}
	u, err := url.Parse(grant.AuthorizationURL)
	if err != nil || u.Scheme != "https" || u.Host != "compute.quikdb.com" || u.Path != "/cli-auth" || grant.GrantID == "" {
		return nil, fmt.Errorf("authentication server returned an invalid authorization link")
	}
	return &grant, nil
}

func Login() error {
	if token, err := RequireAuth(); err == nil {
		if err := authClientFactory().request(context.Background(), "GET", "me", token, nil, &authProfile{}); err == nil {
			fmt.Println("Already logged in with a valid account. Run logout to switch accounts.")
			return nil
		}
	}
	verifier, err := randomSecret()
	if err != nil {
		return err
	}
	state, err := randomSecret()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start loopback login listener: %w; use login --device for a remote terminal", err)
	}
	defer listener.Close()
	host := listener.Addr().String()
	codes := make(chan string, 1)
	server := &http.Server{Handler: callbackHandler(state, host, codes), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 4096}
	go server.Serve(listener)
	defer server.Close()
	interruptCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(interruptCtx, 5*time.Minute)
	defer cancel()
	grant, err := startGrant(ctx, map[string]interface{}{"mode": "browser", "scopes": []string{"compute:deployments", "compute:environment", "compute:domains", "compute:databases"}, "challenge": pkceChallenge(verifier), "state": state, "callback": "http://" + host + "/callback"})
	if err != nil {
		return err
	}
	fmt.Printf("Authorize this terminal in your browser:\n%s\n", grant.AuthorizationURL)
	openBrowser(grant.AuthorizationURL)
	select {
	case code := <-codes:
		var tokens tokenResponse
		if err := authClientFactory().request(ctx, "POST", "token", "", map[string]string{"grantId": grant.GrantID, "code": code, "codeVerifier": verifier}, &tokens); err != nil {
			return err
		}
		if err := saveTokens(tokens); err != nil {
			return err
		}
		fmt.Println("Logged in successfully.")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("login cancelled or expired: %w", ctx.Err())
	}
}

func LoginDevice() error {
	verifier, err := randomSecret()
	if err != nil {
		return err
	}
	interruptCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(interruptCtx, 5*time.Minute)
	defer cancel()
	grant, err := startGrant(ctx, map[string]interface{}{"mode": "device", "scopes": []string{"compute:deployments", "compute:environment", "compute:domains", "compute:databases"}, "challenge": pkceChallenge(verifier)})
	if err != nil {
		return err
	}
	if grant.DeviceCode == "" || grant.UserCode == "" || grant.Interval < 5 {
		return fmt.Errorf("authentication response missing device authorization data")
	}
	fmt.Printf("Open %s on a device with a browser.\nEnter terminal code: %s\nApprove only the terminal you started.\n", grant.AuthorizationURL, grant.UserCode)
	interval := time.Duration(grant.Interval) * time.Second
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("device login cancelled or expired: %w", ctx.Err())
		case <-timer.C:
		}
		var tokens tokenResponse
		err := authClientFactory().request(ctx, "POST", "token", "", map[string]string{"deviceCode": grant.DeviceCode, "codeVerifier": verifier}, &tokens)
		if err == nil {
			if err := saveTokens(tokens); err != nil {
				return err
			}
			fmt.Println("Logged in successfully.")
			return nil
		}
		var apiErr *authAPIError
		if !errors.As(err, &apiErr) {
			return err
		}
		switch apiErr.Code {
		case "authorization_pending":
		case "slow_down":
			if interval < 30*time.Second {
				interval += 5 * time.Second
			}
		default:
			return err
		}
	}
}

func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "linux":
		command = exec.Command("xdg-open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if command != nil {
		command.Start()
	}
}
