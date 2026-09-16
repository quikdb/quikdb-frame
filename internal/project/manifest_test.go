package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultManifestRoundTrip(t *testing.T) {
	manifest, err := New("fixture-app", "postgres")
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "path: /api/*    service:") {
		t.Fatal("routing fields were serialized on one scalar line")
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SchemaVersion != 1 || parsed.Services["api"].Port != 8080 || parsed.Routing.Rules[0].Service != "api" {
		t.Fatalf("unexpected round trip: %+v", parsed)
	}
}

func TestLegacyManifestLoadsAsV1(t *testing.T) {
	data := []byte(`name: fixture-app
version: 1.0.0
database:
  primary:
    type: postgres
    migrations: shared/db/migrations/
services:
  api:
    type: api
    path: services/api
    port: 8080
    routes: [/api/*]
routing:
  rules:
    - path: /api/*    service: api
`)
	manifest, err := Parse(data)
	if err != nil || manifest.SchemaVersion != 1 || manifest.Routing.Rules[0].Service != "api" {
		t.Fatalf("manifest %+v: %v", manifest, err)
	}
}

func TestManifestRejectsUnsafeOrIncoherentInput(t *testing.T) {
	valid, err := New("fixture-app", "postgres")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Manifest){
		"unknown schema": func(m *Manifest) { m.SchemaVersion = 2 },
		"invalid name":   func(m *Manifest) { m.Name = "../fixture" },
		"path escape": func(m *Manifest) {
			s := m.Services["api"]
			s.Path = "../api"
			m.Services["api"] = s
		},
		"duplicate port": func(m *Manifest) {
			s := m.Services["web"]
			s.Port = 8080
			m.Services["web"] = s
		},
		"unknown dependency": func(m *Manifest) {
			s := m.Services["api"]
			s.DependsOn = []string{"missing"}
			m.Services["api"] = s
		},
		"dependency cycle": func(m *Manifest) {
			s := m.Services["api"]
			s.DependsOn = []string{"web"}
			m.Services["api"] = s
		},
		"route mismatch": func(m *Manifest) { m.Routing.Rules[0].Service = "web" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Services = cloneServices(valid.Services)
			candidate.Routing.Rules = append([]RoutingRule(nil), valid.Routing.Rules...)
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	if _, err := Parse([]byte("schemaVersion: 1\nname: fixture-app\nversion: 1.0.0\nunknown: true\nservices: {}\nrouting: {}\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Parse([]byte("name: fixture-app\n---\nname: second\n")); err == nil {
		t.Fatal("multiple documents accepted")
	}
}

func TestAddServiceProducesValidatedUniqueContract(t *testing.T) {
	manifest, _ := New("fixture-app", "postgres")
	for _, tc := range []struct{ serviceType, name string }{{"api", "billing"}, {"ws", "chat"}, {"worker", "email"}, {"web", "admin"}} {
		fullName, err := manifest.AddService(tc.serviceType, tc.name)
		if err != nil {
			t.Fatalf("add %s: %v", tc.serviceType, err)
		}
		if manifest.Services[fullName].Path != "services/"+fullName {
			t.Fatalf("wrong path for %s", fullName)
		}
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.AddService("api", "billing"); err == nil {
		t.Fatal("duplicate service accepted")
	}
	if _, err := manifest.AddService("api", "../unsafe"); err == nil {
		t.Fatal("unsafe service name accepted")
	}
}

func TestLoadRejectsSymlinkAndOversizedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("name: fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "quikdb.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Fatal("symlink manifest accepted")
	}
	large := filepath.Join(dir, "large.yaml")
	if err := os.WriteFile(large, make([]byte, maxManifestBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(large); err == nil {
		t.Fatal("oversized manifest accepted")
	}
}

func cloneServices(source map[string]Service) map[string]Service {
	result := make(map[string]Service, len(source))
	for name, service := range source {
		service.Routes = append([]string(nil), service.Routes...)
		service.Env = append([]string(nil), service.Env...)
		service.DependsOn = append([]string(nil), service.DependsOn...)
		result[name] = service
	}
	return result
}
