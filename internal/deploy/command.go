package deploy

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
)

// DeployOptions describe original-source deployment. Conversion never happens implicitly.
type DeployOptions struct {
	Repo, Branch, Name, Subdirectory, Config, Mode string
	Port                                           int
	DryRun, JSON                                   bool
	Service                                        string
	explicitConfiguration                          map[string]interface{}
	manifestVersion                                int
}

func ParseDeployOptions(args []string) (DeployOptions, error) {
	var o DeployOptions
	f := flag.NewFlagSet("deploy", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.Repo, "repo", "", "GitHub repository URL")
	f.StringVar(&o.Branch, "branch", "", "Connected branch")
	f.StringVar(&o.Name, "name", "", "Application name")
	f.StringVar(&o.Subdirectory, "subdirectory", "", "Service directory")
	f.StringVar(&o.Config, "config", "", "Explicit deployment configuration JSON")
	f.StringVar(&o.Mode, "mode", "as-is", "as-is or native Frame")
	f.IntVar(&o.Port, "port", 0, "Internal application port")
	f.BoolVar(&o.DryRun, "dry-run", false, "Detect and review without submitting a deployment")
	f.BoolVar(&o.JSON, "json", false, "Emit a plan/result JSON object")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() > 1 {
		return o, fmt.Errorf("expected at most one Frame service; put flags before the service name")
	}
	if f.NArg() == 1 {
		o.Service = f.Arg(0)
		if strings.HasPrefix(o.Service, "-") {
			return o, fmt.Errorf("invalid service name")
		}
	}
	if o.Mode != "as-is" && o.Mode != "frame" {
		return o, fmt.Errorf("mode must be as-is or frame")
	}
	if o.Port < 0 || o.Port > 65535 {
		return o, fmt.Errorf("port must be between 1 and 65535")
	}
	if strings.Contains(o.Subdirectory, "\\") || strings.HasPrefix(o.Subdirectory, "/") {
		return o, fmt.Errorf("subdirectory must be a relative repository path")
	}
	for _, part := range strings.Split(o.Subdirectory, "/") {
		if part == ".." {
			return o, fmt.Errorf("subdirectory cannot traverse outside the repository")
		}
	}
	o.Subdirectory = path.Clean(o.Subdirectory)
	if o.Subdirectory == "." {
		o.Subdirectory = ""
	}
	return o, nil
}

var deployClientFactory = newAPIClient

func Command(args []string) error {
	o, err := ParseDeployOptions(args)
	if err != nil {
		return err
	}
	_, nativeErr := os.Stat("quikdb.yaml")
	external := o.Repo != "" || o.Branch != "" || o.Name != "" || o.Config != "" || o.Subdirectory != "" || o.Port != 0
	if nativeErr == nil && !external {
		if o.DryRun || o.JSON {
			return fmt.Errorf("native Frame plan/JSON output is not available yet; omit those flags")
		}
		return Run(o.Service)
	}
	if o.Service != "" {
		return fmt.Errorf("service selection requires a Frame project; use --subdirectory and --config for an existing monorepo")
	}
	if o.Mode == "frame" {
		return fmt.Errorf("deployment-time conversion is not certified yet; use --mode as-is to preserve your existing application")
	}
	if err := resolveSource(&o); err != nil {
		return err
	}
	if o.Config != "" {
		o.explicitConfiguration, o.manifestVersion, err = readDeploymentConfiguration(o.Config, false)
		if err != nil {
			return err
		}
	}
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client := deployClientFactory()
	request, err := planAsIs(ctx, client, token, o)
	if err != nil {
		return err
	}
	summary := map[string]interface{}{"mode": "as-is", "repositoryUrl": request.RepositoryURL, "repositoryBranch": request.RepositoryBranch, "applicationName": request.ApplicationName, "subdirectory": request.Subdirectory, "configSource": request.Configuration["configSource"], "appType": request.Configuration["appType"], "port": request.Configuration["port"], "source": "committed Git branch", "manifestVersion": request.ManifestVersion}
	if o.DryRun {
		return json.NewEncoder(os.Stdout).Encode(summary)
	}
	existing, err := client.List(ctx, token)
	if err != nil {
		return fmt.Errorf("deployment preflight failed: %w", err)
	}
	byName := make(map[string]Deployment)
	for _, d := range existing {
		if d.ApplicationName != o.Name {
			continue
		}
		if _, ok := byName[o.Name]; ok {
			return fmt.Errorf("multiple applications named %s; manage them by ID in Compute", o.Name)
		}
		byName[o.Name] = d
	}
	if !o.JSON {
		fmt.Printf("Deploying %s as-is from %s (%s). Runtime configuration comes from %s.\n", o.Name, o.Repo, o.Branch, request.Configuration["configSource"])
	}
	result, err := deployApplication(ctx, client, token, request, byName, o.JSON)
	if err != nil {
		return err
	}
	if o.JSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"mode": "as-is", "deployment": result})
	}
	fmt.Printf("%s: %s — %s (ID: %s)\n", o.Name, result.Status, result.PublicURL, result.DeploymentID)
	return nil
}

