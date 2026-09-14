package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const archiveUploadLimit int64 = 64 << 20

var sourceIDPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type sourceArchive struct {
	File    *os.File
	SHA256  string
	Size    int64
	Entries int
}

func (a *sourceArchive) Close() { name := a.File.Name(); a.File.Close(); os.Remove(name) }

// Common credential locations and dependency caches are excluded at every depth.
// Built application output is preserved. This is not a general secret scanner.
func excludedSource(name string) bool {
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		p := strings.ToLower(part)
		if strings.HasPrefix(p, ".env") || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".key") {
			return true
		}
		switch p {
		case ".git", ".quikdb", ".ssh", ".aws", ".azure", ".docker", ".npmrc", ".pypirc", ".netrc", "id_rsa", "id_ed25519", "node_modules", ".venv", "venv", "__pycache__", ".pytest_cache":
			return true
		}
	}
	return false
}

type archiveLimitWriter struct {
	out io.Writer
	n   int64
}

func (w *archiveLimitWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > archiveUploadLimit-w.n {
		return 0, fmt.Errorf("compressed source exceeds 64 MiB")
	}
	n, err := w.out.Write(b)
	w.n += int64(n)
	return n, err
}

func packageSource(ctx context.Context, directory string) (_ *sourceArchive, err error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("source must be a real directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("cannot open source directory")
	}
	defer root.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("cannot create private source archive")
	}
	f, err := createPrivateArchive(filepath.Join(os.TempDir(), "quikdb-source-"+hex.EncodeToString(nonce[:])+".tar.gz"))
	if err != nil {
		return nil, fmt.Errorf("cannot create private source archive")
	}
	archive := &sourceArchive{File: f}
	defer func() {
		if err != nil {
			archive.Close()
		}
	}()
	hash := sha256.New()
	limited := &archiveLimitWriter{out: io.MultiWriter(f, hash)}
	gz := gzip.NewWriter(limited)
	tw := tar.NewWriter(gz)
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("cannot read source entry")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if excludedSource(name) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if strings.ContainsAny(name, "\\:\x00\r\n\t") {
			return fmt.Errorf("source contains an unsupported path")
		}
		if archive.Entries >= 20000 {
			return fmt.Errorf("source exceeds 20000 entries")
		}
		stat, err := root.Lstat(filepath.FromSlash(name))
		if err != nil {
			return fmt.Errorf("source entry changed during packaging")
		}
		if !stat.IsDir() && !stat.Mode().IsRegular() {
			return fmt.Errorf("source contains a link or special file; provide regular files")
		}
		size := stat.Size()
		if stat.IsDir() {
			size = 0
		}
		if size > 256<<20 || total > (1<<30)-size {
			return fmt.Errorf("expanded source exceeds application limits")
		}
		total += size
		archive.Entries++
		header := &tar.Header{Name: name, Mode: int64(stat.Mode().Perm()), Size: size, Typeflag: tar.TypeReg, ModTime: stat.ModTime()}
		if stat.IsDir() {
			header.Typeflag = tar.TypeDir
			header.Name += "/"
			if runtime.GOOS == "windows" {
				header.Mode = 0755
			}
			return tw.WriteHeader(header)
		}
		input, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			return fmt.Errorf("source entry changed during packaging")
		}
		defer input.Close()
		opened, err := input.Stat()
		if err != nil || !os.SameFile(stat, opened) {
			return fmt.Errorf("source entry changed during packaging")
		}
		// NTFS does not expose Unix executable bits. Preserve shebang scripts as
		// portable executables; ordinary Windows source files become 0644.
		if runtime.GOOS == "windows" {
			var prefix [2]byte
			n, readErr := input.Read(prefix[:])
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			if _, err := input.Seek(0, io.SeekStart); err != nil {
				return err
			}
			header.Mode = 0644
			if n == 2 && prefix[0] == '#' && prefix[1] == '!' {
				header.Mode = 0755
			}
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		copied, err := io.Copy(tw, io.LimitReader(input, size+1))
		final, statErr := input.Stat()
		pathFinal, pathErr := root.Lstat(filepath.FromSlash(name))
		if err != nil {
			return err
		}
		if copied != size || statErr != nil || pathErr != nil || !os.SameFile(stat, pathFinal) || !final.ModTime().Equal(stat.ModTime()) || final.Size() != size {
			return fmt.Errorf("source changed during packaging; retry after writes finish")
		}
		return nil
	})
	if closeErr := tw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if archive.Entries == 0 {
		return nil, fmt.Errorf("source contains no application files")
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	archive.Size = limited.n
	archive.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return archive, nil
}

func (c *APIClient) uploadSource(ctx context.Context, token string, archive *sourceArchive) (string, error) {
	if c.TokenProvider != nil {
		current, err := c.TokenProvider()
		if err != nil {
			return "", err
		}
		token = current
	}
	if _, err := archive.File.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/api/v1/deployment/sources", io.LimitReader(archive.File, archive.Size))
	if err != nil {
		return "", err
	}
	req.ContentLength = archive.Size
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/gzip")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("source upload response unavailable; no automatic retry or deployment was submitted")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return "", fmt.Errorf("invalid source upload response")
	}
	if response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("source upload failed (%d); check authentication, account limits and archive size", response.StatusCode)
	}
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			ID     string `json:"sourceId"`
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || !sourceIDPattern.MatchString(result.Data.ID) || result.Data.SHA256 != archive.SHA256 || result.Data.Size != archive.Size {
		return "", fmt.Errorf("source upload receipt does not match the packaged application")
	}
	return result.Data.ID, nil
}
