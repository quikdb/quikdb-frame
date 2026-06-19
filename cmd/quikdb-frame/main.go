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

	case "deploy":
		svcName := ""
		if len(os.Args) >= 3 {
			svcName = os.Args[2]
		}
		if err := deploy.Run(svcName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "login":
		token := ""
		for i, arg := range os.Args {
			if arg == "--token" && i+1 < len(os.Args) {
				token = os.Args[i+1]
			}
		}
		var err error
		if token != "" {
			err = deploy.LoginWithToken(token)
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

	case "status":
		if err := deploy.Status(); err != nil {
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
  login --token <token>    Log in with an API token
  logout                   Log out
  deploy [service]         Deploy to QuikDB Compute
  status                   Show deployment status
  convert <path> --from <framework>  Convert existing project
  upgrade                  Upgrade to the latest version
  version                  Print version
  help                     Print this help

Options for init:
  --db <type>              Database type: postgres, mongo, mysql, sqlite (default: postgres)

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
