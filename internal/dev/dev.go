package dev

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/quikdb/quikdb-frame/internal/project"
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

	cmds := make([]*exec.Cmd, 0, len(services))
	done := make(chan error, len(services))
	var startFailures []error

	for _, svc := range services {
		s := svc
		cmd, err := startService(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Failed to start: %v\n", s.name, err)
			startFailures = append(startFailures, fmt.Errorf("%s: %w", s.name, err))
			continue
		}
		cmds = append(cmds, cmd)
		go func() {
			done <- cmd.Wait()
		}()
	}
	if len(cmds) == 0 {
		return fmt.Errorf("all services failed to start: %w", errors.Join(startFailures...))
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigChan)
	remaining := len(cmds)
	var processFailures []error
	for remaining > 0 {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down all services...")
			for _, cmd := range cmds {
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGTERM)
				}
			}
			for remaining > 0 {
				<-done
				remaining--
			}
			fmt.Println("All services stopped.")
			return nil
		case err := <-done:
			remaining--
			if err != nil {
				processFailures = append(processFailures, err)
			}
		}
	}
	if len(processFailures) == 0 {
		return errors.New("all services exited")
	}
	return fmt.Errorf("all services exited: %w", errors.Join(processFailures...))
}

type service struct {
	name    string
	path    string
	svcType string // api, web, ws, worker
	port    string
}

func discoverServices() ([]service, error) {
	manifest, err := project.Load("quikdb.yaml")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(manifest.Services))
	for name := range manifest.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	services := make([]service, 0, len(names))
	for _, name := range names {
		definition := manifest.Services[name]
		info, err := os.Stat(definition.Path)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("service %s path %s is not a directory", name, definition.Path)
		}
		port := ""
		if definition.Port != 0 {
			port = strconv.Itoa(definition.Port)
		}
		services = append(services, service{
			name:    name,
			path:    definition.Path,
			svcType: definition.Type,
			port:    port,
		})
	}

	return services, nil
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

	cmd.Env = os.Environ()
	if svc.port != "" {
		cmd.Env = append(cmd.Env, "PORT="+svc.port)
	}
	cmd.Stdout = &prefixWriter{prefix: fmt.Sprintf("[%s] ", svc.name), w: os.Stdout}
	cmd.Stderr = &prefixWriter{prefix: fmt.Sprintf("[%s] ", svc.name), w: os.Stderr}

	if svc.port == "" {
		fmt.Printf("[%s] %s\n", svc.name, svc.svcType)
	} else {
		fmt.Printf("[%s] %s on :%s\n", svc.name, svc.svcType, svc.port)
	}

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
