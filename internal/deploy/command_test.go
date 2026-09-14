package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeployOptionsRejectUnsupportedOrUnsafeInputs(t *testing.T) {
	for _, args := range [][]string{{"--mode", "magic"}, {"--port", "65536"}, {"--subdirectory", "../secrets"}, {"--subdirectory", "/etc"}, {"--subdirectory", "api\\..\\secrets"}, {"--source", "local"}, {"api", "web"}} {
		if _, err := ParseDeployOptions(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	for _, repo := range []string{"https://token@github.com/team/app", "https://github.com.evil.test/team/app", "https://github.com/team/app?token=secret", "https://github.com/team/app/tree/main", "https://github.com/team/.."} {
		o := DeployOptions{Repo: repo, Branch: "main"}
		if resolveSource(&o) == nil {
			t.Errorf("accepted repository %s", repo)
		}
	}
	if err := resolveSource(&DeployOptions{Repo: "https://github.com/team/app"}); err == nil {
		t.Fatal("invented a branch for external repository")
	}
	o := DeployOptions{Repo: "git@github.com:team/app.git", Branch: "feature/api"}
	if err := resolveSource(&o); err != nil || o.Repo != "https://github.com/team/app" || o.Name != "app" {
		t.Fatalf("source %+v: %v", o, err)
	}
}

func TestAsIsDetectorPreservesOriginalRuntimeAndDockerfile(t *testing.T) {
	for _, source := range []string{"auto-detected", "dockerfile"} {
		t.Run(source, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/deployment/detect-config" || r.Method != "POST" {
					t.Errorf("unexpected request %s", r.URL.Path)
				}
				var input map[string]string
				json.NewDecoder(r.Body).Decode(&input)
				if input["repositoryUrl"] != "https://github.com/team/app" || input["branch"] != "main" {
					t.Error("source changed during detection")
				}
				settings := map[string]interface{}{"buildCommand": "npm ci && npm run build", "startCommand": "node server.js", "port": 3000, "nodeVersion": "22"}
				if source == "dockerfile" {
					settings["startCommand"] = ""
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]interface{}{"appType": "nodejs", "framework": "express", "configSource": source, "detectedConfig": settings}})
			})
			request, err := planAsIs(context.Background(), c, "fixture-token", DeployOptions{Repo: "https://github.com/team/app", Branch: "main", Name: "app"})
			if err != nil || request.Configuration["appType"] != "nodejs" || request.Configuration["configSource"] != source || request.Configuration["nodeVersion"] != "22" {
				t.Fatalf("plan %+v: %v", request, err)
			}
			if _, found := request.Configuration["environmentVariables"]; found {
				t.Fatal("uploaded environment defaults")
			}
		})
	}
}

func TestExplicitMonorepoSettingsAndPortArePreserved(t *testing.T) {
	file := filepath.Join(t.TempDir(), "deployment.json")
	os.WriteFile(file, []byte(`{"appType":"python","configSource":"dockerfile","port":8000,"resources":{"ram":512},"environmentVariables":{"FIXTURE_SECRET":"do-not-print"}}`), 0600)
	request, err := planAsIs(context.Background(), nil, "fixture", DeployOptions{Repo: "https://github.com/team/app", Branch: "main", Name: "api", Subdirectory: "apps/api", Config: file, Port: 9000})
	if err != nil || request.Subdirectory != "apps/api" || request.Configuration["port"] != 9000 || request.Configuration["internalPort"] != 9000 || request.Configuration["appType"] != "python" {
		t.Fatalf("plan %+v: %v", request, err)
	}
}

func TestAsIsDryRunNeverSubmitsAndDoesNotPrintSecrets(t *testing.T) {
	credentialFixture(t)
	t.Setenv("QUIKDB_TOKEN", "")
	SaveAuth(&AuthConfig{Token: "fixture-access", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)})
	oldFactory := deployClientFactory
	t.Cleanup(func() { deployClientFactory = oldFactory })
	deployClientFactory = func() *APIClient {
		return testClient(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("dry run with explicit config made API request %s", r.URL.Path)
		})
	}
	file := filepath.Join(t.TempDir(), "deployment.json")
	os.WriteFile(file, []byte(`{"appType":"nodejs","startCommand":"node server.js --fixture-secret=do-not-print","port":3000,"environmentVariables":{"FIXTURE_SECRET":"do-not-print"}}`), 0600)
	oldOut := os.Stdout
	reader, writer, _ := os.Pipe()
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldOut; reader.Close(); writer.Close() })
	err := Command([]string{"--repo", "https://github.com/team/app", "--branch", "main", "--config", file, "--dry-run", "--json"})
	writer.Close()
	os.Stdout = oldOut
	raw, _ := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "do-not-print") || strings.Contains(string(raw), "fixture-access") {
		t.Fatal("review leaked a secret")
	}
	var summary map[string]interface{}
	if json.Unmarshal(raw, &summary) != nil || summary["mode"] != "as-is" || summary["repositoryBranch"] != "main" {
		t.Fatalf("invalid review %s", raw)
	}
}

