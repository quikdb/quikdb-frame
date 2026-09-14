package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *APIClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("missing authorization")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return &APIClient{BaseURL: server.URL, HTTP: &http.Client{Timeout: time.Second}}
}

func TestListCurrentContractAllPages(t *testing.T) {
	var pages []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployment/list" {
			t.Errorf("path: %s", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		fmt.Fprintf(w, `{"success":true,"data":{"deployments":[{"deploymentId":"id-%s","applicationName":"app-%s","status":"live"}],"pagination":{"page":%s,"pages":2}}}`, page, page, page)
	})
	got, err := c.List(context.Background(), "fixture-token")
	if err != nil || len(got) != 2 || strings.Join(pages, ",") != "1,2" {
		t.Fatalf("got %+v, pages %v, err %v", got, pages, err)
	}
}

func TestListContractsAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		code, count int
		wantError   bool
	}{
		{"legacy", `{"success":true,"data":[{"deploymentId":"id","applicationName":"app","status":"live"}]}`, 200, 1, false},
		{"empty", `{"success":true,"data":{"deployments":[],"pagination":{"page":1,"pages":0}}}`, 200, 0, false},
		{"unauthorized", `{"error":"Invalid token"}`, 401, 0, true},
		{"subscription", `{"message":"No active subscription"}`, 403, 0, true},
		{"quota", `{"message":"Daily build quota reached"}`, 429, 0, true},
		{"server", `<html>upstream error</html>`, 502, 0, true},
		{"malformed", `{"success":true`, 200, 0, true},
		{"rejected", `{"success":false,"data":[]}`, 200, 0, true},
		{"missing-list", `{"success":true,"data":{"pagination":{"page":1,"pages":1}}}`, 200, 0, true},
		{"missing-pagination", `{"success":true,"data":{"deployments":[]}}`, 200, 0, true},
		{"wrong-page", `{"success":true,"data":{"deployments":[],"pagination":{"page":2,"pages":2}}}`, 200, 0, true},
		{"incomplete", `{"success":true,"data":[{"applicationName":"app"}]}`, 200, 0, true},
		{"duplicate", `{"success":true,"data":[{"deploymentId":"id","applicationName":"app","status":"live"},{"deploymentId":"id","applicationName":"app","status":"live"}]}`, 200, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) })
			got, err := c.List(context.Background(), "fixture-token")
			if (err != nil) != tc.wantError {
				t.Fatalf("got %+v, err %v", got, err)
			}
			if !tc.wantError && len(got) != tc.count {
				t.Fatalf("got %d, want %d", len(got), tc.count)
			}
			if tc.code == 403 && !strings.Contains(err.Error(), "/subscription") {
				t.Fatalf("subscription guidance: %v", err)
			}
		})
	}
}

func TestWaitReadsCurrentDetailAndTransitions(t *testing.T) {
	statuses := []string{"pending", "queued", "reserving_nodes", "building", "deploying", "live"}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		status := statuses[0]
		statuses = statuses[1:]
		fmt.Fprintf(w, `{"success":true,"data":{"deployment":{"deploymentId":"id","status":"%s","publicUrl":"https://app.quikdb.net"},"logs":[]}}`, status)
	})
	d, err := c.Wait(context.Background(), "fixture-token", "id", time.Millisecond)
	if err != nil || d.Status != "live" || len(statuses) != 0 {
		t.Fatalf("got %+v, %v", d, err)
	}
}

func TestWaitTerminalAndInvalidResponses(t *testing.T) {
	for _, status := range []string{"failed", "partial", "stopped", "pending_deletion", "archived", "unexpected"} {
		t.Run(status, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"success":true,"data":{"deployment":{"deploymentId":"id","status":"%s"}}}`, status)
			})
			_, err := c.Wait(context.Background(), "fixture-token", "id", time.Millisecond)
			if err == nil {
				t.Fatal("failure reported as success")
			}
		})
	}
	for _, body := range []string{`{"success":true,"data":{"deployment":{"deploymentId":"other","status":"live"}}}`, `{"success":true,"data":{}}`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.Get(context.Background(), "fixture-token", "id"); err == nil {
			t.Fatal("invalid detail accepted")
		}
	}
}

func TestWaitLegacyAndDeadline(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"deploymentId":"id","status":"live"}}`)
	})
	if _, err := c.Wait(context.Background(), "fixture-token", "id", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"deployment":{"deploymentId":"id","status":"queued"}}}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Wait(ctx, "fixture-token", "id", time.Second); err == nil || !strings.Contains(err.Error(), "deployment continues") {
		t.Fatalf("deadline: %v", err)
	}
}

func TestSubmitNeverRetriesAndRequiresAcceptance(t *testing.T) {
	for _, body := range []string{`{"success":false}`, `{"success":true,"data":{}}`, `not-json`} {
		calls := 0
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, body) })
		if _, err := c.submit(context.Background(), "fixture-token", "/create", struct{}{}); err == nil || calls != 1 {
			t.Fatalf("calls %d, err %v", calls, err)
		}
	}
}

func TestDeployFailedApplicationUsesSameIDWithoutCreate(t *testing.T) {
	var paths []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"success":true,"data":{"deploymentId":"id","status":"pending"}}`)
			return
		}
		status := "failed"
		if len(paths) == 3 {
			status = "live"
		}
		fmt.Fprintf(w, `{"success":true,"data":{"deployment":{"deploymentId":"id","status":"%s","repositoryUrl":"https://github.com/team/app","repositoryBranch":"main","subdirectory":"services/api"}}}`, status)
	})
	d, err := deployService(context.Background(), c, "fixture-token", "git@github.com:team/app.git", "main", ServiceConfig{Name: "api", DirName: "api", Type: "api"}, map[string]Deployment{"api": {DeploymentID: "id"}})
	if err != nil || d.DeploymentID != "id" {
		t.Fatalf("got %+v, %v", d, err)
	}
	if strings.Join(paths, ",") != "GET /api/v1/deployment/id,POST /api/v1/deployment/id/redeploy,GET /api/v1/deployment/id" {
		t.Fatalf("requests: %v", paths)
	}
}

func TestDeployNameCollisionCannotMutateOtherApplication(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatal("unexpected mutation")
		}
		fmt.Fprint(w, `{"success":true,"data":{"deployment":{"deploymentId":"id","status":"failed","repositoryUrl":"https://github.com/other/app","repositoryBranch":"main"}}}`)
	})
	_, err := deployService(context.Background(), c, "fixture-token", "https://github.com/team/app", "main", ServiceConfig{Name: "api", DirName: "api"}, map[string]Deployment{"api": {DeploymentID: "id"}})
	if err == nil {
		t.Fatal("name collision accepted")
	}
}

func TestDeployDoesNotUploadEnvironmentFiles(t *testing.T) {
	var request DeployRequest
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"success":true,"data":{"deploymentId":"id","status":"pending"}}`)
		} else {
			fmt.Fprint(w, `{"success":true,"data":{"deployment":{"deploymentId":"id","status":"live"}}}`)
		}
	})
	_, err := deployService(context.Background(), c, "fixture-token", "https://github.com/team/app", "main", ServiceConfig{Name: "api", DirName: "api", Type: "api"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := request.Configuration["envVars"]; exists {
		t.Fatal("unexpected automatic environment upload")
	}
	if request.Subdirectory != "services/api" {
		t.Fatalf("subdirectory %s", request.Subdirectory)
	}
}
