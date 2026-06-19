package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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

type DeployResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		DeploymentID string `json:"deploymentId"`
		Status       string `json:"status"`
		Subdomain    string `json:"subdomain"`
		PublicURL    string `json:"publicUrl"`
	} `json:"data"`
}

type DeployStatusResponse struct {
	Success bool   `json:"success"`
	Data    struct {
		DeploymentID string `json:"deploymentId"`
		Status       string `json:"status"`
		PublicURL    string `json:"publicUrl"`
	} `json:"data"`
}

type ExistingDeployment struct {
	DeploymentID string
	Status       string
	PublicURL    string
}

// Run deploys one or all services to QuikDB Compute.
func Run(svcName string) error {
	// 1. Check we're in a quikdb-frame project
	if _, err := os.Stat("quikdb.yaml"); os.IsNotExist(err) {
		return fmt.Errorf("quikdb.yaml not found. Are you in a quikdb-frame project?")
	}

	// 2. Require auth
	token, err := RequireAuth()
	if err != nil {
		return err
	}

	// 3. Get repo URL from git
	repoURL, branch, err := getGitInfo()
	if err != nil {
		return fmt.Errorf("could not detect git repository: %w\nMake sure your project is pushed to GitHub.", err)
	}

	// 4. Check for uncommitted changes
	if hasUncommittedChanges() {
		fmt.Println("Warning: You have uncommitted changes. Only committed and pushed code will be deployed.")
		fmt.Println()
	}

	// 5. Find services to deploy
	services, err := findServices(svcName)
	if err != nil {
		return err
	}

	// Check 1 & 2: Fetch existing deployments and warn on container count
	existing := fetchExistingDeployments(token)
	liveCount := 0
	for _, d := range existing {
		if d.Status == "live" || d.Status == "building" || d.Status == "deploying" {
			liveCount++
		}
	}
	newServices := 0
	for _, svc := range services {
		if _, found := existing[svc.Name]; !found {
			newServices++
		}
	}
	if newServices > 0 && liveCount+newServices > 3 {
		fmt.Printf("Warning: You have %d live deployment(s) and are adding %d more (%d total).\n", liveCount, newServices, liveCount+newServices)
		fmt.Println("Your plan may limit the number of containers. Check compute.quikdb.com/settings if deploys are rejected.")
		fmt.Println()
	}

	fmt.Printf("Deploying %d service(s) from %s (%s)\n\n", len(services), repoURL, branch)

	// 6. Deploy each service
	for _, svc := range services {
		// Check 1: skip or retry based on existing deployment state
		if dep, found := existing[svc.Name]; found {
			switch dep.Status {
			case "live", "building", "deploying":
				url := dep.PublicURL
				if url == "" {
					url = "compute.quikdb.com"
				}
				fmt.Printf("%-20s  already %s at %s\n", svc.Name, dep.Status, url)
				fmt.Printf("%-20s  Push to %s to redeploy.\n\n", "", branch)
				continue
			case "failed":
				fmt.Printf("Redeploying %s (previous deploy failed)...\n", svc.Name)
			default:
				fmt.Printf("Deploying %s...\n", svc.Name)
			}
		} else {
			fmt.Printf("Deploying %s...\n", svc.Name)
		}

		config := map[string]interface{}{
			"appType":      mapServiceType(svc.Type),
			"buildCommand": svc.BuildCommand,
			"startCommand": svc.StartCommand,
			"port":         3000,
			"configSource": "dockerfile",
		}

		// Read env vars from .env.example if present
		envVars := readEnvVars()
		if len(envVars) > 0 {
			config["envVars"] = envVars
		}

		req := DeployRequest{
			RepositoryURL:    repoURL,
			RepositoryBranch: branch,
			ApplicationName:  svc.Name,
			Subdirectory:     fmt.Sprintf("services/%s", svc.DirName),
			Configuration:    config,
		}

		resp, err := callDeployAPI(token, req)
		if err != nil {
			fmt.Printf("  Failed: %v\n\n", err)
			continue
		}

		fmt.Printf("  Deployment ID: %s\n", resp.Data.DeploymentID)
		fmt.Printf("  Status: %s\n", resp.Data.Status)
		if resp.Data.PublicURL != "" {
			fmt.Printf("  URL: %s\n", resp.Data.PublicURL)
		}
		fmt.Println()

		// Poll for status
		fmt.Println("  Waiting for deployment to go live...")
		pollStart := time.Now()
		finalStatus, finalURL := pollDeployment(token, resp.Data.DeploymentID)
		elapsed := time.Since(pollStart).Round(time.Second)
		if finalStatus == "live" {
			fmt.Printf("  Live at: %s (%s)\n", finalURL, elapsed)
		} else {
			fmt.Printf("  Final status: %s (%s)\n", finalStatus, elapsed)
			fmt.Println("  Check the QuikDB Compute dashboard for details.")
		}
		fmt.Println()
	}

	return nil
}

