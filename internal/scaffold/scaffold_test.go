package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
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