func resolveSource(o *DeployOptions) error {
	if o.Repo == "" {
		repo, branch, err := getGitInfo()
		if err != nil {
			return err
		}
		o.Repo = repo
		if o.Branch == "" {
			o.Branch = branch
		}
		if hasUncommittedChanges() && !o.JSON {
			fmt.Fprintln(os.Stderr, "Warning: only committed and pushed source will be deployed.")
		}
	} else if o.Branch == "" {
		return fmt.Errorf("--branch is required with --repo; choose the actual connected branch")
	}
	u, err := url.Parse(normalizeRepo(o.Repo))
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("use a GitHub HTTPS/SSH repository URL without credentials, query or fragment")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(parts[0]) || !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." {
		return fmt.Errorf("repository URL must identify a GitHub owner/repository")
	}
	if o.Branch == "" || strings.HasPrefix(o.Branch, "-") || strings.ContainsAny(o.Branch, " \t\r\n~^:?*[\\") || strings.Contains(o.Branch, "..") || strings.HasSuffix(o.Branch, ".lock") {
		return fmt.Errorf("invalid repository branch")
	}
	o.Repo = "https://github.com/" + parts[0] + "/" + parts[1]
	if o.Name == "" {
		o.Name = strings.ToLower(parts[1])
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`).MatchString(o.Name) {
		return fmt.Errorf("choose --name using 1–63 lowercase letters, numbers or hyphens")
	}
	return nil
}

func planAsIs(ctx context.Context, c *APIClient, token string, o DeployOptions) (DeployRequest, error) {
	request := DeployRequest{RepositoryURL: o.Repo, RepositoryBranch: o.Branch, ApplicationName: o.Name, Subdirectory: o.Subdirectory}
	var config map[string]interface{}
	if o.Config != "" {
		if o.explicitConfiguration != nil {
			config = o.explicitConfiguration
			request.ManifestVersion = o.manifestVersion
		} else {
			var err error
			config, request.ManifestVersion, err = readDeploymentConfiguration(o.Config, false)
			if err != nil {
				return request, err
			}
		}
	} else {
		data, err := c.request(ctx, token, http.MethodPost, "/detect-config", map[string]string{"repositoryUrl": o.Repo, "branch": o.Branch, "subdirectory": o.Subdirectory})
		if err != nil {
			return request, err
		}
		var detected struct {
			AppType         string                 `json:"appType"`
			Framework       string                 `json:"framework"`
			ConfigSource    string                 `json:"configSource"`
			Subdirectory    string                 `json:"subdirectory"`
			ManifestVersion int                    `json:"manifestVersion"`
			DetectedConfig  map[string]interface{} `json:"detectedConfig"`
		}
		if err := json.Unmarshal(data, &detected); err != nil {
			return request, fmt.Errorf("invalid detected configuration: %w", err)
		}
		if detected.Subdirectory != o.Subdirectory {
			return request, fmt.Errorf("configuration detection returned a different service directory; upgrade the API before deploying this service")
		}
		request.ManifestVersion = detected.ManifestVersion
		config = detected.DetectedConfig
		if config == nil {
			return request, fmt.Errorf("configuration detection returned no build settings; use --config")
		}
		config["appType"] = detected.AppType
		config["framework"] = detected.Framework
		config["configSource"] = detected.ConfigSource
	}
	if o.Port != 0 {
		config["port"] = o.Port
		config["internalPort"] = o.Port
	}
	source, _ := config["configSource"].(string)
	start, _ := config["startCommand"].(string)
	if source != "dockerfile" && source != "docker-compose" && strings.TrimSpace(start) == "" {
		return request, fmt.Errorf("no production start command or Dockerfile detected; provide --config with the app's original build/start settings")
	}
	port, ok := config["port"].(float64)
	if explicit, yes := config["port"].(int); yes {
		port, ok = float64(explicit), true
	}
	if !ok || port < 1 || port > 65535 || port != float64(int(port)) {
		return request, fmt.Errorf("provide the actual internal application port (1–65535) with --port or configuration.port")
	}
	if internal, exists := config["internalPort"]; exists {
		internalPort, valid := internal.(float64)
		if explicit, yes := internal.(int); yes {
			internalPort, valid = float64(explicit), true
		}
		if !valid || internalPort != port {
			return request, fmt.Errorf("configuration port/internalPort disagree; resolve them with --port")
		}
	}
	request.Configuration = config
	return request, nil
}
