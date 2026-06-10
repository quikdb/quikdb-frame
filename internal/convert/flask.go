package convert

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func convertFlask(srcPath string) error {
	fmt.Println("Scanning Flask project...")
	fmt.Println()

	scan := scanFlask(srcPath)

	fmt.Printf("Found: %d routes, %d middleware, %d models, %d env vars\n",
		len(scan.routes), len(scan.middleware), len(scan.models), len(scan.envVars))
	fmt.Println()

	outPath := srcPath + "-quikdb"
	if err := generateFromScan(outPath, scan); err != nil {
		return err
	}

	// Patch the CLAUDE.md to say Flask instead of Express
	claudeMd := fmt.Sprintf(`# %s — converted to quikdb-frame

## Original framework: Flask
## Converted routes: %d
## Models: %s

## Architecture
Single api service with all routes. Split into multiple services as needed.

## Strict rules
- All Go services: CGO_ENABLED=0, single static binary, scratch Docker image
- All services read PORT from environment
- GET /health returns JSON with status
- NO node_modules in production
- Graceful shutdown on SIGTERM
`, filepath.Base(strings.TrimSuffix(outPath, "-quikdb")), len(scan.routes), strings.Join(scan.models, ", "))

	os.WriteFile(filepath.Join(outPath, "CLAUDE.md"), []byte(claudeMd), 0644)

	fmt.Println("Converting...")
	fmt.Printf("Done. Output: %s/\n\n", outPath)

	fmt.Println("Generated:")
	fmt.Printf("  %s/\n", outPath)
	fmt.Println("  ├── quikdb.yaml")
	fmt.Println("  ├── shared/")
	fmt.Println("  ├── services/")
	fmt.Println("  │   ├── api/          (Go REST API)")
	fmt.Println("  │   └── web/          (Preact + Go file server)")
	fmt.Println("  └── CLAUDE.md")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", outPath)
	fmt.Println("  # Review generated Go code")
	fmt.Println("  # Add your business logic to the handler stubs")
	fmt.Println("  quikdb-frame dev")
	fmt.Println()

	return nil
}

func scanFlask(srcPath string) scanResult {
	result := scanResult{}

	// Flask route patterns:
	// @app.route('/path', methods=['GET', 'POST'])
	// @blueprint.route('/path', methods=['GET'])
	// Flask uses <param> or <int:param> for URL params — we normalize to :param
	routePattern := regexp.MustCompile(`@\w+\.route\(\s*['"]([^'"]+)['"](?:[^)]*methods\s*=\s*\[([^\]]*)\])?`)

	// Flask middleware (before_request, after_request, CORS)
	middlewarePattern := regexp.MustCompile(`@\w+\.(?:before_request|after_request|teardown_request)|(?:CORS|flask_cors|flask_limiter|flask_jwt)`)

	// SQLAlchemy model: class User(db.Model):
	modelPattern := regexp.MustCompile(`class\s+(\w+)\s*\(\s*(?:db\.Model|Base)\s*\)`)

	filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			// Skip common non-source dirs (like node_modules in Express)
			if name == "__pycache__" || name == ".git" || name == "venv" || name == ".venv" || name == "env" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only scan Python files
		if filepath.Ext(path) != ".py" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		relPath, _ := filepath.Rel(srcPath, path)

		// Find routes
		matches := routePattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			routePath := flaskPathToStandard(m[1])
			methodsRaw := m[2] // e.g. 'GET', 'POST'

			methods := extractMethods(methodsRaw)
			for _, method := range methods {
				result.routes = append(result.routes, routeInfo{
					method: method,
					path:   routePath,
					file:   relPath,
				})
			}
		}

		// Find middleware
		mwMatches := middlewarePattern.FindAllString(content, -1)
		for _, mw := range mwMatches {
			if !contains(result.middleware, mw) {
				result.middleware = append(result.middleware, mw)
			}
		}

		// Find models
		modelMatches := modelPattern.FindAllStringSubmatch(content, -1)
		for _, m := range modelMatches {
			modelName := strings.ToLower(m[1])
			if !contains(result.models, modelName) {
				result.models = append(result.models, modelName)
			}
		}

		// Detect WebSocket (flask-socketio)
		if strings.Contains(content, "flask_socketio") || strings.Contains(content, "SocketIO") {
			result.hasWS = true
		}

		// Detect database
		if strings.Contains(content, "flask_pymongo") || strings.Contains(content, "PyMongo") || strings.Contains(content, "pymongo") {
			result.dbType = "mongo"
		} else if strings.Contains(content, "flask_sqlalchemy") || strings.Contains(content, "SQLAlchemy") || strings.Contains(content, "psycopg2") {
			result.dbType = "postgres"
		} else if strings.Contains(content, "mysql") || strings.Contains(content, "MySQLdb") {
			result.dbType = "mysql"
		}

		// Detect static files folder
		if strings.Contains(relPath, "static") {
			result.hasStatic = true
		}

		return nil
	})

	// Scan requirements.txt for more DB signals
	reqPath := filepath.Join(srcPath, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "pymongo") {
			result.dbType = "mongo"
		} else if strings.Contains(content, "psycopg2") || strings.Contains(content, "sqlalchemy") {
			if result.dbType == "" {
				result.dbType = "postgres"
			}
		} else if strings.Contains(content, "mysql") {
			if result.dbType == "" {
				result.dbType = "mysql"
			}
		}
	}

	// Scan .env or .env.example for env vars (same as Express)
	for _, envFile := range []string{".env.example", ".env", ".env.sample"} {
		envPath := filepath.Join(srcPath, envFile)
		if data, err := os.ReadFile(envPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) >= 1 && !contains(result.envVars, parts[0]) {
					result.envVars = append(result.envVars, parts[0])
				}
			}
			break
		}
	}

	if result.dbType == "" {
		result.dbType = "postgres"
	}

	return result
}

// flaskPathToStandard converts Flask URL params to standard :param format.
// Flask: /users/<int:id>  →  standard: /users/:id
// Flask: /users/<string:name>  →  standard: /users/:name
// Flask: /users/<id>  →  standard: /users/:id
func flaskPathToStandard(path string) string {
	// Replace <type:name> and <name> with :name
	re := regexp.MustCompile(`<(?:\w+:)?(\w+)>`)
	return re.ReplaceAllString(path, ":$1")
}

// extractMethods parses the methods list from a Flask route decorator.
// Input: "'GET', 'POST'" or '"GET"' or empty string (defaults to GET)
func extractMethods(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{"GET"}
	}
	// Pull out all quoted strings
	re := regexp.MustCompile(`['"]([A-Z]+)['"]`)
	matches := re.FindAllStringSubmatch(raw, -1)
	var methods []string
	for _, m := range matches {
		methods = append(methods, m[1])
	}
	if len(methods) == 0 {
		return []string{"GET"}
	}
	return methods
}
