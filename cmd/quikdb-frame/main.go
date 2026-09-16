package main

import (
	"fmt"
	"os"

	"github.com/quikdb/quikdb-frame/internal/convert"
	"github.com/quikdb/quikdb-frame/internal/deploy"
	"github.com/quikdb/quikdb-frame/internal/dev"
	"github.com/quikdb/quikdb-frame/internal/scaffold"
	"github.com/quikdb/quikdb-frame/internal/upgrade"
)

// version is set at build time via -ldflags "-X main.version=x.y.z"
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "init":
		if len(os.Args) < 3 {
			fmt.Println("Usage: quikdb-frame init <project-name>")
			os.Exit(1)
		}
		name := os.Args[2]
		dbType := "postgres"
		for i, arg := range os.Args {
			if arg == "--db" && i+1 < len(os.Args) {
				dbType = os.Args[i+1]
			}
		}
		if err := scaffold.Init(name, dbType); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "add":
		if len(os.Args) < 4 {
			fmt.Println("Usage: quikdb-frame add <type> <name>")
			fmt.Println("Types: api, ws, worker, web")
			os.Exit(1)
		}
		svcType := os.Args[2]
		svcName := os.Args[3]
		if err := scaffold.Add(svcType, svcName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "dev":
		svcName := ""
		if len(os.Args) >= 3 {
			svcName = os.Args[2]
		}
		if err := dev.Run(svcName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "manifest":
		if err := deploy.ManifestCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "deploy":
		if err := deploy.Command(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "login":
		device := false
		for _, arg := range os.Args[2:] {
			if arg == "--device" {
				device = true
			}
		}
		// Prefer QUIKDB_TOKEN env var — safe for CI/CD, not visible in ps aux or shell history
		token := os.Getenv("QUIKDB_TOKEN")

		// Also accept --token flag for convenience, but warn about exposure risk
		for i, arg := range os.Args {
			if arg == "--token" && i+1 < len(os.Args) {
				token = os.Args[i+1]
				fmt.Fprintln(os.Stderr, "Warning: --token flag exposes the token in process list (ps aux) and shell history.")
				fmt.Fprintln(os.Stderr, "         Use the QUIKDB_TOKEN environment variable instead for better security.")
			}
		}

		var err error
		if token != "" {
			err = deploy.LoginWithToken(token)
		} else if device {
			err = deploy.LoginDevice()
		} else {
			err = deploy.Login()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "logout":
		if err := deploy.Logout(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "status", "inspect", "logs", "history", "stop", "restart", "redeploy", "wake", "rollback", "delete", "config", "resources", "env", "domains":
		if err := deploy.Manage(cmd, os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "whoami":
		if err := deploy.Whoami(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "convert":
		if len(os.Args) < 3 {
			fmt.Println("Usage: quikdb-frame convert <path> --from <framework>")
			os.Exit(1)
		}
		srcPath := os.Args[2]
		fromFramework := ""
		for i, arg := range os.Args {
			if arg == "--from" && i+1 < len(os.Args) {
				fromFramework = os.Args[i+1]
			}
		}
		if fromFramework == "" {
			fmt.Println("Error: --from flag required (express, nestjs, nextjs, fastapi, django, flask)")
			os.Exit(1)
		}
		if err := convert.Run(srcPath, fromFramework); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "upgrade":
		if err := upgrade.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "version", "--version", "-v":
		fmt.Printf("quikdb-frame v%s\n", version)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`quikdb-frame v%s — The operating system for QuikDB applications.

Usage:
  quikdb-frame <command> [arguments]

Commands:
  init <name>              Create a new project
  add <type> <name>        Add a service (api, ws, worker, web)
  dev [service]            Run services locally with hot reload
  login                    Log in to QuikDB Compute
  login --device           Authorize a remote/headless terminal
  login --token <token>    Log in with an API token (see warning below)
  logout                   Log out
  deploy [service]         Deploy a Frame project or existing application
  manifest validate        Validate quikdb.json v1 offline before sign-in
  status                   Show deployment status
  inspect <id>             Show public deployment detail (environment values omitted)
  logs <id> [--follow]      Read recent logs; --json follow produces JSON lines
  history <id>             Show deployment timeline (not rollback indices)
  stop|restart|redeploy|wake <id>  Manage the same dashboard deployment
  rollback <id> --version <n>     Restore retained previous version, 0 = most recent
  delete <id> --yes         Delete this exact application
  config get|set <id>       Read settings or save --file JSON patch
  resources get|set <id>    Read allocation or set --cpu and/or --ram (MB)
  env list|set|remove|export <id>  Manage env; set requires KEY --value-file or --stdin
  domains list|add|remove|verify|repair <id>  Manage app domains, subject to plan limits
  whoami                   Show authenticated account and plan
  convert <path> --from <framework>  Convert existing project
  upgrade                  Upgrade to the latest version
  version                  Print version
  help                     Print this help

Options for init:
  --db <type>              Database type: postgres, mongo, mysql, sqlite (default: postgres)

Options for deploy (flags before service name):
  --mode as-is             Preserve the application language/runtime (default)
  --repo <GitHub URL>      Deploy a repository without a local Frame layout
  --branch <branch>        Required with --repo; otherwise use current Git branch
  --name <name>            Application name (default: repository name)
  --source <directory>    Upload local as-is source; requires --config (no Git branch)
  --subdirectory <path>    Service root; shared detection reads this directory
  --config <path>          Explicit deployment configuration JSON
  --port <port>            Actual internal application port
  --dry-run                Review source/config summary without deployment submission
  --json                   Emit an as-is result JSON object
  --mode frame             Native Frame only; deployment-time conversion not certified yet

Security:
  QUIKDB_TOKEN             Set this environment variable instead of --token to avoid exposing
                           the token in process lists (ps aux) and shell history.
                           Example: QUIKDB_TOKEN=<token> quikdb-frame login

Workflow:
  quikdb-frame init my-app          # create project
  cd my-app
  quikdb-frame dev                  # develop locally
  git init && git add . && git commit -m "init"
  gh repo create --public --push    # push to GitHub
  quikdb-frame login                # log in to QuikDB
  quikdb-frame deploy               # deploy to Compute
  quikdb-frame status               # check deployment

`, version)
}
