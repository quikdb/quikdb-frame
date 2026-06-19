package upgrade

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
)

const releaseBase = "https://github.com/quikdb/quikdb-frame/releases/latest/download"

func Run() error {
	binaryName := binaryForPlatform()
	url := fmt.Sprintf("%s/%s", releaseBase, binaryName)

	fmt.Printf("Downloading latest quikdb-frame for %s/%s...\n", runtime.GOOS, runtime.GOARCH)

	// Get current binary path
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find current binary: %w", err)
	}

	// Download to a temp file next to the current binary
	tmp := self + ".new"
	if err := download(url, tmp); err != nil {
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

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned %d — release may not exist yet", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			downloaded += int64(n)
			if total > 0 {
				pct := downloaded * 100 / total
				fmt.Printf("\r  %d%% (%d / %d KB)", pct, downloaded/1024, total/1024)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	fmt.Println()
	return nil
}

func binaryForPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "quikdb-frame-windows-amd64.exe"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "quikdb-frame-darwin-arm64"
		}
		return "quikdb-frame-darwin-amd64"
	default:
		if runtime.GOARCH == "arm64" {
			return "quikdb-frame-linux-arm64"
		}
		return "quikdb-frame-linux-amd64"
	}
}
