package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureSourceID = "12345678-1234-1234-1234-123456789abc"

func archiveFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{"server.js": "console.log('original business logic')", "dist/original.js": "compiled original", "start.sh": "#!/bin/sh\nexec node server.js\n", ".env": "TOKEN=private-fixture", ".npmrc": "private-fixture", "node_modules/private.js": "dependency fixture", ".git/config": "private-fixture", ".aws/credentials": "private-fixture"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(file), 0700)
		if err := os.WriteFile(file, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(root, "quikdb.json")
	os.WriteFile(config, []byte(`{"appType":"nodejs","startCommand":"node server.js","installCommand":"","buildCommand":"","port":8123,"healthCheck":"/ready"}`), 0600)
	return root, config
}
func readArchive(t *testing.T, raw io.Reader) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	files := map[string]string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			bytes, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			files[header.Name] = string(bytes)
			if header.Name == "start.sh" && header.Mode&0111 == 0 {
				t.Fatal("lost executable mode")
			}
		}
	}
	return files
}
func TestLocalArchivePreservesBusinessFilesAndExcludesCredentials(t *testing.T) {
	root, _ := archiveFixture(t)
	first, err := packageSource(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	files := readArchive(t, first.File)
	if !strings.Contains(files["server.js"], "original business logic") || files["dist/original.js"] != "compiled original" {
		t.Fatal("business files changed")
	}
	for name, content := range files {
		if excludedSource(name) || strings.Contains(content, "private-fixture") {
			t.Fatal("included credential location")
		}
	}
	second, err := packageSource(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.SHA256 != second.SHA256 || first.Size != second.Size {
		t.Fatal("unchanged files generated different archive identity")
	}
	name := second.File.Name()
	second.Close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("private archive not removed")
	}
}
func TestLocalArchiveRejectsLinksAndCancellation(t *testing.T) {
	root, _ := archiveFixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("outside fixture"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	if a, err := packageSource(context.Background(), root); err == nil {
		a.Close()
		t.Fatal("accepted symbolic link")
	}
	os.Remove(filepath.Join(root, "link"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a, err := packageSource(ctx, root); err == nil {
		a.Close()
		t.Fatal("ignored cancellation")
	}
}
func TestArchiveUploadChecksReceiptAndRotatesCredentialWithoutRetries(t *testing.T) {
	root, _ := archiveFixture(t)
	archive, err := packageSource(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, failure := range []string{"", "digest", "size", "handle", "status", "json"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Content-Type") != "application/gzip" {
					t.Error("wrong credential/type")
				}
				bytes, _ := io.ReadAll(r.Body)
				sum := sha256.Sum256(bytes)
				digest := hex.EncodeToString(sum[:])
				size := len(bytes)
				id := fixtureSourceID
				if failure == "digest" {
					digest = strings.Repeat("0", 64)
				}
				if failure == "size" {
					size++
				}
				if failure == "handle" {
					id = "https://private.invalid/source"
				}
				if failure == "status" {
					w.WriteHeader(503)
				} else {
					w.WriteHeader(201)
				}
				if failure == "json" {
					fmt.Fprint(w, "invalid")
					return
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]interface{}{"sourceId": id, "sha256": digest, "size": size}})
			})
			c.TokenProvider = func() (string, error) { return "fixture-token", nil }
			id, err := c.uploadSource(context.Background(), "old", archive)
			if (err != nil) != (failure != "") || (failure == "" && id != fixtureSourceID) {
				t.Fatalf("receipt acceptance: %q %v", id, err)
			}
			if calls != 1 {
				t.Fatal("retried upload mutation")
			}
		})
	}
}
func TestArchiveDryRunIsOfflineAndNeverPrintsSourceOrSecretValues(t *testing.T) {
	root, config := archiveFixture(t)
	oldFactory := deployClientFactory
	defer func() { deployClientFactory = oldFactory }()
	deployClientFactory = func() *APIClient { t.Fatal("dry-run requested API/authentication"); return nil }
	oldOut := os.Stdout
	read, write, _ := os.Pipe()
	os.Stdout = write
	err := Command([]string{"--source", root, "--config", config, "--name", "fixture", "--dry-run", "--json"})
	write.Close()
	os.Stdout = oldOut
	raw, _ := io.ReadAll(read)
	read.Close()
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]interface{}
	if json.Unmarshal(raw, &plan) != nil || plan["source"] != "local archive" || plan["mode"] != "as-is" {
		t.Fatal("invalid offline plan")
	}
	if strings.Contains(string(raw), "private-fixture") || strings.Contains(string(raw), root) || strings.Contains(string(raw), "original business logic") {
		t.Fatal("plan leaked local content")
	}
}
func TestArchiveCommandUploadsOnlyAfterAccountPreflightThenCreatesByHandle(t *testing.T) {
	credentialFixture(t)
	t.Setenv("QUIKDB_TOKEN", "")
	if err := SaveAuth(&AuthConfig{Token: "fixture-token", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	root, config := archiveFixture(t)
	var calls []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/deployment/list":
			fmt.Fprint(w, `{"success":true,"data":[]}`)
		case "/api/v1/deployment/source-capabilities":
			fmt.Fprint(w, `{"success":true,"data":{"archiveDeployment":true}}`)
		case "/api/v1/deployment/sources":
			bytes, _ := io.ReadAll(r.Body)
			hash := sha256.Sum256(bytes)
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"success":true,"data":{"sourceId":"%s","sha256":"%s","size":%d}}`, fixtureSourceID, hex.EncodeToString(hash[:]), len(bytes))
		case "/api/v1/deployment/create":
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["sourceId"] != fixtureSourceID || payload["repositoryUrl"] != nil || payload["repositoryBranch"] != nil {
				t.Error("archive confused with Git")
			}
			cfg := payload["configuration"].(map[string]interface{})
			if cfg["startCommand"] != "node server.js" || cfg["buildCommand"] != "" || cfg["healthCheck"] != "/ready" {
				t.Error("original settings changed")
			}
			fmt.Fprint(w, `{"success":true,"data":{"deploymentId":"fixture","status":"pending"}}`)
		case "/api/v1/deployment/fixture":
			fmt.Fprint(w, `{"success":true,"data":{"deployment":{"deploymentId":"fixture","status":"live"}}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	oldFactory := deployClientFactory
	defer func() { deployClientFactory = oldFactory }()
	deployClientFactory = func() *APIClient { return c }
	if err := Command([]string{"--source", root, "--config", config, "--name", "fixture", "--json"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "/api/v1/deployment/list,/api/v1/deployment/source-capabilities,/api/v1/deployment/sources,/api/v1/deployment/create,/api/v1/deployment/fixture" {
		t.Fatalf("unexpected submission order %v", calls)
	}
}
func TestArchiveCannotSilentlyReuseDifferentApplicationSource(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("mutated mismatched source")
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"deployment":{"deploymentId":"fixture","status":"live","sourceSnapshot":{"version":1,"kind":"archive","sha256":"different"}}}}`)
	})
	_, err := deployApplication(context.Background(), c, "fixture-token", DeployRequest{ApplicationName: "app", SourceSHA256: strings.Repeat("a", 64)}, map[string]Deployment{"app": {DeploymentID: "fixture"}}, true)
	if err == nil {
		t.Fatal("silently kept a different source")
	}
}

func TestArchiveInactiveConsumerCannotUploadOrCreate(t *testing.T) {
	credentialFixture(t)
	t.Setenv("QUIKDB_TOKEN", "")
	if err := SaveAuth(&AuthConfig{Token: "fixture-token", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	root, config := archiveFixture(t)
	mutations := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations++
			w.WriteHeader(500)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/list") {
			fmt.Fprint(w, `{"success":true,"data":[]}`)
		} else {
			fmt.Fprint(w, `{"success":true,"data":{"archiveDeployment":false}}`)
		}
	})
	old := deployClientFactory
	defer func() { deployClientFactory = old }()
	deployClientFactory = func() *APIClient { return c }
	err := Command([]string{"--source", root, "--config", config, "--name", "fixture", "--json"})
	if err == nil || !strings.Contains(err.Error(), "not active") || mutations != 0 {
		t.Fatal("inactive archive consumer accepted mutations")
	}
}

func TestArchiveRepeatPreservesIdentityWithoutUploadingAgain(t *testing.T) {
	credentialFixture(t)
	t.Setenv("QUIKDB_TOKEN", "")
	if err := SaveAuth(&AuthConfig{Token: "fixture-token", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	root, config := archiveFixture(t)
	packaged, err := packageSource(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	digest := packaged.SHA256
	packaged.Close()
	mutations := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations++
			w.WriteHeader(500)
			return
		}
		switch r.URL.Path {
		case "/api/v1/deployment/list":
			fmt.Fprint(w, `{"success":true,"data":[{"deploymentId":"same-fixture-id","applicationName":"fixture","status":"live"}]}`)
		case "/api/v1/deployment/source-capabilities":
			fmt.Fprint(w, `{"success":true,"data":{"archiveDeployment":true}}`)
		case "/api/v1/deployment/same-fixture-id":
			fmt.Fprintf(w, `{"success":true,"data":{"deployment":{"deploymentId":"same-fixture-id","status":"live","sourceSnapshot":{"version":1,"kind":"archive","sha256":"%s"}}}}`, digest)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	old := deployClientFactory
	defer func() { deployClientFactory = old }()
	deployClientFactory = func() *APIClient { return c }
	if err := Command([]string{"--source", root, "--config", config, "--name", "fixture", "--json"}); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 {
		t.Fatal("repeated upload/deployment")
	}
}
