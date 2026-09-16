package convert

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/quikdb/quikdb-frame/internal/project"
)

func TestExpressPilotPlansWithoutWritingAndPreservesContract(t *testing.T) {
	output := filepath.Join(t.TempDir(), "converted")
	plan, err := Run(Options{SourcePath: "testdata/express-static", Framework: "express", OutputPath: output})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("plan-only conversion wrote output: %v", err)
	}
	if plan.Converter != converterVersion || plan.Original.StartCommand != "npm start" || plan.Original.StartScript != "node server.js" || plan.Original.Port != 3187 || plan.Original.NodeVersion != "20" {
		t.Fatalf("original runtime contract not preserved: %+v", plan)
	}
	if len(plan.Routes) != 3 || len(plan.StaticMounts) != 1 || strings.Join(plan.Environment, ",") != "CATALOG_MODE,PORT" {
		t.Fatalf("unexpected conversion surface: %+v", plan)
	}
	if len(plan.SourceDigest) != 64 || strings.Contains(mustJSON(t, plan), "fixture\n") {
		t.Fatal("plan digest missing or environment values leaked")
	}
}

func TestExpressPilotApplyProducesReviewableFrameProjectAndRollback(t *testing.T) {
	output := filepath.Join(t.TempDir(), "converted")
	plan, err := Run(Options{SourcePath: "testdata/express-static", Framework: "express", OutputPath: output, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Load(filepath.Join(output, "quikdb.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := manifest.Services["api"]
	if manifest.Name != "express-static-fixture" || service.Port != 3187 || service.Build.Context != "." || service.Build.Target != "runtime" || strings.Join(service.Env, ",") != "CATALOG_MODE,PORT" {
		t.Fatalf("unexpected Frame manifest: %+v", manifest)
	}
	routes := readFile(t, filepath.Join(output, "services/api/routes.go"))
	if strings.Contains(routes, "TODO") || !strings.Contains(routes, `201`) || !strings.Contains(routes, `fixture-order`) {
		t.Fatalf("generated business contract is incomplete:\n%s", routes)
	}
	if got := readFile(t, filepath.Join(output, "services/api/assets/0/banner.txt")); got != "QuikDB conversion fixture asset\n" {
		t.Fatalf("asset changed: %q", got)
	}
	var saved Plan
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(output, "conversion/conversion-plan.json"))), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SourceDigest != plan.SourceDigest || saved.Output != "converted" {
		t.Fatalf("saved plan differs: %+v", saved)
	}
	var rollback map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(output, plan.Rollback.Manifest))), &rollback); err != nil {
		t.Fatal(err)
	}
	if rollback["startCommand"] != "npm start" || rollback["runtime"] != "nodejs" || rollback["runtimeVersion"] != "20" {
		t.Fatalf("rollback manifest lost original runtime: %+v", rollback)
	}
	if env := readFile(t, filepath.Join(output, ".env.example")); env != "CATALOG_MODE=\nPORT=\n" {
		t.Fatalf("environment values should not be copied: %q", env)
	}
}

func TestExpressPilotOutputIsDeterministic(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "converted")
	second := filepath.Join(root, "two", "converted")
	for _, output := range []string{first, second} {
		if _, err := Run(Options{SourcePath: "testdata/express-static", Framework: "express", OutputPath: output, Apply: true}); err != nil {
			t.Fatal(err)
		}
	}
	if a, b := treeDigest(t, first), treeDigest(t, second); a != b {
		t.Fatalf("same source produced different artifacts: %s != %s", a, b)
	}
}

func TestExpressPilotFailsClosedBeforeWriting(t *testing.T) {
	tests := map[string]string{
		"request-dependent handler": `app.get("/users", (req, res) => res.json({"id":req.query.id}));`,
		"dynamic JSON":              `app.get("/users", (_req, res) => res.json(buildResponse()));`,
		"custom middleware":         `app.use(authentication);`,
	}
	for name, statement := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, statement)
			output := filepath.Join(t.TempDir(), "converted")
			_, err := Run(Options{SourcePath: root, Framework: "express", OutputPath: output, Apply: true})
			if err == nil || !strings.Contains(err.Error(), "deploy the original application with --mode as-is") {
				t.Fatalf("unsafe source did not fail closed: %v", err)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("failed conversion left output: %v", statErr)
			}
		})
	}
}

func TestExpressPilotRejectsAdditionalLogicAndUnqualifiedFrameworks(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, `app.get("/", (_req, res) => res.send("ok"));`)
	if err := os.WriteFile(filepath.Join(root, "hidden-logic.js"), []byte("module.exports = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{SourcePath: root, Framework: "express"}); err == nil || !strings.Contains(err.Error(), "additional source file") {
		t.Fatalf("additional logic accepted: %v", err)
	}
	if _, err := Run(Options{SourcePath: root, Framework: "flask"}); err == nil || !strings.Contains(err.Error(), "not qualified") {
		t.Fatalf("unqualified framework accepted: %v", err)
	}
}

func TestConversionCommandRequiresExplicitApply(t *testing.T) {
	output := filepath.Join(t.TempDir(), "converted")
	var stdout bytes.Buffer
	if err := Command([]string{"testdata/express-static", "--from", "express", "--output", output, "--json"}, &stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("command wrote without --apply: %v", err)
	}
	var plan Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil || plan.Converter != converterVersion {
		t.Fatalf("invalid plan output: %v %+v", err, plan)
	}
	if err := Command([]string{"testdata/express-static", "--from", "express", "--output", output, "--apply"}, &stdout); err != nil {
		t.Fatal(err)
	}
	if err := Command([]string{"testdata/express-static", "--from", "express", "--output", output, "--apply"}, &stdout); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output was overwritten: %v", err)
	}
}

func writeFixture(t *testing.T, root, statement string) {
	t.Helper()
	pkg := `{"name":"unsafe-fixture","engines":{"node":"20"},"scripts":{"start":"node server.js"},"dependencies":{"express":"4.21.2"}}`
	entry := "const express = require(\"express\");\nconst app = express();\n" + statement + "\nconst port = process.env.PORT || 8080;\napp.listen(port);\n"
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(pkg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.js"), []byte(entry), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			rel, _ := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(path + "\x00"))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
