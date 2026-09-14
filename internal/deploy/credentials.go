package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gofrs/flock"
	"github.com/zalando/go-keyring"
)

type credentialStore interface {
	Get(string, string) (string, error)
	Set(string, string, string) error
	Delete(string, string) error
}
type osCredentialStore struct{}

func (osCredentialStore) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}
func (osCredentialStore) Set(service, account, value string) error {
	return keyring.Set(service, account, value)
}
func (osCredentialStore) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

var credentials credentialStore = osCredentialStore{}
var credentialDir = defaultCredentialDir

func defaultCredentialDir() string {
	if dir := os.Getenv("QUIKDB_FRAME_CONFIG_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			return ""
		}
		return filepath.Clean(dir)
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, configDir)
}

const credentialService = "quikdb-frame"
const credentialAccount = "compute-production"

func authConfigPath() string { return filepath.Join(credentialDir(), configFile) }

func secureCredentialDir() error {
	dir := credentialDir()
	if dir == "" {
		return fmt.Errorf("could not locate user credential directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("credential directory must not be a symbolic link")
	}
	return os.Chmod(dir, 0700)
}

func LoadAuth() (*AuthConfig, error) {
	value, err := credentials.Get(credentialService, credentialAccount)
	if err != nil {
		// Read the existing permission-restricted fallback, including pre-upgrade credentials.
		if credentialDir() == "" {
			return nil, fmt.Errorf("could not locate user credential directory")
		}
		dirInfo, dirErr := os.Lstat(credentialDir())
		if dirErr != nil {
			return nil, dirErr
		}
		if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && dirInfo.Mode().Perm()&0077 != 0) {
			return nil, fmt.Errorf("credential directory must be private and must not be a symbolic link")
		}
		path := authConfigPath()
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
			return nil, fmt.Errorf("credential file must be a private regular file")
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		value = string(data)
	}
	var auth AuthConfig
	if err := json.Unmarshal([]byte(value), &auth); err != nil {
		return nil, err
	}
	return &auth, nil
}

func SaveAuth(auth *AuthConfig) error {
	if credentialDir() == "" {
		return fmt.Errorf("credential directory must be absolute and available")
	}
	if auth == nil || auth.Token == "" {
		return fmt.Errorf("cannot store empty credentials")
	}
	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	if err := credentials.Set(credentialService, credentialAccount, string(data)); err == nil {
		// Remove legacy copies only after secure storage succeeded.
		if err := os.Remove(authConfigPath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("secure credentials saved, but could not remove legacy copy: %w", err)
		}
		return nil
	} else if runtime.GOOS != "linux" {
		return fmt.Errorf("OS credential storage unavailable: %w", err)
	}
	// Headless Linux fallback; no fallback to plaintext on desktop platforms.
	if err := secureCredentialDir(); err != nil {
		return err
	}
	file, err := os.CreateTemp(credentialDir(), ".auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), authConfigPath()); err != nil {
		return err
	}
	return nil
}

func DeleteAuth() error {
	if credentialDir() == "" {
		return fmt.Errorf("credential directory must be absolute and available")
	}
	keyringErr := credentials.Delete(credentialService, credentialAccount)
	fileErr := os.Remove(authConfigPath())
	if errors.Is(keyringErr, keyring.ErrNotFound) {
		keyringErr = nil
	}
	// A headless Linux fallback can exist without an available keyring.
	if runtime.GOOS == "linux" && fileErr == nil {
		keyringErr = nil
	}
	if os.IsNotExist(fileErr) {
		fileErr = nil
	}
	return errors.Join(keyringErr, fileErr)
}

func refreshAuth() (string, error) {
	if err := secureCredentialDir(); err != nil {
		return "", err
	}
	lock := flock.New(filepath.Join(credentialDir(), "session.lock"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil || !locked {
		return "", fmt.Errorf("another CLI command is refreshing credentials; retry shortly")
	}
	defer func() { lock.Unlock(); lock.Close() }()
	// Re-read under the cross-process lock to avoid replaying another command's old token.
	auth, err := LoadAuth()
	if err != nil {
		return "", err
	}
	if tokenCurrent(auth) {
		return auth.Token, nil
	}
	var tokens tokenResponse
	if err := authClientFactory().request(context.Background(), "POST", "refresh", "", map[string]string{"refreshToken": auth.RefreshToken}, &tokens); err != nil {
		return "", fmt.Errorf("refresh failed; run quikdb-frame login: %w", err)
	}
	if err := saveTokens(tokens); err != nil {
		return "", err
	}
	return tokens.AccessToken, nil
}
