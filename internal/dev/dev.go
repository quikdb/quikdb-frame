package dev

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

func Run(svcName string) error {
	if _, err := os.Stat("quikdb.yaml"); os.IsNotExist(err) {
		return fmt.Errorf("quikdb.yaml not found. Are you in a quikdb-frame project?")
	}

	services, err := discoverServices()
	if err != nil {
		return err
	}

	if svcName != "" {
		found := false
		for _, s := range services {
			if s.name == svcName {
				services = []service{s}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("service %s not found. Available: %s", svcName, serviceNames(services))
		}
	}

	if len(services) == 0 {
		return fmt.Errorf("no services found in services/")
	}

	fmt.Printf("Starting %d service(s)...\n\n", len(services))

	var wg sync.WaitGroup
	cmds := make([]*exec.Cmd, 0, len(services))

	for _, svc := range services {
		wg.Add(1)
		s := svc
		cmd, err := startService(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Failed to start: %v\n", s.name, err)
			wg.Done()
			continue
		}
		cmds = append(cmds, cmd)
		go func() {
			defer wg.Done()
			cmd.Wait()
		}()
	}

	// Graceful shutdown on Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	<-sigChan

	fmt.Println("\nShutting down all services...")
	for _, cmd := range cmds {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}
	wg.Wait()
	fmt.Println("All services stopped.")

	return nil
}

type service struct {
	name    string
	path    string
	svcType string // api, web, ws, worker
	port    string
}

func discoverServices() ([]service, error) {
	entries, err := os.ReadDir("services")
	if err != nil {
		return nil, fmt.Errorf("no services/ directory found")
	}

	portCounter := 8080
	var services []service

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		svcPath := filepath.Join("services", name)
		svcType := detectType(name, svcPath)
		port := fmt.Sprintf("%d", portCounter)
		portCounter++

		services = append(services, service{
			name:    name,
			path:    svcPath,
			svcType: svcType,
			port:    port,
		})
	}

	return services, nil
}

func detectType(name, path string) string {
	if strings.HasPrefix(name, "ws-") || name == "ws" {
		return "ws"
	}
	if strings.HasPrefix(name, "worker-") || name == "worker" {
		return "worker"
	}
	if strings.HasPrefix(name, "web") {
		return "web"
	}
	return "api"
}

func startService(svc service) (*exec.Cmd, error) {
	var cmd *exec.Cmd

	switch svc.svcType {
	case "web":
		// Check if node_modules exists, if not run npm install
		nmPath := filepath.Join(svc.path, "node_modules")
		if _, err := os.Stat(nmPath); os.IsNotExist(err) {
			fmt.Printf("[%s] Installing dependencies...\n", svc.name)
			install := exec.Command("npm", "install")
			install.Dir = svc.path
			install.Stdout = os.Stdout
			install.Stderr = os.Stderr
			if err := install.Run(); err != nil {
				return nil, fmt.Errorf("npm install failed: %w", err)
			}
		}
		cmd = exec.Command("npx", "vite", "--port", svc.port)
		cmd.Dir = svc.path

	default:
		// Go service — use go run
		cmd = exec.Command("go", "run", ".")
		cmd.Dir = svc.path
	}

	cmd.Env = append(os.Environ(), "PORT="+svc.port)
	cmd.Stdout = &prefixWriter{prefix: fmt.Sprintf("[%s] ", svc.name), w: os.Stdout}
	cmd.Stderr = &prefixWriter{prefix: fmt.Sprintf("[%s] ", svc.name), w: os.Stderr}

	fmt.Printf("[%s] %s on :%s\n", svc.name, svc.svcType, svc.port)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

func serviceNames(services []service) string {
	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.name
	}
	return strings.Join(names, ", ")
}

type prefixWriter struct {
	prefix string
	w      *os.File
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintf(pw.w, "%s%s\n", pw.prefix, line)
	}
	return len(p), nil
}
