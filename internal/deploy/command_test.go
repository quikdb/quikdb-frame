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
	if _, err := planAsIs(context.Background(), nil, "fixture", DeployOptions{Subdirectory: "apps/api"}); err == nil {
		t.Fatal("detected repository root as a service")
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
