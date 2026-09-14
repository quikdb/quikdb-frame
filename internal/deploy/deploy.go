package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

type ServiceConfig struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	BuildCommand string `json:"buildCommand"`
	StartCommand string `json:"startCommand"`
	DirName      string `json:"-"` // actual directory name (always used for subdirectory path)
}

type DeployRequest struct {
	RepositoryURL    string                 `json:"repositoryUrl"`
	RepositoryBranch string                 `json:"repositoryBranch"`
	ApplicationName  string                 `json:"applicationName"`
	CustomSubdomain  string                 `json:"customSubdomain,omitempty"`
	Subdirectory     string                 `json:"subdirectory,omitempty"`
	Configuration    map[string]interface{} `json:"configuration"`
}

// Run deploys one or all services through the same Compute API as the dashboard.
func Run(svcName string) error {
	if _, err := os.Stat("quikdb.yaml"); err != nil {
		return fmt.Errorf("read quikdb.yaml: %w", err)
	}
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	repoURL, branch, err := getGitInfo()
	if err != nil {
		return fmt.Errorf("could not detect git repository: %w", err)
	}
	if hasUncommittedChanges() {
		fmt.Println("Warning: Only committed and pushed code will be deployed.")
	}
	services, err := findServices(svcName)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client := newAPIClient()
	existing, err := client.List(ctx, token)
	if err != nil {
		return fmt.Errorf("deployment preflight failed: %w", err)
	}
	byName := make(map[string]Deployment)
	for _, d := range existing {
		if _, duplicate := byName[d.ApplicationName]; duplicate {
			return fmt.Errorf("multiple deployments named %s; manage them by ID in the dashboard", d.ApplicationName)
		}
		byName[d.ApplicationName] = d
	}
	fmt.Printf("Deploying %d service(s) from %s (%s)\n\n", len(services), repoURL, branch)
	var failures []error
	for _, svc := range services {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		d, err := deployService(ctx, client, token, repoURL, branch, svc, byName)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", svc.Name, err))
			fmt.Fprintf(os.Stderr, "%s failed: %v\n", svc.Name, err)
			continue
		}
		fmt.Printf("%s: %s — %s (ID: %s)\n", svc.Name, d.Status, d.PublicURL, d.DeploymentID)
	}
	return errors.Join(failures...)
}

func deployService(ctx context.Context, client *APIClient, token, repoURL, branch string, svc ServiceConfig, existing map[string]Deployment) (*Deployment, error) {
	subdirectory := "services/" + svc.DirName
	var d *Deployment
	if prior, found := existing[svc.Name]; found {
		detail, err := client.Get(ctx, token, prior.DeploymentID)
		if err != nil {
			return nil, err
		}
		if normalizeRepo(detail.RepositoryURL) != normalizeRepo(repoURL) || detail.RepositoryBranch != branch || detail.Subdirectory != subdirectory {
			return nil, fmt.Errorf("name belongs to another repository, branch or service directory; choose a distinct service name")
		}
		switch detail.Status {
		case "live":
			fmt.Printf("%s already live; push to %s for automatic deployment of changes.\n", svc.Name, branch)
			return detail, nil
		case "failed", "stopped", "sleeping":
			d, err = client.submit(ctx, token, "/"+url.PathEscape(prior.DeploymentID)+"/redeploy", struct{}{})
			if err != nil {
				return nil, err
			}
		default:
			d = detail // Observe active work without scheduling a duplicate build.
		}
	} else {
		config := map[string]interface{}{
			"appType": mapServiceType(svc.Type), "buildCommand": svc.BuildCommand,
			"startCommand": svc.StartCommand, "port": 3000, "configSource": "dockerfile",
		}
		// Environment values are configured explicitly in Compute; never upload local
		// .env files or example defaults to every service as a deployment side effect.
		var err error
		d, err = client.submit(ctx, token, "/create", DeployRequest{
			RepositoryURL: repoURL, RepositoryBranch: branch, ApplicationName: svc.Name,
			Subdirectory: subdirectory, Configuration: config,
		})
		if err != nil {
			return nil, err
		}
	}
	fmt.Printf("%s accepted (ID: %s, status: %s); waiting...\n", svc.Name, d.DeploymentID, d.Status)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return client.Wait(waitCtx, token, d.DeploymentID, 5*time.Second)
}

func normalizeRepo(repo string) string {
	repo = strings.Replace(repo, "git@github.com:", "https://github.com/", 1)
	return strings.TrimSuffix(strings.TrimRight(repo, "/"), ".git")
}

// Status lists every page and reports API failures instead of pretending the account is empty.
func Status() error {
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	deployments, err := newAPIClient().List(ctx, token)
	if err != nil {
		return err
	}
	if len(deployments) == 0 {
		fmt.Println("No deployments found.")
		return nil
	}
	for _, d := range deployments {
		fmt.Printf("%-25s %-15s %s (ID: %s)\n", d.ApplicationName, d.Status, d.PublicURL, d.DeploymentID)
	}
	return nil
}

func findServices(svcName string) ([]ServiceConfig, error) {
	entries, err := os.ReadDir("services")
	if err != nil {
		return nil, fmt.Errorf("no services/ directory found")
	}

	var services []ServiceConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if svcName != "" && name != svcName {
			continue
		}

		// Read quikdb.json
		configPath := filepath.Join("services", name, "quikdb.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}
		var svc ServiceConfig
		if err := json.Unmarshal(data, &svc); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", configPath, err)
		}
		if svc.Name == "" {
			svc.Name = name
		}
		svc.DirName = name // always the actual directory name
		services = append(services, svc)
	}

	if len(services) == 0 {
		if svcName != "" {
			return nil, fmt.Errorf("service %s not found or has no quikdb.json", svcName)
		}
		return nil, fmt.Errorf("no services with quikdb.json found")
	}

	return services, nil
}

func getGitInfo() (string, string, error) {
	// Get remote URL
	urlOut, err := exec.Command("git", "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return "", "", fmt.Errorf("no git remote found")
	}
	repoURL := strings.TrimSpace(string(urlOut))

	// Convert SSH to HTTPS
	if strings.HasPrefix(repoURL, "git@github.com:") {
		repoURL = strings.Replace(repoURL, "git@github.com:", "https://github.com/", 1)
		repoURL = strings.TrimSuffix(repoURL, ".git")
	}

	// Get current branch
	branchOut, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("could not read git branch: %w", err)
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" || branch == "HEAD" {
		return "", "", fmt.Errorf("deploy from a named branch, not detached HEAD")
	}

	return repoURL, branch, nil
}

func hasUncommittedChanges() bool {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

func mapServiceType(svcType string) string {
	switch svcType {
	case "api", "ws", "worker":
		return "go"
	case "web":
		return "static"
	default:
		return "docker"
	}
}
