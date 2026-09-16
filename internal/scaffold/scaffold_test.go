package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quikdb/quikdb-frame/internal/project"
)

func TestInitAndAddProduceCoherentManifests(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "fixture-app")
	if err := Init(app, "postgres"); err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Load(filepath.Join(app, "quikdb.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Services) != 2 {
		t.Fatalf("unexpected initial manifest: %+v", manifest)
	}
	rootModule, err := os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil || !strings.Contains(string(rootModule), "module fixture-app") {
		t.Fatalf("root module: %s: %v", rootModule, err)
	}
	if _, err := os.Stat(filepath.Join(app, "services", "api", "go.mod")); !os.IsNotExist(err) {
		t.Fatal("generated a nested API module")
	}
	apiSource, err := os.ReadFile(filepath.Join(app, "services", "api", "routes.go"))
	if err != nil || !strings.Contains(string(apiSource), `"fixture-app/shared/auth"`) {
		t.Fatalf("API does not import shared auth contract: %v", err)
	}
	healthSource, err := os.ReadFile(filepath.Join(app, "services", "api", "health.go"))
	if err != nil || !strings.Contains(string(healthSource), `"fixture-app/shared/db"`) {
		t.Fatalf("API does not import shared database contract: %v", err)
	}
	apiDockerfile, err := os.ReadFile(filepath.Join(app, "services", "api", "Dockerfile"))
	if err != nil || !strings.Contains(string(apiDockerfile), "COPY shared ./shared") || !strings.Contains(string(apiDockerfile), "COPY services/api ./services/api") {
		t.Fatalf("API Dockerfile does not use the declared root context: %v", err)
	}

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(app); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	for _, tc := range []struct{ serviceType, name string }{{"api", "billing"}, {"ws", "chat"}, {"worker", "email"}, {"web", "admin"}} {
		if err := Add(tc.serviceType, tc.name); err != nil {
			t.Fatalf("add %s: %v", tc.serviceType, err)
		}
		configPath := filepath.Join("services", tc.serviceType+"-"+tc.name, "quikdb.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatalf("generated %s is invalid JSON: %v", configPath, err)
		}
		if _, err := os.Stat(filepath.Join("services", tc.serviceType+"-"+tc.name, "go.mod")); !os.IsNotExist(err) {
			t.Fatalf("generated a nested module for %s", tc.name)
		}
	}
	manifest, err = project.Load("quikdb.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Services) != 6 || len(manifest.Routing.Rules) != 5 {
		t.Fatalf("added services missing from manifest: %+v", manifest)
	}
}

func TestInitRejectsInvalidNamesAndDatabaseBeforeWriting(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ name, database string }{{"Invalid_Name", "postgres"}, {"fixture", "unknown"}} {
		target := filepath.Join(root, tc.name)
		if err := Init(target, tc.database); err == nil {
			t.Fatalf("accepted %q/%q", tc.name, tc.database)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("left partial project at %s", target)
		}
	}
}

func TestAddFailsClosedWithoutTheDeclaredRootModule(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "fixture-app")
	if err := Init(app, "postgres"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(app, "go.mod")); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(app); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	})
	if err := Add("api", "billing"); err == nil || !strings.Contains(err.Error(), "project Go module") {
		t.Fatalf("missing root module result: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app, "services", "api-billing")); !os.IsNotExist(err) {
		t.Fatal("left a generated service after module validation failed")
	}
}
