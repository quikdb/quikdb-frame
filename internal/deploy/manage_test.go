package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const managementFixtureID = "deploy-fixture"
const managementFixtureApp = "aaaaaaaaaaaaaaaaaaaaaaaa"
const managementFixtureEnv = "eeeeeeeeeeeeeeeeeeeeeeee"

func managementDetail(w http.ResponseWriter) {
	fmt.Fprint(w, `{"success":true,"data":{"deployment":{"_id":"aaaaaaaaaaaaaaaaaaaaaaaa","deploymentId":"deploy-fixture","applicationName":"fixture","status":"live","configuration":{"port":3000,"resources":{"cpuCores":0.5,"ram":256},"environmentVariables":{"SECRET":"must-not-print"}}},"logs":[{"message":"must-not-print"}]}}`)
}
func managementFixture(t *testing.T, command string, args []string, handler http.HandlerFunc, input string) (string, error) {
	t.Helper()
	o, err := parseManagement(command, args)
	if err != nil {
		return "", err
	}
	c := testClient(t, handler)
	var out bytes.Buffer
	err = executeManagement(context.Background(), c, "fixture-token", o, strings.NewReader(input), &out)
	return out.String(), err
}

func TestManagementInputsRejectBeforeAuthentication(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"delete", []string{managementFixtureID}}, {"stop", []string{"../other"}}, {"restart", []string{"https://host/app"}},
		{"stop", []string{managementFixtureID, "--file", "unused"}}, {"rollback", []string{managementFixtureID}},
		{"rollback", []string{managementFixtureID, "--version", "0.5"}}, {"rollback", []string{managementFixtureID, "--version", "-1"}},
		{"logs", []string{managementFixtureID, "--limit", "501"}}, {"logs", []string{managementFixtureID, "--limit", "0"}},
		{"logs", []string{managementFixtureID, "--replica", "-2"}}, {"logs", []string{managementFixtureID, "--level", "secret"}},
		{"resources", []string{"set", managementFixtureID, "--cpu", "NaN"}}, {"resources", []string{"set", managementFixtureID, "--cpu", "Inf"}},
		{"resources", []string{"set", managementFixtureID, "--ram", "-1"}}, {"resources", []string{"set", managementFixtureID, "--cpu", "0", "--ram", "256"}},
		{"config", []string{"set", managementFixtureID}}, {"env", []string{"export", managementFixtureID}},
		{"env", []string{"set", managementFixtureID, "KEY", "--value-file", "path", "--stdin"}},
		{"env", []string{"set", managementFixtureID, "KEY"}}, {"env", []string{"set", managementFixtureID, "bad-key", "--stdin"}},
		{"env", []string{"set", managementFixtureID, "KEY", "--value", "must-not-print"}},
		{"domains", []string{"add", managementFixtureID, "https://example.com"}},
		{"domains", []string{"remove", managementFixtureID, "../other"}},
	} {
		t.Run(tc.command+strings.Join(tc.args, "_"), func(t *testing.T) {
			if _, err := parseManagement(tc.command, tc.args); err == nil {
				t.Fatalf("accepted %v", tc.args)
			}
		})
	}
}
func TestManagementFlagsWorkOnEitherSideOfID(t *testing.T) {
	a, err := parseManagement("logs", []string{"--json", "--limit=25", managementFixtureID, "--replica", "0"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseManagement("logs", []string{managementFixtureID, "--replica", "0", "--json", "--limit", "25"})
	if err != nil || a != b {
		t.Fatalf("%+v %+v %v", a, b, err)
	}
}
func TestManagementReadDoesNotExposeEnvOrBundledLogs(t *testing.T) {
	for _, command := range []string{"status", "inspect", "config", "resources"} {
		t.Run(command, func(t *testing.T) {
			args := []string{managementFixtureID, "--json"}
			if command == "config" || command == "resources" {
				args = append([]string{"get"}, args...)
			}
			out, err := managementFixture(t, command, args, func(w http.ResponseWriter, r *http.Request) { managementDetail(w) }, "")
			if err != nil || strings.Contains(out, "must-not-print") || strings.Contains(out, "environmentVariables") {
				t.Fatalf("output %s error %v", out, err)
			}
		})
	}
}
func TestManagementLifecycleUsesOwnedIDAndNeverRetriesMutation(t *testing.T) {
	for _, command := range []string{"stop", "restart", "redeploy", "wake", "rollback", "delete"} {
		t.Run(command, func(t *testing.T) {
			calls := 0
			args := []string{managementFixtureID, "--json"}
			if command == "delete" {
				args = append(args, "--yes")
			}
			if command == "rollback" {
				args = append(args, "--version", "0")
			}
			_, err := managementFixture(t, command, args, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					managementDetail(w)
					return
				}
				calls++
				path := "/api/v1/deployment/" + managementFixtureID + "/" + command
				method := "POST"
				if command == "delete" {
					path = "/api/v1/deployment/" + managementFixtureID
					method = "DELETE"
				}
				if r.URL.Path != path || r.Method != method {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(502)
				fmt.Fprint(w, `{"error":"synthetic lost response"}`)
			}, "")
			if err == nil || calls != 1 {
				t.Fatalf("mutation count %d error %v", calls, err)
			}
		})
	}
}
func TestManagementAcknowledgementCanOmitData(t *testing.T) {
	out, err := managementFixture(t, "restart", []string{managementFixtureID, "--json"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			managementDetail(w)
		} else {
			fmt.Fprint(w, `{"success":true,"message":"Restart accepted"}`)
		}
	}, "")
	if err != nil || !strings.Contains(out, "Restart accepted") {
		t.Fatalf("output %s error %v", out, err)
	}
}
func TestConfigurationPatchRejectsSecretAndInvalidFields(t *testing.T) {
	for _, body := range []string{`{}`, `{"environmentVariables":{"TOKEN":"must-not-print"}}`, `{"port":0}`, `{"port":3000.5}`, `{"autoDeployEnabled":"false"}`, `{"port":3000} {}`, `null`} {
		file := filepath.Join(t.TempDir(), "patch.json")
		os.WriteFile(file, []byte(body), 0600)
		if _, err := readConfigPatch(file); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	file := filepath.Join(t.TempDir(), "valid.json")
	os.WriteFile(file, []byte(`{"port":3000,"startCommand":null,"autoDeployEnabled":false}`), 0600)
	if _, err := readConfigPatch(file); err != nil {
		t.Fatal(err)
	}
}
func TestManagementConfigAndResourcePatchesMatchDashboardAPI(t *testing.T) {
	file := filepath.Join(t.TempDir(), "patch.json")
	os.WriteFile(file, []byte(`{"port":3000,"autoDeployEnabled":false}`), 0600)
	for _, tc := range []struct {
		command string
		args    []string
		method  string
	}{
		{"config", []string{"set", managementFixtureID, "--file", file}, "PUT"},
		{"resources", []string{"set", managementFixtureID, "--cpu", "0.25", "--ram", "256"}, "PATCH"},
	} {
		calls := 0
		_, err := managementFixture(t, tc.command, tc.args, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				managementDetail(w)
				return
			}
			calls++
			if r.Method != tc.method || r.URL.Path != "/api/v1/deployment/"+managementFixtureID+"/"+tc.command {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
			var values map[string]interface{}
			json.NewDecoder(r.Body).Decode(&values)
			if tc.command == "resources" && (values["cpuCores"] != 0.25 || values["ram"] != float64(256)) {
				t.Errorf("wrong resources %v", values)
			}
			fmt.Fprint(w, `{"success":true,"data":{"saved":true}}`)
		}, "")
		if err != nil || calls != 1 {
			t.Fatalf("count %d error %v", calls, err)
		}
	}
}
func TestEnvSetReadsSecretFromStdinAndRedactsResponses(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			calls := 0
			out, err := managementFixture(t, "env", []string{"set", managementFixtureID, "TOKEN", "--stdin", "--json"}, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/deployment/"+managementFixtureID {
					managementDetail(w)
					return
				}
				base := "/api/v1/applications/" + managementFixtureApp + "/env"
				if r.Method == "GET" {
					if exists {
						fmt.Fprintf(w, `{"success":true,"data":[{"_id":"%s","key":"TOKEN","value":"must-not-print","isSecret":true}]}`, managementFixtureEnv)
					} else {
						fmt.Fprint(w, `{"success":true,"data":[]}`)
					}
					return
				}
				calls++
				expected := base
				method := "POST"
				if exists {
					expected += "/" + managementFixtureEnv
					method = "PUT"
				}
				if r.URL.Path != expected || r.Method != method {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				var input map[string]interface{}
				json.NewDecoder(r.Body).Decode(&input)
				if input["value"] != "must-not-print\n" || input["isSecret"] != true {
					t.Error("secret input changed or plaintext storage chosen")
				}
				fmt.Fprint(w, `{"success":true,"data":{"value":"must-not-print"}}`)
			}, "must-not-print\n")
			if err != nil || calls != 1 || strings.Contains(out, "must-not-print") {
				t.Fatalf("out %s count %d error %v", out, calls, err)
			}
		})
	}
}
func TestEnvListOnlyExposesMetadata(t *testing.T) {
	out, err := managementFixture(t, "env", []string{"list", managementFixtureID}, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/deployment/") {
			managementDetail(w)
		} else {
			fmt.Fprintf(w, `{"success":true,"data":[{"_id":"%s","key":"PUBLIC","value":"must-not-print","isSecret":false}]}`, managementFixtureEnv)
		}
	}, "")
	if err != nil || strings.Contains(out, "must-not-print") || !strings.Contains(out, "PUBLIC") {
		t.Fatalf("out %s error %v", out, err)
	}
}
func TestEnvWriteErrorsCannotEchoSecrets(t *testing.T) {
	out, err := managementFixture(t, "env", []string{"set", managementFixtureID, "TOKEN", "--stdin"}, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/deployment/") {
			managementDetail(w)
		} else if r.Method == "GET" {
			fmt.Fprint(w, `{"success":true,"data":[]}`)
		} else {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":"must-not-print"}`)
		}
	}, "must-not-print")
	if err == nil || strings.Contains(out+err.Error(), "must-not-print") {
		t.Fatalf("out %s error %v", out, err)
	}
}
func TestEnvInvalidOrOversizedInputDoesNotCallAPI(t *testing.T) {
	for _, input := range []string{"", strings.Repeat("s", 65537), "NUL\x00value", string([]byte{255})} {
		_, err := managementFixture(t, "env", []string{"set", managementFixtureID, "TOKEN", "--stdin"}, func(w http.ResponseWriter, r *http.Request) { t.Errorf("invalid value called API") }, input)
		if err == nil {
			t.Fatal("accepted invalid input")
		}
	}
}
func TestEnvExportIsPrivateAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.env")
	fetches := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/deployment/") {
			managementDetail(w)
			return
		}
		fetches++
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "TOKEN=must-not-print\n")
	}
	out, err := managementFixture(t, "env", []string{"export", managementFixtureID, "--output", path}, handler, "")
	if err != nil || strings.Contains(out, "must-not-print") {
		t.Fatalf("out %s error %v", out, err)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if _, err = managementFixture(t, "env", []string{"export", managementFixtureID, "--output", path}, handler, ""); err == nil || fetches != 1 {
		t.Fatalf("overwritten or re-fetched %d %v", fetches, err)
	}
}
func TestEnvFailedExportRemovesReservedFile(t *testing.T) {
	for _, code := range []int{200, 500} {
		path := filepath.Join(t.TempDir(), "fixture.env")
		_, err := managementFixture(t, "env", []string{"export", managementFixtureID, "--output", path}, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/deployment/") {
				managementDetail(w)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(code)
			fmt.Fprint(w, "must-not-print")
		}, "")
		if err == nil {
			t.Fatal("accepted HTML export")
		}
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failed export left a file")
		}
	}
}
func TestManagementLogsAndHistoryOmitUnrequestedMetadata(t *testing.T) {
	for _, command := range []string{"logs", "history"} {
		out, err := managementFixture(t, command, []string{managementFixtureID, "--json"}, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/deployment/"+managementFixtureID {
				managementDetail(w)
				return
			}
			if command == "logs" {
				fmt.Fprintf(w, `{"success":true,"data":{"deploymentId":"%s","logs":[{"logId":"log-1","message":"synthetic log","metadata":{"TOKEN":"must-not-print"}}]}}`, managementFixtureID)
			} else {
				fmt.Fprintf(w, `{"success":true,"data":{"deploymentId":"%s","history":[{"status":"live","commitHash":"fixture","configuration":{"TOKEN":"must-not-print"}}]}}`, managementFixtureID)
			}
		}, "")
		if err != nil || strings.Contains(out, "must-not-print") {
			t.Fatalf("out %s error %v", out, err)
		}
	}
}
func TestManagementDomainCommandsUseOwnedAppRoutes(t *testing.T) {
	for _, action := range []string{"list", "add", "remove", "verify", "repair"} {
		args := []string{action, managementFixtureID}
		if action == "add" {
			args = append(args, "fixture.example.com")
		} else if action != "list" {
			args = append(args, "domain-fixture")
		}
		calls := 0
		_, err := managementFixture(t, "domains", args, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/deployment/") {
				managementDetail(w)
				return
			}
			calls++
			base := "/api/v1/deployments/" + managementFixtureID + "/domains"
			if !strings.HasPrefix(r.URL.Path, base) {
				t.Errorf("unexpected %s", r.URL.Path)
			}
			fmt.Fprint(w, `{"success":true,"data":{"domains":[]}}`)
		}, "")
		if err != nil || calls != 1 {
			t.Fatalf("action %s count %d error %v", action, calls, err)
		}
	}
}

