package convert

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quikdb/quikdb-frame/internal/project"
)

func writeConvertedProject(output, source string, analysis expressAnalysis, plan Plan) error {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create conversion parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(output)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary conversion output: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()

	for _, directory := range []string{"services/api", "conversion"} {
		if err := os.MkdirAll(filepath.Join(temporary, directory), 0755); err != nil {
			return fmt.Errorf("create conversion directory: %w", err)
		}
	}
	for index, mount := range analysis.assetSources {
		targetRoot := filepath.Join(temporary, "services/api/assets", strconv.Itoa(index))
		for _, file := range mount.files {
			rel, _ := filepath.Rel(filepath.FromSlash(mount.directory), filepath.FromSlash(file.Path))
			target := filepath.Join(targetRoot, rel)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			data, _, err := readRegularFile(source, file.Path, maxAssetBytes)
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0644); err != nil {
				return fmt.Errorf("copy static asset %s: %w", file.Path, err)
			}
		}
	}

	manifest := project.Manifest{
		SchemaVersion: project.CurrentSchemaVersion,
		Name:          analysis.projectName,
		Version:       "1.0.0",
		GoModule:      analysis.projectName,
		Services: map[string]project.Service{
			"api": {Type: "api", Path: "services/api", Build: project.Build{Context: ".", Dockerfile: "services/api/Dockerfile", Target: "runtime"},
				Port: analysis.plan.Original.Port, Routes: []string{"/*"}, Env: analysis.plan.Environment},
		},
		Routing: project.Routing{Rules: []project.RoutingRule{{Path: "/*", Service: "api"}}},
	}
	if err := project.Save(filepath.Join(temporary, "quikdb.yaml"), manifest); err != nil {
		return fmt.Errorf("write project manifest: %w", err)
	}
	files := map[string]string{
		"go.mod":                  fmt.Sprintf("module %s\n\ngo 1.24\n", analysis.projectName),
		".gitignore":              ".env\n*.exe\n/app\ncoverage.out\n",
		".dockerignore":           ".git\n.env\n.env.*\nconversion\n",
		".env.example":            environmentExample(analysis.plan.Environment),
		"services/api/main.go":    generatedMain(analysis.plan.Original.Port),
		"services/api/routes.go":  generatedRoutes(analysis.plan.Routes, analysis.plan.StaticMounts),
		"services/api/Dockerfile": generatedDockerfile(analysis.plan.Original.Port),
		"README.md":               generatedReadme(analysis.plan),
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(temporary, path)), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(temporary, path), []byte(content), 0644); err != nil {
			return fmt.Errorf("write generated file %s: %w", path, err)
		}
	}
	asIs := map[string]any{
		"schemaVersion": 1, "runtime": "nodejs", "framework": "express",
		"installCommand": analysis.plan.Original.InstallCommand, "buildCommand": "",
		"startCommand": analysis.plan.Original.StartCommand, "port": analysis.plan.Original.Port,
		"healthCheck": healthPath(analysis.plan.Routes),
	}
	if analysis.plan.Original.NodeVersion != "" {
		asIs["runtimeVersion"] = analysis.plan.Original.NodeVersion
	}
	asIsData, _ := json.MarshalIndent(asIs, "", "  ")
	if err := os.WriteFile(filepath.Join(temporary, analysis.plan.Rollback.Manifest), append(asIsData, '\n'), 0644); err != nil {
		return err
	}
	planData, err := encodePlan(plan)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(temporary, "conversion/conversion-plan.json"), planData, 0644); err != nil {
		return err
	}
	if err := os.Rename(temporary, output); err != nil {
		return fmt.Errorf("commit converted project: %w", err)
	}
	committed = true
	return nil
}

func environmentExample(environment []string) string {
	var result strings.Builder
	for _, name := range environment {
		result.WriteString(name)
		result.WriteString("=\n")
	}
	return result.String()
}

func generatedMain(port int) string {
	return fmt.Sprintf(`package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "%d"
	}
	server := &http.Server{Addr: ":" + port, Handler: routes(), ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("application listening on port %%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("application server stopped")
	}
}
`, port)
}

func generatedRoutes(routes []Route, mounts []StaticMount) string {
	imports := `import "net/http"`
	if len(mounts) > 0 {
		imports = `import (
	"embed"
	"io/fs"
	"net/http"
)`
	}
	var out strings.Builder
	out.WriteString("package main\n\n")
	out.WriteString(imports)
	out.WriteString("\n\n")
	if len(mounts) > 0 {
		out.WriteString("//go:embed assets\nvar staticAssets embed.FS\n\n")
	}
	out.WriteString("func routes() http.Handler {\n\tmux := http.NewServeMux()\n")
	for _, route := range routes {
		fmt.Fprintf(&out, "\tmux.HandleFunc(%q, func(w http.ResponseWriter, _ *http.Request) {\n", route.Method+" "+route.Path)
		fmt.Fprintf(&out, "\t\tw.Header().Set(\"Content-Type\", %q)\n", route.ContentType)
		fmt.Fprintf(&out, "\t\tw.WriteHeader(%d)\n", route.Status)
		fmt.Fprintf(&out, "\t\t_, _ = w.Write([]byte(%q))\n\t})\n", route.Body)
	}
	for index, mount := range mounts {
		fmt.Fprintf(&out, "\tassetRoot%d, err := fs.Sub(staticAssets, %q)\n", index, "assets/"+strconv.Itoa(index))
		out.WriteString("\tif err != nil { panic(\"embedded asset contract failed\") }\n")
		fmt.Fprintf(&out, "\tmux.Handle(%q, http.StripPrefix(%q, http.FileServer(http.FS(assetRoot%d))))\n", mount.Prefix+"/", mount.Prefix, index)
	}
	out.WriteString("\treturn mux\n}\n")
	return out.String()
}

func generatedDockerfile(port int) string {
	return fmt.Sprintf(`FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY services/api ./services/api
RUN mkdir -p /out && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/app ./services/api

FROM scratch AS runtime
COPY --from=builder /out/app /app
EXPOSE %d
ENTRYPOINT ["/app"]
`, port)
}

func generatedReadme(plan Plan) string {
	return fmt.Sprintf("# %s\n\nThis is a bounded QuikDB Frame conversion produced by %s.\n\nReview `conversion/conversion-plan.json` before using it. The converter proved only the fixed routes and static files listed there. It did not infer or replace dynamic business logic.\n\nThe original source was not modified. To keep using it, deploy that original directory as-is with `conversion/as-is-quikdb.json` as its explicit deployment configuration. Runtime environment values are intentionally absent from both artifacts and must be configured separately.\n", plan.ProjectName, plan.Converter)
}

func healthPath(routes []Route) string {
	for _, route := range routes {
		if route.Method == "GET" && (route.Path == "/health" || route.Path == "/") && route.Status >= 200 && route.Status < 400 {
			return route.Path
		}
	}
	return "/"
}
