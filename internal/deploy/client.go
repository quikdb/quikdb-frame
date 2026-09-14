package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIClient never retries mutations: a lost response may still mean the build was accepted.
type APIClient struct {
	BaseURL string
	HTTP    *http.Client
}

func newAPIClient() *APIClient {
	return &APIClient{BaseURL: apiBase, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type Deployment struct {
	DeploymentID     string `json:"deploymentId"`
	ApplicationName  string `json:"applicationName"`
	Status           string `json:"status"`
	PublicURL        string `json:"publicUrl"`
	FailureReason    string `json:"failureReason"`
	RepositoryURL    string `json:"repositoryUrl"`
	RepositoryBranch string `json:"repositoryBranch"`
	Subdirectory     string `json:"subdirectory"`
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
}

func (c *APIClient) request(ctx context.Context, token, method, path string, payload interface{}) (json.RawMessage, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+"/api/v1/deployment"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deployment API request failed: %w", err)
	}
	defer resp.Body.Close()
	const maxBody = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read API response: %w", err)
	}
	if len(raw) > maxBody {
		return nil, fmt.Errorf("API response exceeds size limit")
	}
	var envelope apiEnvelope
	decodeErr := json.Unmarshal(raw, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := envelope.Message
		if message == "" {
			message = envelope.Error
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		if resp.StatusCode == 401 {
			message = "authentication expired or invalid; run quikdb-frame login"
		}
		if resp.StatusCode == 403 {
			message += "; check account permissions and subscription at " + computeBase + "/subscription"
		}
		return nil, fmt.Errorf("deployment API (%d): %s", resp.StatusCode, message)
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("invalid API response: %w", decodeErr)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("deployment API rejected request: %s", envelope.Message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("API response missing data")
	}
	return envelope.Data, nil
}

func (c *APIClient) List(ctx context.Context, token string) ([]Deployment, error) {
	var deployments []Deployment
	for page := 1; page <= 1000; page++ {
		data, err := c.request(ctx, token, http.MethodGet, fmt.Sprintf("/list?page=%d&limit=100", page), nil)
		if err != nil {
			return nil, err
		}
		// Support the published legacy array while accepting the current paginated contract.
		if len(data) > 0 && data[0] == '[' {
			if page != 1 {
				return nil, fmt.Errorf("API pagination changed during listing")
			}
			if err := json.Unmarshal(data, &deployments); err != nil {
				return nil, fmt.Errorf("invalid deployment list: %w", err)
			}
			return deployments, validateDeployments(deployments)
		}
		var result struct {
			Deployments []Deployment `json:"deployments"`
			Pagination  struct {
				Page  int `json:"page"`
				Pages int `json:"pages"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("invalid deployment list: %w", err)
		}
		if result.Deployments == nil || result.Pagination.Page != page || result.Pagination.Pages < 0 {
			return nil, fmt.Errorf("API response missing deployment list or valid pagination")
		}
		deployments = append(deployments, result.Deployments...)
		if page >= result.Pagination.Pages {
			return deployments, validateDeployments(deployments)
		}
	}
	return nil, fmt.Errorf("API pagination exceeded safety limit")
}

func validateDeployments(deployments []Deployment) error {
	seen := make(map[string]bool)
	for _, d := range deployments {
		if d.DeploymentID == "" || d.ApplicationName == "" || d.Status == "" {
			return fmt.Errorf("API returned an incomplete deployment")
		}
		if seen[d.DeploymentID] {
			return fmt.Errorf("API returned duplicate deployment IDs; retry listing")
		}
		seen[d.DeploymentID] = true
	}
	return nil
}

func (c *APIClient) Get(ctx context.Context, token, id string) (*Deployment, error) {
	data, err := c.request(ctx, token, http.MethodGet, "/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Deployment *Deployment `json:"deployment"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid deployment detail: %w", err)
	}
	if result.Deployment == nil {
		result.Deployment = &Deployment{}
		if err := json.Unmarshal(data, result.Deployment); err != nil {
			return nil, err
		}
	}
	if result.Deployment.DeploymentID != id || result.Deployment.Status == "" {
		return nil, fmt.Errorf("API returned missing or mismatched deployment detail")
	}
	return result.Deployment, nil
}

func (c *APIClient) submit(ctx context.Context, token, path string, payload interface{}) (*Deployment, error) {
	data, err := c.request(ctx, token, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}
	var result Deployment
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid deployment acceptance: %w", err)
	}
	if result.DeploymentID == "" || result.Status == "" {
		return nil, fmt.Errorf("API did not return a deployment ID and status; check dashboard before retrying")
	}
	return &result, nil
}

func (c *APIClient) Wait(ctx context.Context, token, id string, interval time.Duration) (*Deployment, error) {
	for {
		d, err := c.Get(ctx, token, id)
		if err != nil {
			return nil, err
		}
		switch d.Status {
		case "live":
			return d, nil
		case "failed", "partial", "stopped", "archived", "pending_deletion":
			return d, fmt.Errorf("deployment %s ended with status %s; inspect logs in the dashboard", id, d.Status)
		case "pending", "queued", "reserving_nodes", "building", "deploying", "restarting", "waking":
		default:
			return d, fmt.Errorf("deployment %s has unknown status %q", id, d.Status)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return d, fmt.Errorf("stopped waiting for deployment %s (%s): %w; deployment continues on QuikDB", id, d.Status, ctx.Err())
		case <-timer.C:
		}
	}
}
