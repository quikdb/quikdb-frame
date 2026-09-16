package deploy

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quikdb/quikdb-frame/internal/project"
)

func TestNativeServicesComeFromProjectManifest(t *testing.T) {
	root := t.TempDir()
	manifest, err := project.New("fixture-app", "postgres")
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range manifest.Services {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(service.Path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(service.Build.Dockerfile)), []byte("FROM scratch AS runtime\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := project.Save(filepath.Join(root, "quikdb.yaml"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture-app\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	withinDirectory(t, root, func() {
		services, err := findServices("api")
		if err != nil {
			t.Fatal(err)
		}
		if len(services) != 1 {
			t.Fatalf("services: %+v", services)
		}
		service := services[0]
		if service.Name != "fixture-app-api" || service.ManifestID != "api" || service.Type != "api" || service.Path != "services/api" || service.Port != 8080 || service.Build.Context != "." || service.Build.Dockerfile != "services/api/Dockerfile" {
			t.Fatalf("manifest fields changed: %+v", service)
		}
	})
}

func TestNativeRootContextAndWorkersFailBeforeSubmission(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unsupported native plan made API request %s", r.URL.Path)
	})
	rootContext := ServiceConfig{Name: "fixture-api", ManifestID: "api", Type: "api", Path: "services/api", Port: 8080, Build: project.Build{Context: ".", Dockerfile: "services/api/Dockerfile", Target: "runtime"}}
	if _, err := deployService(context.Background(), client, "fixture-token", "https://github.com/team/app", "main", rootContext, nil); err == nil || !strings.Contains(err.Error(), "cannot represent") {
		t.Fatalf("root context result: %v", err)
	}
	worker := ServiceConfig{Name: "fixture-worker", ManifestID: "worker-jobs", Type: "worker", Path: "services/worker-jobs", Build: project.Build{Context: "services/worker-jobs", Dockerfile: "services/worker-jobs/Dockerfile"}}
	if _, err := deployService(context.Background(), client, "fixture-token", "https://github.com/team/app", "main", worker, nil); err == nil || !strings.Contains(err.Error(), "requires an HTTP port") {
		t.Fatalf("worker result: %v", err)
	}
}

func withinDirectory(t *testing.T, directory string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	}()
	fn()
}
