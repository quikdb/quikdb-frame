package main

import (
	"fmt"
	"os"

	"github.com/quikdb/quikdb-frame/internal/convert"
	"github.com/quikdb/quikdb-frame/internal/dev"
	"github.com/quikdb/quikdb-frame/internal/scaffold"
)

const version = "0.1.0"

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
		if err := deploy(svcName); err != nil {
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

func deploy(svcName string) error {
	// Read quikdb.yaml, build Docker images, push to QuikDB Compute
	if svcName != "" {
		fmt.Printf("Deploying service: %s\n", svcName)
	} else {
		fmt.Println("Deploying all services...")
	}

	// Check quikdb.yaml exists
	if _, err := os.Stat("quikdb.yaml"); os.IsNotExist(err) {
		return fmt.Errorf("quikdb.yaml not found. Are you in a quikdb-frame project?")
	}

	fmt.Println("")
	fmt.Println("Deploy requires Docker and the QuikDB CLI.")
	fmt.Println("Run: quikdb deploy")
	fmt.Println("")
	fmt.Println("quikdb-frame generates the Dockerfiles and quikdb.json files.")
	fmt.Println("The QuikDB CLI handles the actual deployment to Compute.")
	return nil
}

func printUsage() {
	fmt.Printf(`quikdb-frame v%s — The operating system for QuikDB applications.

Usage:
  quikdb-frame <command> [arguments]

Commands:
  init <name>              Create a new project
  add <type> <name>        Add a service (api, ws, worker, web)
  dev [service]            Run services locally with hot reload
  deploy [service]         Deploy to QuikDB Compute
  convert <path> --from <framework>  Convert existing project
  version                  Print version
  help                     Print this help

Options for init:
  --db <type>              Database type: postgres, mongo, mysql, sqlite (default: postgres)

Examples:
  quikdb-frame init my-app
  quikdb-frame init my-app --db mongo
  quikdb-frame add api payments
  quikdb-frame add ws chat
  quikdb-frame add worker email
  quikdb-frame dev
  quikdb-frame dev api
  quikdb-frame deploy
  quikdb-frame convert ./my-express-app --from express

`, version)
}
