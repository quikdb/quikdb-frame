package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/quikdb/quikdb-frame/internal/project"
)

type ServiceConfig struct {
	Name       string
	ManifestID string
	Type       string
	Path       string
	Port       int
	Build      project.Build
}

type DeployRequest struct {
	ManifestVersion  int                    `json:"-"`
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
	for _, service := range services {
		if err := validateCurrentNativeDeployContract(service); err != nil {
			return err
		}
	}
	token, err := RequireAuth()
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
	if err := validateCurrentNativeDeployContract(svc); err != nil {
		return nil, err
	}
	config := map[string]interface{}{
		"appType": mapServiceType(svc.Type), "port": svc.Port,
		"internalPort": svc.Port, "configSource": "dockerfile",
	}
	return deployApplication(ctx, client, token, DeployRequest{RepositoryURL: repoURL,
		RepositoryBranch: branch, ApplicationName: svc.Name, Subdirectory: svc.Build.Context,
		Configuration: config}, existing, false)
}

func validateCurrentNativeDeployContract(svc ServiceConfig) error {
	if svc.Type == "worker" {
		return fmt.Errorf("service %s is a worker; the current Compute deployment contract requires an HTTP port, so no deployment was submitted", svc.ManifestID)
	}
	if svc.Port < 1 || svc.Port > 65535 {
		return fmt.Errorf("service %s has no deployable HTTP port", svc.ManifestID)
	}
	expectedDockerfile := path.Join(svc.Build.Context, "Dockerfile")
	if svc.Build.Context != svc.Path || svc.Build.Dockerfile != expectedDockerfile || svc.Build.Target != "" {
		return fmt.Errorf("service %s requires build context %q, Dockerfile %q and target %q; the current Compute deployment contract cannot represent these separately, so no deployment was submitted", svc.ManifestID, svc.Build.Context, svc.Build.Dockerfile, svc.Build.Target)
	}
	return nil
}

func deployApplication(ctx context.Context, client *APIClient, token string, request DeployRequest, existing map[string]Deployment, quiet bool) (*Deployment, error) {
	repoURL, branch, subdirectory, name := request.RepositoryURL, request.RepositoryBranch, request.Subdirectory, request.ApplicationName
	var d *Deployment
	if prior, found := existing[name]; found {
		detail, err := client.Get(ctx, token, prior.DeploymentID)
		if err != nil {
			return nil, err
		}
		if normalizeRepo(detail.RepositoryURL) != normalizeRepo(repoURL) || detail.RepositoryBranch != branch || detail.Subdirectory != subdirectory {
			return nil, fmt.Errorf("name belongs to another repository, branch or service directory; choose a distinct service name")
		}
		switch detail.Status {
		case "live":
			if !quiet {
				fmt.Printf("%s already live; push to %s for automatic deployment of changes.\n", name, branch)
			}
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
		var err error
		d, err = client.submit(ctx, token, "/create", request)
		if err != nil {
			return nil, err
		}
	}
	if !quiet {
		fmt.Printf("%s accepted (ID: %s, status: %s); waiting...\n", name, d.DeploymentID, d.Status)
	}
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
	manifest, err := project.Load("quikdb.yaml")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(manifest.Services))
	requiresRootModule := false
	for name := range manifest.Services {
		names = append(names, name)
		if manifest.Services[name].Build.Context == "." {
			requiresRootModule = true
		}
	}
	if requiresRootModule {
		if err := project.ValidateGoModule("go.mod", manifest.GoModule); err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	var services []ServiceConfig
	for _, name := range names {
		if svcName != "" && name != svcName {
			continue
		}
		declared := manifest.Services[name]
		info, err := os.Lstat(filepath.FromSlash(declared.Path))
		if err != nil {
			return nil, fmt.Errorf("service %s path %s is unavailable: %w", name, declared.Path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("service %s path must be a real directory", name)
		}
		buildContext := filepath.FromSlash(declared.Build.Context)
		info, err = os.Lstat(buildContext)
		if err != nil {
			return nil, fmt.Errorf("service %s build context %s is unavailable: %w", name, declared.Build.Context, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("service %s build context must be a real directory", name)
		}
		dockerfile := filepath.FromSlash(declared.Build.Dockerfile)
		info, err = os.Lstat(dockerfile)
		if err != nil {
			return nil, fmt.Errorf("service %s Dockerfile %s is unavailable: %w", name, declared.Build.Dockerfile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("service %s Dockerfile must be a regular file", name)
		}
		deploymentName := manifest.Name + "-" + name
		if len(deploymentName) > 63 {
			return nil, fmt.Errorf("service %s deployment name exceeds 63 characters; shorten the project or service name", name)
		}
		services = append(services, ServiceConfig{Name: deploymentName, ManifestID: name, Type: declared.Type, Path: declared.Path, Port: declared.Port, Build: declared.Build})
	}

	if len(services) == 0 {
		if svcName != "" {
			return nil, fmt.Errorf("service %s is not declared in quikdb.yaml", svcName)
		}
		return nil, fmt.Errorf("quikdb.yaml declares no services")
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
