package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestV1SharedCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/deployment-manifest-v1.cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Manifest json.RawMessage `json:"manifest"`
		Valid    bool            `json:"valid"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			var manifest map[string]interface{}
			decodeErr := json.Unmarshal(fixture.Manifest, &manifest)
			valid := decodeErr == nil && validateManifestV1(manifest) == nil
			if valid != fixture.Valid {
				t.Fatalf("valid=%v expected=%v", valid, fixture.Valid)
			}
		})
	}
}
func manifestFile(t *testing.T, raw []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "quikdb.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return file
}
func TestV1ExplicitPlanPreservesOriginalCommandsAndPortOverride(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/deployment-manifest-v1.example.json")
	if err != nil {
		t.Fatal(err)
	}
	file := manifestFile(t, raw)
	plan, err := planAsIs(context.Background(), nil, "unused", DeployOptions{Config: file, Port: 9000, Subdirectory: "apps/api"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ManifestVersion != 1 || plan.Configuration["appType"] != "nodejs" || plan.Configuration["startCommand"] != "  node original.js  " || plan.Configuration["installCommand"] != "" || plan.Configuration["buildCommand"] != "" || plan.Configuration["nodeVersion"] != float64(22) || plan.Configuration["port"] != 9000 || plan.Configuration["internalPort"] != 9000 {
		t.Fatal("original settings not preserved")
	}
	encoded, _ := json.Marshal(plan)
	if strings.Contains(string(encoded), "schemaVersion") || strings.Contains(string(encoded), "manifestVersion") {
		t.Fatal("manifest metadata leaked into API configuration")
	}
}
func TestManifestCommandOfflineAndMetadataOnly(t *testing.T) {
	raw, _ := os.ReadFile("../../contracts/deployment-manifest-v1.example.json")
	file := manifestFile(t, raw)
	var output bytes.Buffer
	if err := manifestCommand([]string{"validate", "--file", file, "--json"}, &output); err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	if json.Unmarshal(output.Bytes(), &result) != nil || result["valid"] != true || result["schemaVersion"] != float64(1) {
		t.Fatal(output.String())
	}
	if strings.Contains(output.String(), "original.js") || strings.Contains(output.String(), "Command") {
		t.Fatal("command contents disclosed")
	}
}
func TestManifestInvalidBeforeAuthentication(t *testing.T) {
	file := manifestFile(t, []byte(`{"schemaVersion":2,"source":"source-secret"}`))
	t.Setenv("QUIKDB_TOKEN", "")
	t.Setenv("QUIKDB_FRAME_CONFIG_DIR", t.TempDir())
	err := Command([]string{"--repo", "https://github.com/quikdb/fixture", "--branch", "main", "--config", file, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "invalid deployment manifest") || strings.Contains(err.Error(), "source-secret") {
		t.Fatalf("wrong failure: %v", err)
	}
}
func TestLegacyExplicitConfigRemainsReadableWithoutV1(t *testing.T) {
	file := manifestFile(t, []byte(`{"appType":"python","startCommand":"python original.py","port":8123,"envVars":{"FIXTURE":"explicit"}}`))
	config, version, err := readDeploymentConfiguration(file, false)
	if err != nil || version != 0 || config["startCommand"] != "python original.py" || config["envVars"] == nil {
		t.Fatalf("legacy config changed: %v", err)
	}
	if _, _, err = readDeploymentConfiguration(file, true); err == nil {
		t.Fatal("legacy object reported as v1")
	}
}
func TestManifestInputBoundsAndSanitizedErrors(t *testing.T) {
	for _, raw := range [][]byte{[]byte("source-secret not JSON"), bytes.Repeat([]byte("x"), 65537), {0xff, 0xfe}, []byte("null"), []byte("[]")} {
		file := manifestFile(t, raw)
		_, _, err := readDeploymentConfiguration(file, false)
		if err == nil || strings.Contains(err.Error(), "source-secret") {
			t.Fatal("unsafe input accepted/disclosed")
		}
	}
	if _, _, err := readDeploymentConfiguration(t.TempDir(), false); err == nil {
		t.Fatal("directory input accepted")
	}
}
