package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/quikdb/quikdb-frame/releases/latest"

var httpClient = &http.Client{Timeout: 2 * time.Minute}

func Run() error {
	binaryName, err := binaryForPlatform()
	if err != nil {
		return err
	}
	// Resolve the release once so the binary and checksums cannot cross a latest-tag update.
	tag, err := latestTag()
	if err != nil {
		return err
	}
	base := "https://github.com/quikdb/quikdb-frame/releases/download/" + tag
	checksums, err := fetchSmall(base + "/SHA256SUMS")
	if err != nil {
		return fmt.Errorf("could not fetch release checksums: %w", err)
	}
	expected, err := checksumFor(checksums, binaryName)
	if err != nil {
		return err
	}
	url := base + "/" + binaryName

	fmt.Printf("Downloading latest quikdb-frame for %s/%s...\n", runtime.GOOS, runtime.GOARCH)

	// Get current binary path
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find current binary: %w", err)
	}

	// Download to a temp file next to the current binary
	tmp := self + ".new"
	if err := downloadVerified(url, tmp, expected); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmp, 0755); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not make binary executable: %w", err)
	}

	// Replace current binary.
	// On Windows, a running executable cannot be overwritten directly.
	// Move the old binary aside first, then move the new one into place.
	old := self + ".old"
	os.Remove(old) // clean up any previous failed upgrade
	if err := os.Rename(self, old); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not move current binary (try running as administrator): %w", err)
	}
	if err := os.Rename(tmp, self); err != nil {
		// Restore old binary before giving up
		os.Rename(old, self)
		os.Remove(tmp)
		return fmt.Errorf("could not install new binary: %w", err)
	}
	os.Remove(old)

	fmt.Println("Upgraded successfully. Run 'quikdb-frame version' to confirm.")
	return nil
}

func fetchSmall(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		return nil, err
	}
	if len(body) > 65536 {
		return nil, fmt.Errorf("release metadata exceeds size limit")
	}
	return body, nil
}

func latestTag() (string, error) {
	body, err := fetchSmall(releasesAPI)
	if err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(release.Tag) {
		return "", fmt.Errorf("invalid stable release tag %q", release.Tag)
	}
	return release.Tag, nil
}

func checksumFor(body []byte, binaryName string) (string, error) {
	checksum := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != binaryName {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size || checksum != "" {
			return "", fmt.Errorf("invalid or duplicate release checksum for %s", binaryName)
		}
		checksum = strings.ToLower(fields[0])
	}
	if checksum == "" {
		return "", fmt.Errorf("release has no checksum for %s; current binary retained", binaryName)
	}
	return checksum, nil
}

func downloadVerified(url, dest, expected string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	verified := false
	defer func() {
		if !verified {
			os.Remove(dest)
		}
	}()
	hash := sha256.New()
	const maxSize = 64 << 20
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, maxSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n == 0 || n > maxSize {
		return fmt.Errorf("release binary has invalid size")
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("release checksum mismatch; current binary retained")
	}
	verified = true
	return nil
}

func binaryForPlatform() (string, error) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH != "amd64" {
			return "", fmt.Errorf("Windows ARM64 binary is not published")
		}
		return "quikdb-frame-windows-amd64.exe", nil
	case "darwin", "linux":
		return "quikdb-frame-" + runtime.GOOS + "-" + runtime.GOARCH, nil
	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}
