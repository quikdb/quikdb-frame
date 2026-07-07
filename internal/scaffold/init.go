package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

func Init(name, dbType string) error {
	if err := validateName(name, "project name"); err != nil {
		return err
	}

	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %s already exists", name)
	}

	fmt.Printf("Creating project: %s (database: %s)\n\n", name, dbType)

	dirs := []string{
		"shared/db",
		"shared/auth",
		"shared/types",
		"shared/queue",
		"shared/cache",
		"shared/notify",
		"shared/logging",
		"shared/payments",
		"shared/storage",
		"shared/messaging",
		"services/api",
		"services/web/src/pages",
		"services/web/src/components",
		"config",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(name, dir), 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	files := map[string]func(string, string) string{
		"quikdb.yaml":                       quikdbYaml,
		".env.example":                      envExample,
		".gitignore":                        gitignore,
		"services/api/main.go":              apiMainGo,
		"services/api/routes.go":            apiRoutesGo,
		"services/api/health.go":            apiHealthGo,
		"services/api/hello.go":             apiHelloGo,
		"services/api/Dockerfile":           apiDockerfile,
		"services/api/quikdb.json":          apiQuikdbJson,
		"services/api/go.mod":               apiGoMod,
		"services/web/server.go":            webServerGo,
		"services/web/go.mod":               webGoMod,
		"services/web/index.html":           webIndexHtml,
		"services/web/package.json":         webPackageJson,
		"services/web/vite.config.ts":       webViteConfig,
		"services/web/src/index.tsx":        webIndexTsx,
		"services/web/src/app.tsx":          webAppTsx,
		"services/web/Dockerfile":           webDockerfile,
		"services/web/quikdb.json":          webQuikdbJson,
		"shared/db/database.go":             sharedDbGo,
		"shared/auth/jwt.go":               sharedJwtGo,
		"shared/auth/middleware.go":         sharedMiddlewareGo,
		"shared/logging/logger.go":          sharedLoggerGo,
		"shared/types/user.go":             sharedUserType,
		"config/database.yaml":             configDatabase,
		"config/ratelimit.yaml":            configRatelimit,
		"CLAUDE.md":                         claudeMd,
		".cursorrules":                      cursorrules,
		".github/copilot-instructions.md":   copilotInstructions,
	}

	baseName := filepath.Base(name)
	for path, fn := range files {
		fullPath := filepath.Join(name, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		content := fn(baseName, dbType)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	fmt.Printf("  Created %s/\n", name)
	fmt.Printf("  ├── quikdb.yaml\n")
	fmt.Printf("  ├── shared/          (auth, db, types, logging)\n")
	fmt.Printf("  ├── services/\n")
	fmt.Printf("  │   ├── api/         (Go REST API)\n")
	fmt.Printf("  │   └── web/         (Preact + Go file server)\n")
	fmt.Printf("  ├── config/          (database, ratelimit)\n")
	fmt.Printf("  ├── CLAUDE.md        (AI instructions)\n")
	fmt.Printf("  └── .cursorrules\n")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", name)
	fmt.Println("  quikdb-frame dev          # run locally")
	fmt.Println("  quikdb-frame add api auth # add more services")
	fmt.Println("  quikdb-frame deploy       # deploy to QuikDB")
	fmt.Println()

	return nil
}