func TestUncertifiedConversionBlocksBeforeAuthenticationOrSubmission(t *testing.T) {
	t.Setenv("QUIKDB_TOKEN", "")
	err := Command([]string{"--repo", "https://github.com/team/app", "--branch", "main", "--mode", "frame"})
	if err == nil || !strings.Contains(err.Error(), "not certified") {
		t.Fatalf("conversion result %v", err)
	}
}

func TestAsIsCommandCreatesOriginalApplicationAndReportsItsLiveID(t *testing.T) {
	credentialFixture(t)
	t.Setenv("QUIKDB_TOKEN", "")
	if err := SaveAuth(&AuthConfig{Token: "fixture-token", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	oldFactory := deployClientFactory
	t.Cleanup(func() { deployClientFactory = oldFactory })
	paths := []string{}
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/deployment/list":
			io.WriteString(w, `{"success":true,"data":{"deployments":[],"pagination":{"page":1,"pages":0}}}`)
		case "/api/v1/deployment/create":
			var payload DeployRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.Subdirectory != "" || payload.RepositoryURL != "https://github.com/team/app" || payload.RepositoryBranch != "main" || payload.Configuration["appType"] != "nodejs" || payload.Configuration["startCommand"] != "node server.js" {
				t.Errorf("original request altered: %+v", payload)
			}
			io.WriteString(w, `{"success":true,"data":{"deploymentId":"original-id","status":"building"}}`)
		case "/api/v1/deployment/original-id":
			io.WriteString(w, `{"success":true,"data":{"deployment":{"deploymentId":"original-id","status":"live","applicationName":"app"}}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	deployClientFactory = func() *APIClient { return client }
	file := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(file, []byte(`{"appType":"nodejs","startCommand":"node server.js","port":3000}`), 0600); err != nil {
		t.Fatal(err)
	}
	oldOut := os.Stdout
	reader, writer, _ := os.Pipe()
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldOut; reader.Close(); writer.Close() })
	err := Command([]string{"--repo", "https://github.com/team/app", "--branch", "main", "--config", file, "--json"})
	writer.Close()
	os.Stdout = oldOut
	raw, _ := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Mode       string     `json:"mode"`
		Deployment Deployment `json:"deployment"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Mode != "as-is" || result.Deployment.DeploymentID != "original-id" || result.Deployment.Status != "live" {
		t.Fatalf("result %s", raw)
	}
	if strings.Join(paths, ",") != "/api/v1/deployment/list,/api/v1/deployment/create,/api/v1/deployment/original-id" {
		t.Fatalf("unexpected mutations %v", paths)
	}
}

func TestServiceDirectoryDetectionUsesTheSameOwnedAPIAndOriginalSettings(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/api/v1/deployment/detect-config" || input["subdirectory"] != "apps/api" || input["branch"] != "feature/api" {
			t.Fatalf("wrong service source: %+v", input)
		}
		io.WriteString(w, `{"success":true,"data":{"appType":"python","framework":"flask","configSource":"quikdb.json","subdirectory":"apps/api","detectedConfig":{"buildCommand":"python build.py","startCommand":"python original.py","installCommand":"pip install -r locked.txt","runtimeVersion":"3.12","port":8080,"healthCheck":"/ready"}}}`)
	})
	request, err := planAsIs(context.Background(), c, "fixture-token", DeployOptions{Repo: "https://github.com/quikdb/fixture", Branch: "feature/api", Name: "fixture-api", Subdirectory: "apps/api"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Subdirectory != "apps/api" || request.Configuration["startCommand"] != "python original.py" || request.Configuration["installCommand"] != "pip install -r locked.txt" || request.Configuration["healthCheck"] != "/ready" {
		t.Fatalf("changed original settings: %+v", request)
	}
}

func TestServiceDetectionFailuresCannotBecomeADeploymentPlan(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, `{"success":false,"error":"repository_access_denied"}`)
	})
	if _, err := planAsIs(context.Background(), c, "fixture-token", DeployOptions{Subdirectory: "apps/api"}); err == nil {
		t.Fatal("ignored private-source denial")
	}
}

func TestSelectedServiceCannotSilentlyUseRepositoryRootSettings(t *testing.T) {
	for _, returned := range []string{"", "apps/other"} {
		t.Run(returned, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"appType": "nodejs", "subdirectory": returned, "detectedConfig": map[string]any{"startCommand": "node root.js"}}})
			})
			if _, err := planAsIs(context.Background(), c, "fixture-token", DeployOptions{Repo: "https://github.com/quikdb/fixture", Branch: "main", Subdirectory: "apps/api"}); err == nil {
				t.Fatal("accepted settings from a different service directory")
			}
		})
	}
}