func TestManagementLogFollowDeduplicatesAndCancels(t *testing.T) {
	oldInterval := managementLogInterval
	managementLogInterval = 10 * time.Millisecond
	t.Cleanup(func() { managementLogInterval = oldInterval })
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"success":true,"data":{"deploymentId":"deploy-fixture","logs":[{"logId":"log-2","message":"second"},{"logId":"log-1","message":"first"}]}}`)
	})
	o, err := parseManagement("logs", []string{managementFixtureID, "--follow", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	err = executeManagement(ctx, c, "fixture-token", o, strings.NewReader(""), &out)
	if err == nil || calls.Load() < 2 || strings.Count(out.String(), `"logId":"log-1"`) != 1 || strings.Count(out.String(), `"logId":"log-2"`) != 1 {
		t.Fatalf("follow calls %d output %s error %v", calls.Load(), out.String(), err)
	}
}

func TestEnvRemovalUsesRecordIDAndPreservesSecretMetadata(t *testing.T) {
	removed := false
	out, err := managementFixture(t, "env", []string{"remove", managementFixtureID, "PUBLIC", "--json"}, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/deployment/") {
			managementDetail(w)
			return
		}
		if r.Method == "GET" {
			fmt.Fprintf(w, `{"success":true,"data":[{"_id":"%s","key":"PUBLIC","isSecret":false,"value":"must-not-print"}]}`, managementFixtureEnv)
			return
		}
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/applications/"+managementFixtureApp+"/env/"+managementFixtureEnv {
			t.Errorf("unexpected removal %s %s", r.Method, r.URL.Path)
		}
		removed = true
		fmt.Fprint(w, `{"success":true,"message":"deleted"}`)
	}, "")
	if err != nil || !removed || !strings.Contains(out, `"isSecret":false`) || strings.Contains(out, "must-not-print") {
		t.Fatalf("removed %v output %s error %v", removed, out, err)
	}
}
