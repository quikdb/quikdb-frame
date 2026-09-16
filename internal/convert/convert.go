// Package convert implements deliberately bounded source-to-Frame converters.
// A converter must reject source it cannot prove equivalent; it must never emit
// placeholder handlers or claim support based only on route discovery.
package convert

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const converterVersion = "express-static-v1"

type Options struct {
	SourcePath string
	Framework  string
	OutputPath string
	Apply      bool
}

type Plan struct {
	SchemaVersion int             `json:"schemaVersion"`
	Converter     string          `json:"converter"`
	Framework     string          `json:"framework"`
	ProjectName   string          `json:"projectName"`
	SourceDigest  string          `json:"sourceDigest"`
	SourceFiles   []SourceFile    `json:"sourceFiles"`
	Original      OriginalRuntime `json:"original"`
	Converted     FrameRuntime    `json:"converted"`
	Routes        []Route         `json:"routes"`
	StaticMounts  []StaticMount   `json:"staticMounts"`
	Environment   []string        `json:"environment"`
	Rollback      Rollback        `json:"rollback"`
	Limitations   []string        `json:"limitations"`
	Output        string          `json:"output,omitempty"`
}

type SourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type OriginalRuntime struct {
	Entry          string `json:"entry"`
	StartCommand   string `json:"startCommand"`
	StartScript    string `json:"startScript"`
	InstallCommand string `json:"installCommand"`
	Port           int    `json:"port"`
	NodeVersion    string `json:"nodeVersion,omitempty"`
}

type FrameRuntime struct {
	Language       string `json:"language"`
	StartCommand   string `json:"startCommand"`
	DockerTarget   string `json:"dockerTarget"`
	BusinessParity string `json:"businessParity"`
}

type Rollback struct {
	Mode            string `json:"mode"`
	Manifest        string `json:"manifest"`
	SourceAuthority string `json:"sourceAuthority"`
}

type Route struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	Source      string `json:"source"`
}

type StaticMount struct {
	Prefix    string       `json:"prefix"`
	Directory string       `json:"directory"`
	Files     []SourceFile `json:"files"`
}

// Command parses the public CLI contract. Planning is the default and has no
// filesystem side effects; --apply is required to create a converted project.
func Command(args []string, output io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quikdb-frame convert <path> --from express [--output <path>] [--apply] [--json]")
	}
	opts := Options{SourcePath: args[0]}
	jsonOutput := false
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if seen[arg] {
			return fmt.Errorf("duplicate conversion option %s", arg)
		}
		switch arg {
		case "--from", "--output":
			seen[arg] = true
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return fmt.Errorf("%s requires a value", arg)
			}
			i++
			if arg == "--from" {
				opts.Framework = args[i]
			} else {
				opts.OutputPath = args[i]
			}
		case "--apply":
			seen[arg] = true
			opts.Apply = true
		case "--json":
			seen[arg] = true
			jsonOutput = true
		default:
			return fmt.Errorf("unexpected conversion argument %q", arg)
		}
	}
	if opts.Framework == "" {
		return fmt.Errorf("--from is required; the only qualified pilot is express")
	}
	plan, err := Run(opts)
	if err != nil {
		return err
	}
	if jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	action := "Plan ready; no files were written. Review it, then rerun with --apply."
	if opts.Apply {
		action = "Converted project written. Review conversion/conversion-plan.json and run the parity checks before deployment."
	}
	_, err = fmt.Fprintf(output, "%s\nFramework: %s (%s)\nRoutes: %d\nSource digest: %s\nOutput: %s\nRollback: deploy the unchanged original source with %s\n",
		action, plan.Framework, plan.Converter, len(plan.Routes), plan.SourceDigest, plan.Output, plan.Rollback.Manifest)
	return err
}

// Run validates the complete supported source subset before it writes output.
func Run(options Options) (Plan, error) {
	framework := strings.ToLower(strings.TrimSpace(options.Framework))
	if framework != "express" {
		if framework == "" {
			framework = "unspecified"
		}
		return Plan{}, fmt.Errorf("%s conversion is not qualified; use --mode as-is", framework)
	}
	source, err := canonicalDirectory(options.SourcePath)
	if err != nil {
		return Plan{}, err
	}
	output := options.OutputPath
	if output == "" {
		output = source + "-quikdb"
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve conversion output: %w", err)
	}
	if insidePath(source, output) {
		return Plan{}, fmt.Errorf("conversion output must be outside the original source directory")
	}

	analysis, err := analyzeExpress(source)
	if err != nil {
		return Plan{}, err
	}
	plan := analysis.plan
	plan.Output = filepath.Base(filepath.Clean(output))
	if !options.Apply {
		return plan, nil
	}
	if _, err := os.Lstat(output); err == nil {
		return Plan{}, fmt.Errorf("conversion output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return Plan{}, fmt.Errorf("inspect conversion output: %w", err)
	}
	if err := writeConvertedProject(output, source, analysis, plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func canonicalDirectory(input string) (string, error) {
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve source directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve source directory: %w", err)
	}
	info, err := os.Lstat(real)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("source must be an existing directory")
	}
	return real, nil
}

func insidePath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func encodePlan(plan Plan) ([]byte, error) {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func compactJSON(raw string) (string, error) {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := json.Compact(&output, []byte(raw)); err != nil {
		return "", err
	}
	return output.String(), nil
}

func sourceDigest(files []SourceFile) string {
	h := sha256.New()
	for _, file := range files {
		fmt.Fprintf(h, "%s\x00%s\x00%d\n", file.Path, file.SHA256, file.Bytes)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func readRegularFile(root, relative string, limit int64) ([]byte, SourceFile, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, SourceFile{}, fmt.Errorf("unsafe source path %q", relative)
	}
	path := filepath.Join(root, clean)
	if !insidePath(root, path) {
		return nil, SourceFile{}, fmt.Errorf("source path escapes project root: %q", relative)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, SourceFile{}, fmt.Errorf("read %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, SourceFile{}, fmt.Errorf("%s must be a regular file no larger than %d bytes", relative, limit)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, SourceFile{}, fmt.Errorf("read %s: %w", relative, err)
	}
	sum := sha256.Sum256(data)
	return data, SourceFile{Path: filepath.ToSlash(clean), SHA256: hex.EncodeToString(sum[:]), Bytes: info.Size()}, nil
}

func sortedUnique(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
