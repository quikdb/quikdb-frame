package convert

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxPackageBytes = 256 << 10
	maxLockBytes    = 4 << 20
	maxEntryBytes   = 1 << 20
	maxEnvBytes     = 64 << 10
	maxAssetBytes   = 10 << 20
	maxAssetsBytes  = 32 << 20
	maxAssetFiles   = 5000
)

var (
	startScriptPattern = regexp.MustCompile(`^node ([A-Za-z0-9][A-Za-z0-9._/-]*\.(?:js|cjs))$`)
	projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	envNamePattern     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	routePathPattern   = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)
	portPattern        = regexp.MustCompile(`^const\s+port\s*=\s*(?:Number\()?process\.env\.PORT\s*\|\|\s*([0-9]{1,5})\)?;$`)
	routePattern       = regexp.MustCompile(`^app\.(get|post|put|patch|delete)\(\s*"([^"]+)"\s*,\s*\(_req\s*,\s*res\)\s*=>\s*res(?:\.status\(([0-9]{3})\))?(?:\.type\("([^"]+)"\))?\.(json|send)\((.*)\)\s*\);$`)
	staticPattern      = regexp.MustCompile(`^app\.use\(\s*"([^"]+)"\s*,\s*express\.static\("([^"]+)"\)\s*\);$`)
)

type expressPackage struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Scripts      map[string]string `json:"scripts"`
	Dependencies map[string]string `json:"dependencies"`
	Engines      map[string]string `json:"engines"`
}

type expressAnalysis struct {
	plan         Plan
	projectName  string
	entry        string
	assetSources []assetSource
}

type assetSource struct {
	prefix    string
	directory string
	files     []SourceFile
}