// Status checks the status of current deployments.
func Status() error {
	token, err := RequireAuth()
	if err != nil {
		return err
	}

	req, _ := http.NewRequest("GET", apiBase+"/api/v1/deployment/list", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool `json:"success"`
		Data    []struct {
			DeploymentID    string `json:"deploymentId"`
			ApplicationName string `json:"applicationName"`
			Status          string `json:"status"`
			PublicURL       string `json:"publicUrl"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)

	if !result.Success || len(result.Data) == 0 {
		fmt.Println("No deployments found.")
		return nil
	}

	fmt.Println("Deployments:")
	fmt.Println()
	for _, d := range result.Data {
		status := d.Status
		url := d.PublicURL
		if url == "" {
			url = "-"
		}
		fmt.Printf("  %-25s  %-12s  %s\n", d.ApplicationName, status, url)
	}
	fmt.Println()

	return nil
}

func callDeployAPI(token string, req DeployRequest) (*DeployResponse, error) {
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequest("POST", apiBase+"/api/v1/deployment/create", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		var errResp struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		json.Unmarshal(respBody, &errResp)
		combined := strings.ToLower(errResp.Message + " " + errResp.Error)
		if strings.Contains(combined, "subscri") || strings.Contains(combined, "upgrade") || strings.Contains(combined, "plan") {
			msg := errResp.Message
			if msg == "" {
				msg = "No active subscription found."
			}
			return nil, fmt.Errorf("%s\nVisit %s/settings/subscription to subscribe", msg, computeBase)
		}
		return nil, fmt.Errorf("authentication failed. Run: quikdb-frame logout && quikdb-frame login")
	}

	if resp.StatusCode == 409 {
		return nil, fmt.Errorf("a deployment with this name already exists.\nIf it's already live, push your changes to GitHub to redeploy automatically.\nOr delete it from the dashboard first: %s", computeBase)
	}

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("daily build quota exhausted. Try again tomorrow or upgrade your plan")
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result DeployResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("invalid API response: %w", err)
	}

	return &result, nil
}

func pollDeployment(token, deploymentID string) (string, string) {
	for i := 0; i < 60; i++ { // max 5 minutes (60 * 5s)
		time.Sleep(5 * time.Second)

		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/deployment/%s", apiBase, deploymentID), nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result DeployStatusResponse
		json.Unmarshal(body, &result)

		status := result.Data.Status
		switch status {
		case "live":
			return "live", result.Data.PublicURL
		case "failed":
			return "failed", ""
		case "building":
			if i%3 == 0 {
				fmt.Printf("  Still building...\n")
			}
		}
	}
	return "timeout", ""
}

// fetchExistingDeployments returns a map of applicationName → ExistingDeployment.
// If the request fails, returns an empty map (non-fatal).
func fetchExistingDeployments(token string) map[string]ExistingDeployment {
	req, _ := http.NewRequest("GET", apiBase+"/api/v1/deployment/list", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]ExistingDeployment{}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool `json:"success"`
		Data    []struct {
			DeploymentID    string `json:"deploymentId"`
			ApplicationName string `json:"applicationName"`
			Status          string `json:"status"`
			PublicURL       string `json:"publicUrl"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)

	out := make(map[string]ExistingDeployment)
	for _, d := range result.Data {
		out[d.ApplicationName] = ExistingDeployment{
			DeploymentID: d.DeploymentID,
			Status:       d.Status,
			PublicURL:    d.PublicURL,
		}
	}
	return out
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
			continue
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
		return repoURL, "main", nil
	}
	branch := strings.TrimSpace(string(branchOut))

	return repoURL, branch, nil
}

func hasUncommittedChanges() bool {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

func readEnvVars() map[string]string {
	for _, f := range []string{".env", ".env.example"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		vars := make(map[string]string)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && parts[1] != "" {
				vars[parts[0]] = parts[1]
			}
		}
		return vars
	}
	return nil
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
