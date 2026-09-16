package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	CurrentSchemaVersion = 1
	maxManifestBytes     = 1 << 20
)

var (
	namePattern              = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	modulePattern            = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*(/[A-Za-z0-9][A-Za-z0-9._~-]*)*$`)
	versionPattern           = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	envPattern               = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	schemaVersionLinePattern = regexp.MustCompile(`(?m)^schemaVersion\s*:`)
	legacyRoutingLinePattern = regexp.MustCompile(`(?m)^([ \t]*)-\s+path:\s+(\S+)\s+service:\s+([a-z][a-z0-9-]*)\s*$`)
)

type Manifest struct {
	SchemaVersion int                `yaml:"schemaVersion" json:"schemaVersion"`
	Name          string             `yaml:"name" json:"name"`
	Version       string             `yaml:"version" json:"version"`
	GoModule      string             `yaml:"goModule" json:"goModule"`
	Database      *Database          `yaml:"database,omitempty" json:"database,omitempty"`
	Services      map[string]Service `yaml:"services" json:"services"`
	Routing       Routing            `yaml:"routing" json:"routing"`
}

type Database struct {
	Primary *DatabaseTarget `yaml:"primary,omitempty" json:"primary,omitempty"`
	Cache   *DatabaseTarget `yaml:"cache,omitempty" json:"cache,omitempty"`
}

type DatabaseTarget struct {
	Type       string `yaml:"type" json:"type"`
	Migrations string `yaml:"migrations,omitempty" json:"migrations,omitempty"`
}

type Service struct {
	Type      string   `yaml:"type" json:"type"`
	Path      string   `yaml:"path" json:"path"`
	Build     Build    `yaml:"build" json:"build"`
	Port      int      `yaml:"port,omitempty" json:"port,omitempty"`
	Routes    []string `yaml:"routes,omitempty" json:"routes,omitempty"`
	Env       []string `yaml:"env,omitempty" json:"env,omitempty"`
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
}

// Build keeps the source root, Docker context and Dockerfile distinct. Context
// and Dockerfile are repository-relative; Dockerfile must remain inside Context.
type Build struct {
	Context    string `yaml:"context" json:"context"`
	Dockerfile string `yaml:"dockerfile" json:"dockerfile"`
	Target     string `yaml:"target,omitempty" json:"target,omitempty"`
}

type Routing struct {
	Rules []RoutingRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

type RoutingRule struct {
	Path    string `yaml:"path" json:"path"`
	Service string `yaml:"service" json:"service"`
}

func New(name, databaseType string) (Manifest, error) {
	m := Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Name:          name,
		Version:       "1.0.0",
		GoModule:      name,
		Database: &Database{
			Primary: &DatabaseTarget{Type: databaseType, Migrations: "shared/db/migrations"},
			Cache:   &DatabaseTarget{Type: "redis"},
		},
		Services: map[string]Service{
			"api": {
				Type: "api", Path: "services/api", Build: Build{Context: ".", Dockerfile: "services/api/Dockerfile", Target: "runtime"}, Port: 8080,
				Routes: []string{"/api/*"},
				Env:    []string{"DATABASE_URL", "REDIS_URL", "JWT_SECRET", "PORT"},
			},
			"web": {
				Type: "web", Path: "services/web", Build: Build{Context: ".", Dockerfile: "services/web/Dockerfile", Target: "runtime"}, Port: 3000,
				Routes: []string{"/*"}, Env: []string{"API_URL", "PORT"}, DependsOn: []string{"api"},
			},
		},
		Routing: Routing{Rules: []RoutingRule{{Path: "/api/*", Service: "api"}, {Path: "/*", Service: "web"}}},
	}
	return m, m.Validate()
}

func Load(filename string) (Manifest, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return Manifest{}, fmt.Errorf("read project manifest: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("project manifest must be a regular file")
	}
	if info.Size() > maxManifestBytes {
		return Manifest{}, fmt.Errorf("project manifest exceeds %d bytes", maxManifestBytes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return Manifest{}, fmt.Errorf("read project manifest: %w", err)
	}
	return Parse(data)
}

func Parse(data []byte) (Manifest, error) {
	legacy := !schemaVersionLinePattern.Match(data)
	if legacy {
		data = legacyRoutingLinePattern.ReplaceAll(data, []byte("${1}- path: ${2}\n${1}  service: ${3}"))
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse project manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, fmt.Errorf("parse project manifest: multiple YAML documents are not allowed")
		}
		return Manifest{}, fmt.Errorf("parse project manifest: %w", err)
	}
	// Manifests generated before the versioned contract are the v1 shape. Their
	// routing template put service on the path scalar, so repair that exact form.
	if legacy {
		if err := normalizeLegacyRouting(&manifest); err != nil {
			return Manifest{}, fmt.Errorf("invalid legacy project manifest: %w", err)
		}
		manifest.SchemaVersion = CurrentSchemaVersion
	}
	normalizeBuildDefaults(&manifest)
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("invalid project manifest: %w", err)
	}
	return manifest, nil
}

func Marshal(manifest Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid project manifest: %w", err)
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("serialize project manifest: %w", err)
	}
	return data, nil
}

func Save(filename string, manifest Manifest) error {
	data, err := Marshal(manifest)
	if err != nil {
		return err
	}
	temporary := filename + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return fmt.Errorf("write project manifest: %w", err)
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace project manifest: %w", err)
	}
	return nil
}

func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", CurrentSchemaVersion)
	}
	if err := validateName("project name", manifest.Name); err != nil {
		return err
	}
	if !versionPattern.MatchString(manifest.Version) {
		return fmt.Errorf("version must use major.minor.patch numbers")
	}
	if !modulePattern.MatchString(manifest.GoModule) {
		return fmt.Errorf("goModule must be a clean Go import path")
	}
	for _, segment := range strings.Split(manifest.GoModule, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("goModule must be a clean Go import path")
		}
	}
	if err := validateDatabase(manifest.Database); err != nil {
		return err
	}
	if len(manifest.Services) == 0 || len(manifest.Services) > 64 {
		return fmt.Errorf("services must contain between 1 and 64 entries")
	}

	ports := map[int]string{}
	paths := map[string]string{}
	routes := map[string]string{}
	for name, service := range manifest.Services {
		if err := validateName("service name", name); err != nil {
			return err
		}
		if err := validateService(name, service); err != nil {
			return err
		}
		cleanPath := path.Clean(service.Path)
		if previous, ok := paths[cleanPath]; ok {
			return fmt.Errorf("services %s and %s use the same path", previous, name)
		}
		paths[cleanPath] = name
		if service.Port != 0 {
			if previous, ok := ports[service.Port]; ok {
				return fmt.Errorf("services %s and %s use the same port", previous, name)
			}
			ports[service.Port] = name
		}
		for _, route := range service.Routes {
			if previous, ok := routes[route]; ok {
				return fmt.Errorf("services %s and %s use the same route %s", previous, name, route)
			}
			routes[route] = name
		}
	}
	if err := validateDependencies(manifest.Services); err != nil {
		return err
	}
	if len(manifest.Routing.Rules) != len(routes) {
		return fmt.Errorf("routing rules must contain exactly one rule for every service route")
	}
	seenRules := map[string]bool{}
	for _, rule := range manifest.Routing.Rules {
		_, ok := manifest.Services[rule.Service]
		if !ok {
			return fmt.Errorf("routing rule %s references unknown service %s", rule.Path, rule.Service)
		}
		if routes[rule.Path] != rule.Service {
			return fmt.Errorf("routing rule %s does not match a declared route for %s", rule.Path, rule.Service)
		}
		key := rule.Path + "\x00" + rule.Service
		if seenRules[key] {
			return fmt.Errorf("duplicate routing rule for %s", rule.Path)
		}
		seenRules[key] = true
	}
	return nil
}

func (manifest *Manifest) AddService(serviceType, shortName string) (string, error) {
	if err := validateName("service name", shortName); err != nil {
		return "", err
	}
	if _, ok := allowedServiceTypes[serviceType]; !ok {
		return "", fmt.Errorf("service type must be api, web, ws or worker")
	}
	fullName := serviceType + "-" + shortName
	if _, exists := manifest.Services[fullName]; exists {
		return "", fmt.Errorf("service %s already exists", fullName)
	}
	servicePath := "services/" + fullName
	service := Service{Type: serviceType, Path: servicePath, Build: Build{Context: ".", Dockerfile: servicePath + "/Dockerfile", Target: "runtime"}}
	switch serviceType {
	case "api":
		service.Port = manifest.nextPort(8080)
		service.Routes = []string{"/api/" + shortName + "/*"}
		service.Env = []string{"DATABASE_URL", "REDIS_URL", "JWT_SECRET", "PORT"}
	case "ws":
		service.Port = manifest.nextPort(8081)
		service.Routes = []string{"/ws/" + shortName + "/*"}
		service.Env = []string{"REDIS_URL", "JWT_SECRET", "PORT"}
	case "web":
		service.Port = manifest.nextPort(3000)
		service.Routes = []string{"/" + shortName + "/*"}
		service.Env = []string{"API_URL", "PORT"}
		if _, ok := manifest.Services["api"]; ok {
			service.DependsOn = []string{"api"}
		}
	case "worker":
		service.Env = []string{"DATABASE_URL", "REDIS_URL"}
	}
	manifest.Services[fullName] = service
	for _, route := range service.Routes {
		manifest.Routing.Rules = append(manifest.Routing.Rules, RoutingRule{Path: route, Service: fullName})
	}
	if err := manifest.Validate(); err != nil {
		delete(manifest.Services, fullName)
		manifest.Routing.Rules = manifest.Routing.Rules[:len(manifest.Routing.Rules)-len(service.Routes)]
		return "", err
	}
	return fullName, nil
}

var allowedServiceTypes = map[string]struct{}{"api": {}, "web": {}, "ws": {}, "worker": {}}

func validateName(label, value string) error {
	if !namePattern.MatchString(value) {
		return fmt.Errorf("%s %q must start with a lowercase letter and contain only lowercase letters, numbers or hyphens", label, value)
	}
	return nil
}

func validateDatabase(database *Database) error {
	if database == nil {
		return nil
	}
	if database.Primary != nil {
		allowed := map[string]bool{"postgres": true, "mongo": true, "mysql": true, "sqlite": true}
		if !allowed[database.Primary.Type] {
			return fmt.Errorf("database.primary.type must be postgres, mongo, mysql or sqlite")
		}
		if database.Primary.Migrations != "" {
			if _, err := cleanRelativePath("database.primary.migrations", database.Primary.Migrations); err != nil {
				return err
			}
		}
	}
	if database.Cache != nil && database.Cache.Type != "redis" {
		return fmt.Errorf("database.cache.type must be redis")
	}
	return nil
}

func validateService(name string, service Service) error {
	if _, ok := allowedServiceTypes[service.Type]; !ok {
		return fmt.Errorf("service %s type must be api, web, ws or worker", name)
	}
	if _, err := cleanRelativePath("service "+name+" path", service.Path); err != nil {
		return err
	}
	contextPath, err := cleanBuildContext("service "+name+" build.context", service.Build.Context)
	if err != nil {
		return err
	}
	dockerfilePath, err := cleanRelativePath("service "+name+" build.dockerfile", service.Build.Dockerfile)
	if err != nil {
		return err
	}
	if contextPath != "." && dockerfilePath != contextPath && !strings.HasPrefix(dockerfilePath, contextPath+"/") {
		return fmt.Errorf("service %s build.dockerfile must be inside build.context", name)
	}
	if service.Build.Target != "" && !namePattern.MatchString(service.Build.Target) {
		return fmt.Errorf("service %s build.target must use a service-style name", name)
	}
	if service.Type == "worker" {
		if service.Port != 0 || len(service.Routes) != 0 {
			return fmt.Errorf("worker service %s cannot declare a port or routes", name)
		}
	} else if service.Port < 1 || service.Port > 65535 {
		return fmt.Errorf("service %s port must be between 1 and 65535", name)
	}
	seenRoutes := map[string]bool{}
	for _, route := range service.Routes {
		if !strings.HasPrefix(route, "/") || strings.ContainsAny(route, " \t\r\n") || strings.Contains(route, "//") {
			return fmt.Errorf("service %s has invalid route %q", name, route)
		}
		if strings.Contains(route, "*") && !strings.HasSuffix(route, "/*") {
			return fmt.Errorf("service %s wildcard route %q must end in /*", name, route)
		}
		if seenRoutes[route] {
			return fmt.Errorf("service %s repeats route %s", name, route)
		}
		seenRoutes[route] = true
	}
	seenEnv := map[string]bool{}
	for _, variable := range service.Env {
		if !envPattern.MatchString(variable) {
			return fmt.Errorf("service %s has invalid environment name %q", name, variable)
		}
		if seenEnv[variable] {
			return fmt.Errorf("service %s repeats environment name %s", name, variable)
		}
		seenEnv[variable] = true
	}
	return nil
}

func cleanBuildContext(label, value string) (string, error) {
	if value == "." {
		return value, nil
	}
	return cleanRelativePath(label, value)
}

func normalizeBuildDefaults(manifest *Manifest) {
	if manifest.GoModule == "" {
		manifest.GoModule = manifest.Name
	}
	for name, service := range manifest.Services {
		if service.Build.Context == "" && service.Build.Dockerfile == "" && service.Path != "" {
			service.Build = Build{Context: service.Path, Dockerfile: path.Join(service.Path, "Dockerfile")}
			manifest.Services[name] = service
		}
	}
}

func cleanRelativePath(label, value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("%s must be a relative slash-separated path", label)
	}
	trimmed := strings.TrimSuffix(value, "/")
	clean := path.Clean(trimmed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != trimmed {
		return "", fmt.Errorf("%s must be a clean relative path", label)
	}
	return clean, nil
}

func normalizeLegacyRouting(manifest *Manifest) error {
	for index, rule := range manifest.Routing.Rules {
		if rule.Service != "" {
			continue
		}
		fields := strings.Fields(rule.Path)
		if len(fields) != 3 || fields[1] != "service:" {
			return fmt.Errorf("routing rule %q has no service", rule.Path)
		}
		manifest.Routing.Rules[index] = RoutingRule{Path: fields[0], Service: fields[2]}
	}
	return nil
}

func validateDependencies(services map[string]Service) error {
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("service dependencies contain a cycle at %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		seen := map[string]bool{}
		for _, dependency := range services[name].DependsOn {
			if _, ok := services[dependency]; !ok {
				return fmt.Errorf("service %s depends on unknown service %s", name, dependency)
			}
			if dependency == name || seen[dependency] {
				return fmt.Errorf("service %s has invalid dependency %s", name, dependency)
			}
			seen[dependency] = true
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func (manifest Manifest) nextPort(first int) int {
	used := map[int]bool{}
	for _, service := range manifest.Services {
		used[service.Port] = true
	}
	for candidate := first; candidate <= 65535; candidate++ {
		if !used[candidate] {
			return candidate
		}
	}
	return 0
}
