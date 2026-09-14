package deploy

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const manifestLimit = 65536

var manifestRuntimes = map[string]bool{"nodejs": true, "python": true, "go": true, "bun": true, "deno": true, "ruby": true, "java": true, "dotnet": true, "php": true, "rust": true, "static": true}
var manifestFields = map[string]bool{"schemaVersion": true, "runtime": true, "framework": true, "installCommand": true, "buildCommand": true, "startCommand": true, "port": true, "runtimeVersion": true, "nodeVersion": true, "healthCheck": true}

func validManifestText(value interface{}, limit int) bool {
	s, ok := value.(string)
	return ok && utf8.RuneCountInString(s) <= limit && !strings.ContainsRune(s, 0)
}
func manifestInteger(value interface{}, minimum, maximum float64) bool {
	n, ok := value.(float64)
	return ok && !math.IsNaN(n) && n >= minimum && n <= maximum && math.Trunc(n) == n
}

// validateManifestV1 does not authenticate or grant access. Unknown fields and versions fail closed.
func validateManifestV1(manifest map[string]interface{}) error {
	invalid := func() error { return fmt.Errorf("invalid deployment manifest v1; check the published schema") }
	if manifest == nil || manifest["schemaVersion"] != float64(1) {
		return invalid()
	}
	for key := range manifest {
		if !manifestFields[key] {
			return invalid()
		}
	}
	runtime, ok := manifest["runtime"].(string)
	if !ok || !manifestRuntimes[runtime] {
		return invalid()
	}
	for _, key := range []string{"installCommand", "buildCommand", "startCommand"} {
		if !validManifestText(manifest[key], 4096) {
			return invalid()
		}
	}
	if strings.TrimFunc(manifest["startCommand"].(string), func(r rune) bool { return (unicode.IsSpace(r) && r != 0x85) || r == 0xfeff }) == "" || !manifestInteger(manifest["port"], 1, 65535) {
		return invalid()
	}
	if framework, exists := manifest["framework"]; exists && !validManifestText(framework, 256) {
		return invalid()
	}
	version, hasVersion := manifest["runtimeVersion"]
	if hasVersion && (!validManifestText(version, 32) || !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}$`).MatchString(version.(string))) {
		return invalid()
	}
	if runtime == "nodejs" && hasVersion && !regexp.MustCompile(`^[1-9][0-9]?$`).MatchString(version.(string)) {
		return invalid()
	}
	if node, exists := manifest["nodeVersion"]; exists {
		if runtime != "nodejs" || !manifestInteger(node, 1, 99) {
			return invalid()
		}
		if hasVersion && fmt.Sprintf("%.0f", node) != version {
			return invalid()
		}
	}
	if health, exists := manifest["healthCheck"]; exists {
		if !validManifestText(health, 2048) {
			return invalid()
		}
		s := health.(string)
		if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.Contains(s, "\\") {
			return invalid()
		}
		for _, r := range s {
			if r < 32 || r == 127 {
				return invalid()
			}
		}
	}
	return nil
}

// manifestConfiguration normalizes only declared v1 defaults; original commands stay byte-for-byte.
func manifestConfiguration(manifest map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range manifest {
		if key != "schemaVersion" && key != "runtime" {
			result[key] = value
		}
	}
	result["appType"] = manifest["runtime"]
	result["configSource"] = "quikdb.json"
	if _, exists := result["framework"]; !exists {
		result["framework"] = ""
	}
	if _, exists := result["healthCheck"]; !exists {
		result["healthCheck"] = "/"
	}
	if manifest["runtime"] == "nodejs" {
		if version, exists := result["runtimeVersion"]; exists {
			var major float64
			if json.Unmarshal([]byte(version.(string)), &major) == nil {
				result["nodeVersion"] = major
			}
		} else if node, exists := result["nodeVersion"]; exists {
			result["runtimeVersion"] = fmt.Sprintf("%.0f", node)
		}
	}
	return result
}

func readDeploymentConfiguration(file string, requireV1 bool) (map[string]interface{}, int, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, 0, fmt.Errorf("cannot read deployment configuration file")
	}
	if !info.Mode().IsRegular() || info.Size() > manifestLimit {
		return nil, 0, fmt.Errorf("deployment configuration must be a regular file no larger than 64 KiB")
	}
	input, err := os.Open(file)
	if err != nil {
		return nil, 0, fmt.Errorf("cannot read deployment configuration file")
	}
	defer input.Close()
	raw, err := readBounded(input, manifestLimit)
	if err != nil {
		return nil, 0, fmt.Errorf("deployment configuration exceeds size limit or cannot be read")
	}
	if !utf8.Valid(raw) {
		return nil, 0, fmt.Errorf("deployment configuration must be UTF-8 JSON")
	}
	var manifest map[string]interface{}
	if json.Unmarshal(raw, &manifest) != nil || manifest == nil {
		return nil, 0, fmt.Errorf("deployment configuration must be a JSON object")
	}
	_, versioned := manifest["schemaVersion"]
	if versioned || requireV1 {
		if err := validateManifestV1(manifest); err != nil {
			return nil, 0, err
		}
		return manifestConfiguration(manifest), 1, nil
	}
	// Unversioned --config keeps the existing explicit Compute configuration contract.
	return manifest, 0, nil
}

func ManifestCommand(args []string) error { return manifestCommand(args, os.Stdout) }
func manifestCommand(args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "validate" {
		return fmt.Errorf("usage: quikdb-frame manifest validate --file quikdb.json [--json]")
	}
	flags := flag.NewFlagSet("manifest validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "quikdb.json", "Versioned deployment manifest file")
	jsonOutput := flags.Bool("json", false, "Metadata-only validation result")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("invalid manifest validation arguments")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected manifest validation arguments")
	}
	configuration, version, err := readDeploymentConfiguration(*file, true)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(output).Encode(map[string]interface{}{"valid": true, "schemaVersion": version, "runtime": configuration["appType"], "port": configuration["port"], "mode": "as-is"})
	}
	_, err = fmt.Fprintln(output, "Deployment manifest v1 valid. Runtime and deployment compatibility require separate qualification.")
	return err
}
