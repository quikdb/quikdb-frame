package dev

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/quikdb/quikdb-frame/internal/project"
)

func TestDiscoverServicesUsesValidatedManifestContract(t *testing.T) {
	root := t.TempDir()
	manifest, err := project.New("fixture-app", "postgres")
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range manifest.Services {
		if err := os.MkdirAll(filepath.Join(root, service.Path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := project.Save(filepath.Join(root, "quikdb.yaml"), manifest); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	services, err := discoverServices()
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 || services[0].name != "api" || services[0].port != "8080" || services[1].name != "web" || services[1].port != "3000" {
		t.Fatalf("unexpected services: %+v", services)
	}
}

func TestDiscoverServicesRejectsMissingDeclaredDirectory(t *testing.T) {
	root := t.TempDir()
	manifest, _ := project.New("fixture-app", "postgres")
	if err := project.Save(filepath.Join(root, "quikdb.yaml"), manifest); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if _, err := discoverServices(); err == nil {
		t.Fatal("missing declared service directory accepted")
	}
}