func analyzeExpress(source string) (expressAnalysis, error) {
	packageData, packageFile, err := readRegularFile(source, "package.json", maxPackageBytes)
	if err != nil {
		return expressAnalysis{}, fmt.Errorf("Express pilot requires package.json: %w", err)
	}
	var pkg expressPackage
	if err := json.Unmarshal(packageData, &pkg); err != nil {
		return expressAnalysis{}, fmt.Errorf("package.json must be valid JSON")
	}
	if pkg.Type != "" && pkg.Type != "commonjs" {
		return expressAnalysis{}, unsupported("only CommonJS Express entrypoints are supported")
	}
	if _, ok := pkg.Dependencies["express"]; !ok {
		return expressAnalysis{}, unsupported("package.json must declare express as a production dependency")
	}
	if pkg.Dependencies["express"] != "4.21.2" {
		return expressAnalysis{}, unsupported("the pilot supports exactly express 4.21.2")
	}
	if strings.TrimSpace(pkg.Engines["node"]) != "20" {
		return expressAnalysis{}, unsupported("the pilot requires engines.node to be exactly 20")
	}
	for dependency := range pkg.Dependencies {
		if dependency != "express" {
			return expressAnalysis{}, unsupported("additional production dependency %q is not proven equivalent", dependency)
		}
	}
	for _, hook := range []string{"prestart", "poststart"} {
		if pkg.Scripts[hook] != "" {
			return expressAnalysis{}, unsupported("npm %s lifecycle hooks are not supported", hook)
		}
	}
	startScript := strings.TrimSpace(pkg.Scripts["start"])
	match := startScriptPattern.FindStringSubmatch(startScript)
	if match == nil {
		return expressAnalysis{}, unsupported("start script must be exactly node <relative .js or .cjs entry>")
	}
	entry := filepath.ToSlash(filepath.Clean(match[1]))
	if entry == ".." || strings.HasPrefix(entry, "../") {
		return expressAnalysis{}, unsupported("start entry must stay inside the project")
	}
	entryData, entryFile, err := readRegularFile(source, entry, maxEntryBytes)
	if err != nil {
		return expressAnalysis{}, err
	}
	if !utf8.Valid(entryData) {
		return expressAnalysis{}, unsupported("entrypoint must be UTF-8 source")
	}
	routes, mounts, port, err := parseExpressEntry(entry, string(entryData))
	if err != nil {
		return expressAnalysis{}, err
	}
	if err := rejectAdditionalCode(source, entry, mounts); err != nil {
		return expressAnalysis{}, err
	}
	if len(routes) == 0 {
		return expressAnalysis{}, unsupported("at least one static response route is required")
	}

	environment, envFile, err := readEnvironmentDeclarations(source)
	if err != nil {
		return expressAnalysis{}, err
	}
	environment = sortedUnique(append(environment, "PORT"))
	files := []SourceFile{packageFile, entryFile}
	if _, err := os.Lstat(filepath.Join(source, "package-lock.json")); err == nil {
		_, lockFile, err := readRegularFile(source, "package-lock.json", maxLockBytes)
		if err != nil {
			return expressAnalysis{}, err
		}
		files = append(files, lockFile)
	} else if !os.IsNotExist(err) {
		return expressAnalysis{}, fmt.Errorf("inspect package-lock.json: %w", err)
	}
	if envFile != nil {
		files = append(files, *envFile)
	}
	staticMounts := make([]StaticMount, 0, len(mounts))
	assetSources := make([]assetSource, 0, len(mounts))
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].prefix < mounts[j].prefix })
	for _, mount := range mounts {
		assetFiles, err := scanAssets(source, mount.directory)
		if err != nil {
			return expressAnalysis{}, err
		}
		files = append(files, assetFiles...)
		staticMounts = append(staticMounts, StaticMount{Prefix: mount.prefix, Directory: mount.directory, Files: assetFiles})
		assetSources = append(assetSources, assetSource{prefix: mount.prefix, directory: mount.directory, files: assetFiles})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
	sort.Slice(staticMounts, func(i, j int) bool { return staticMounts[i].Prefix < staticMounts[j].Prefix })

	name := normalizedProjectName(pkg.Name, filepath.Base(source))
	installCommand := "npm install --omit=dev"
	if info, err := os.Lstat(filepath.Join(source, "package-lock.json")); err == nil && info.Mode().IsRegular() {
		installCommand = "npm ci --omit=dev"
	}
	nodeVersion := "20"
	plan := Plan{
		SchemaVersion: 1,
		Converter:     converterVersion,
		Framework:     "express",
		ProjectName:   name,
		SourceFiles:   files,
		Original: OriginalRuntime{Entry: entry, StartCommand: "npm start", StartScript: startScript,
			InstallCommand: installCommand, Port: port, NodeVersion: nodeVersion},
		Converted: FrameRuntime{Language: "go", StartCommand: "/app", DockerTarget: "runtime",
			BusinessParity: "static route status, content type and body plus declared static asset bytes"},
		Routes:       routes,
		StaticMounts: staticMounts,
		Environment:  environment,
		Rollback: Rollback{Mode: "as-is", Manifest: "conversion/as-is-quikdb.json",
			SourceAuthority: "unchanged original source directory"},
		Limitations: []string{
			"Only constant JSON or string response handlers are converted.",
			"Request-dependent logic, middleware, databases, templates, WebSockets and custom headers are rejected.",
			"Parity covers status, content type and response bytes for declared routes and static asset bytes; transport-generated headers are not claimed.",
		},
	}
	plan.SourceDigest = sourceDigest(files)
	return expressAnalysis{plan: plan, projectName: name, entry: entry, assetSources: assetSources}, nil
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("Express conversion not safe: %s; deploy the original application with --mode as-is", fmt.Sprintf(format, args...))
}

func normalizedProjectName(packageName, fallback string) string {
	name := strings.ToLower(strings.TrimSpace(packageName))
	if strings.HasPrefix(name, "@") && strings.Contains(name, "/") {
		name = strings.SplitN(name, "/", 2)[1]
	}
	name = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(fallback), "-")
		name = strings.Trim(name, "-")
	}
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if !projectNamePattern.MatchString(name) {
		return "converted-express-app"
	}
	return name
}

type parsedMount struct{ prefix, directory string }

func parseExpressEntry(entry, content string) ([]Route, []parsedMount, int, error) {
	var routes []Route
	var mounts []parsedMount
	port := 0
	seenRoute := map[string]bool{}
	seenMount := map[string]bool{}
	seenExpress, seenApp, seenListen := false, false, false
	for index, raw := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		lineNumber := index + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if seenListen {
			return nil, nil, 0, unsupported("statements after app.listen are not supported at %s:%d", entry, lineNumber)
		}
		switch line {
		case `const express = require("express");`:
			if seenExpress {
				return nil, nil, 0, unsupported("duplicate Express import at %s:%d", entry, lineNumber)
			}
			seenExpress = true
			continue
		case `const app = express();`:
			if !seenExpress || seenApp {
				return nil, nil, 0, unsupported("app initialization order at %s:%d is ambiguous", entry, lineNumber)
			}
			seenApp = true
			continue
		case `app.listen(port);`:
			if !seenApp || port == 0 || seenListen {
				return nil, nil, 0, unsupported("listener must follow one supported PORT declaration at %s:%d", entry, lineNumber)
			}
			seenListen = true
			continue
		}
		if match := portPattern.FindStringSubmatch(line); match != nil {
			if port != 0 {
				return nil, nil, 0, unsupported("multiple port declarations at %s:%d", entry, lineNumber)
			}
			parsed, _ := strconv.Atoi(match[1])
			if parsed < 1 || parsed > 65535 {
				return nil, nil, 0, unsupported("port must be between 1 and 65535 at %s:%d", entry, lineNumber)
			}
			port = parsed
			continue
		}
		if match := staticPattern.FindStringSubmatch(line); match != nil {
			if !seenApp {
				return nil, nil, 0, unsupported("static mount before app initialization at %s:%d", entry, lineNumber)
			}
			prefix, directory := match[1], filepath.ToSlash(filepath.Clean(match[2]))
			if !routePathPattern.MatchString(prefix) || prefix == "/" || strings.HasSuffix(prefix, "/") {
				return nil, nil, 0, unsupported("static mount prefix must be a fixed non-root path without a trailing slash at %s:%d", entry, lineNumber)
			}
			if directory == "." || directory == ".." || strings.HasPrefix(directory, "../") || filepath.IsAbs(directory) {
				return nil, nil, 0, unsupported("static directory must stay inside the project at %s:%d", entry, lineNumber)
			}
			if seenMount[prefix] {
				return nil, nil, 0, unsupported("duplicate static mount %s", prefix)
			}
			seenMount[prefix] = true
			mounts = append(mounts, parsedMount{prefix: prefix, directory: directory})
			continue
		}
		if match := routePattern.FindStringSubmatch(line); match != nil {
			if !seenApp {
				return nil, nil, 0, unsupported("route before app initialization at %s:%d", entry, lineNumber)
			}
			method, path := strings.ToUpper(match[1]), match[2]
			if !routePathPattern.MatchString(path) || strings.Contains(path, "//") || (strings.HasSuffix(path, "/") && path != "/") {
				return nil, nil, 0, unsupported("route %q is outside the fixed-path pilot", path)
			}
			key := method + " " + path
			if seenRoute[key] {
				return nil, nil, 0, unsupported("duplicate route %s", key)
			}
			status := 200
			if match[3] != "" {
				status, _ = strconv.Atoi(match[3])
				if status < 100 || status > 599 {
					return nil, nil, 0, unsupported("invalid response status at %s:%d", entry, lineNumber)
				}
			}
			contentType, body := match[4], match[6]
			if match[5] == "json" {
				if contentType != "" {
					return nil, nil, 0, unsupported("json routes cannot override content type in the pilot")
				}
				compacted, compactErr := compactJSON(body)
				if compactErr != nil {
					return nil, nil, 0, unsupported("route JSON must be a literal at %s:%d", entry, lineNumber)
				}
				body = compacted
				contentType = "application/json; charset=utf-8"
			} else {
				if !strings.HasPrefix(body, `"`) || !strings.HasSuffix(body, `"`) {
					return nil, nil, 0, unsupported("send response must be a double-quoted string literal at %s:%d", entry, lineNumber)
				}
				decoded, err := strconv.Unquote(body)
				if err != nil {
					return nil, nil, 0, unsupported("invalid response string at %s:%d", entry, lineNumber)
				}
				body = decoded
				if contentType == "" {
					contentType = "text/html; charset=utf-8"
				} else if contentType == "text/plain" || contentType == "text/html" {
					contentType += "; charset=utf-8"
				} else {
					return nil, nil, 0, unsupported("only text/plain and text/html send types are supported")
				}
			}
			seenRoute[key] = true
			routes = append(routes, Route{Method: method, Path: path, Status: status, ContentType: contentType, Body: body, Source: fmt.Sprintf("%s:%d", entry, lineNumber)})
			continue
		}
		return nil, nil, 0, unsupported("unrecognized or dynamic statement at %s:%d", entry, lineNumber)
	}
	if !seenExpress || !seenApp || !seenListen || port == 0 {
		return nil, nil, 0, unsupported("entrypoint must import Express, initialize app, declare PORT fallback and call app.listen(port)")
	}
	for _, route := range routes {
		for _, mount := range mounts {
			if route.Path == mount.prefix || strings.HasPrefix(route.Path, mount.prefix+"/") {
				return nil, nil, 0, unsupported("route %s overlaps static mount %s", route.Path, mount.prefix)
			}
		}
	}
	return routes, mounts, port, nil
}

func rejectAdditionalCode(source, entry string, mounts []parsedMount) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if rel == "node_modules" || rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return unsupported("special file %s is not supported", rel)
		}
		ext := strings.ToLower(filepath.Ext(rel))
		isAsset := false
		for _, mount := range mounts {
			if rel == mount.directory || strings.HasPrefix(rel, mount.directory+"/") {
				isAsset = true
				break
			}
		}
		if (ext == ".js" || ext == ".cjs" || ext == ".mjs" || ext == ".jsx" || ext == ".ts" || ext == ".tsx") && rel != entry && !isAsset {
			return unsupported("additional source file %s could contain unconverted logic", rel)
		}
		return nil
	})
}

func readEnvironmentDeclarations(source string) ([]string, *SourceFile, error) {
	path := filepath.Join(source, ".env.example")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, unsupported(".env.example must be a regular file")
	}
	data, file, err := readRegularFile(source, ".env.example", maxEnvBytes)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for index, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || !envNamePattern.MatchString(parts[0]) {
			return nil, nil, unsupported("invalid environment declaration at .env.example:%d", index+1)
		}
		names = append(names, parts[0])
	}
	return sortedUnique(names), &file, nil
}

func scanAssets(source, directory string) ([]SourceFile, error) {
	root := filepath.Join(source, filepath.FromSlash(directory))
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return nil, unsupported("static directory %s must exist", directory)
	}
	var files []SourceFile
	var total int64
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relAsset, _ := filepath.Rel(root, path)
		for _, segment := range strings.Split(filepath.ToSlash(relAsset), "/") {
			if strings.HasPrefix(segment, ".") || strings.HasPrefix(segment, "_") {
				return unsupported("hidden or underscore-prefixed asset %s is not supported", relAsset)
			}
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return unsupported("asset %s must be a regular file", relAsset)
		}
		if len(files) >= maxAssetFiles || info.Size() > maxAssetBytes || total+info.Size() > maxAssetsBytes {
			return unsupported("static assets exceed pilot limits")
		}
		relSource, _ := filepath.Rel(source, path)
		_, file, err := readRegularFile(source, filepath.ToSlash(relSource), maxAssetBytes)
		if err != nil {
			return err
		}
		total += info.Size()
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, unsupported("static directory %s must contain at least one file", directory)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
