package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const databaseFixtureID = "db_fixture123"

func databaseCommandFixture(t *testing.T, args []string, handler http.HandlerFunc, input string, pollInterval time.Duration) (string, error) {
	t.Helper()
	options, err := parseDatabaseCommand(args)
	if err != nil {
		return "", err
	}
	client := testClient(t, handler)
	var output bytes.Buffer
	err = executeDatabaseCommand(context.Background(), client, "fixture-token", options, strings.NewReader(input), &output, pollInterval)
	return output.String(), safeDatabaseError(err)
}

func TestDatabaseListAndTablesNeverReturnInfrastructureDetails(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/managed-databases":
			fmt.Fprint(w, `{"success":true,"data":{"databases":[{"databaseId":"db_fixture123","displayName":"Orders","projectId":"orders","engineVersion":"16","status":"available"}]}}`)
		case "/api/v1/managed-databases/db_fixture123/tables":
			fmt.Fprint(w, `{"success":true,"data":{"tables":[{"schema":"public","name":"orders","estimatedRows":3,"sizeBytes":8192}]}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	databases, err := c.listDatabases(context.Background(), "fixture-token")
	if err != nil || len(databases) != 1 || databases[0].DisplayName != "Orders" {
		t.Fatalf("%+v %v", databases, err)
	}
	tables, err := c.listDatabaseTables(context.Background(), "fixture-token", "db_fixture123")
	if err != nil || len(tables) != 1 || tables[0].Name != "orders" {
		t.Fatalf("%+v %v", tables, err)
	}
	encoded := fmt.Sprintf("%+v %+v", databases, tables)
	if strings.Contains(encoded, "postgresql://") || strings.Contains(encoded, "provider") || strings.Contains(encoded, "host") {
		t.Fatalf("private infrastructure leaked: %s", encoded)
	}
}

func TestDatabaseListAndTablesCommandsUseOpaqueOwnerScopedRoutes(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/managed-databases":
			fmt.Fprint(w, `{"success":true,"data":{"databases":[{"databaseId":"db_fixture123","displayName":"Orders","projectId":"orders","engineVersion":"16","status":"available","provider":"must-not-print","connectionString":"must-not-print"}]}}`)
		case "/api/v1/managed-databases/db_fixture123/tables":
			fmt.Fprint(w, `{"success":true,"data":{"tables":[{"schema":"public","name":"orders","estimatedRows":3,"sizeBytes":8192,"host":"must-not-print"}]}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}
	list, err := databaseCommandFixture(t, []string{"list", "--json"}, handler, "", time.Millisecond)
	if err != nil || !strings.Contains(list, "Orders") || strings.Contains(list, "must-not-print") {
		t.Fatalf("list output %s error %v", list, err)
	}
	tables, err := databaseCommandFixture(t, []string{"tables", databaseFixtureID, "--json"}, handler, "", time.Millisecond)
	if err != nil || !strings.Contains(tables, "orders") || strings.Contains(tables, "must-not-print") {
		t.Fatalf("tables output %s error %v", tables, err)
	}
}

func TestDatabaseShellQueriesThroughAuthenticatedAPI(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/managed-databases/db_fixture123/query" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"success":true,"data":{"command":"SELECT","columns":["answer"],"rows":[[42]],"rowCount":1,"truncated":false,"durationMs":3}}`)
	})
	var output bytes.Buffer
	err := databaseShell(context.Background(), c, "fixture-token", "db_fixture123", strings.NewReader("SELECT 42;\n\\q\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "answer") || !strings.Contains(text, "42") || !strings.Contains(strings.ToLower(text), "credentials") {
		t.Fatalf("unexpected shell output: %s", text)
	}
	if strings.Contains(text, "postgresql://") || strings.Contains(text, "database.example") {
		t.Fatalf("connection details leaked: %s", text)
	}
}

func TestDatabaseRequestSendsSourceOnlyInProtectedBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), "source.example") {
			t.Fatal("source leaked into URL")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type %s", r.Header.Get("Content-Type"))
		}
		fmt.Fprint(w, `{"success":true,"data":{"tableCount":1,"rowCount":2}}`)
	})
	payload := []byte(`{"connectionString":"postgresql://user:secret@source.example/app"}`)
	raw, _, err := c.databaseRequest(context.Background(), "fixture-token", http.MethodPost,
		"/api/v1/managed-databases/db_fixture123/migrations", "application/json", payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := databaseEnvelope[map[string]interface{}](raw)
	if err != nil || result["rowCount"].(float64) != 2 {
		t.Fatalf("%v %v", result, err)
	}
}

func TestDatabaseErrorsDoNotEchoConnectionStrings(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"error":"source postgresql://alice:secret@source.example/app password=hunter2 token=fixture-secret host=db.internal credential=hidden eyJabcdefghijk.abcdefghijk.abcdefghijk","code":"managed_database_source_unreachable"}`)
	})
	_, _, err := c.databaseRequest(context.Background(), "fixture-token", http.MethodPost,
		"/api/v1/managed-databases/db_fixture123/migrations", "application/json",
		[]byte(`{"connectionString":"postgresql://user:secret@source.example/app"}`))
	if err == nil {
		t.Fatal("unsafe response reported success")
	}
	for _, private := range []string{"alice", "secret", "source.example", "hunter2", "fixture-secret", "db.internal", "hidden", "eyJabcdefghijk"} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("unsafe error contains %q: %v", private, err)
		}
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestDatabaseCommandParsingRejectsAmbiguousOrUnsafeInputs(t *testing.T) {
	invalid := [][]string{
		{}, {"unknown"}, {"list", databaseFixtureID}, {"list", "--file", "query.sql"},
		{"tables", "../other"}, {"query", databaseFixtureID},
		{"query", databaseFixtureID, "--file", "query.sql", "--stdin"},
		{"dump", databaseFixtureID}, {"migrate", databaseFixtureID},
		{"migrate", databaseFixtureID, "--file", "dump.sql", "--source-env", "SOURCE_DATABASE_URL"},
	}
	for _, args := range invalid {
		if _, err := parseDatabaseCommand(args); err == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
	a, err := parseDatabaseCommand([]string{"dump", "--output", "fixture.sql", databaseFixtureID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseDatabaseCommand([]string{"dump", databaseFixtureID, "--output", "fixture.sql"})
	if err != nil || a != b {
		t.Fatalf("flags changed by position: %+v %+v %v", a, b, err)
	}
}

func TestDatabaseQueryCommandUsesBoundedInputAndOpaqueRoute(t *testing.T) {
	calls := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/managed-databases/"+databaseFixtureID+"/query" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["sql"] != "SELECT 42" {
			t.Fatalf("unexpected query body %#v: %v", body, err)
		}
		fmt.Fprint(w, `{"success":true,"data":{"command":"SELECT","columns":["answer"],"rows":[[42]],"rowCount":1,"truncated":false,"durationMs":2}}`)
	}
	out, err := databaseCommandFixture(t, []string{"query", databaseFixtureID, "--stdin", "--json"}, handler, "SELECT 42", time.Millisecond)
	if err != nil || calls != 1 || !strings.Contains(out, `"answer"`) || !strings.Contains(out, "42") {
		t.Fatalf("output %s calls %d error %v", out, calls, err)
	}
	_, err = databaseCommandFixture(t, []string{"query", databaseFixtureID, "--stdin"}, func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized query reached API")
	}, strings.Repeat("x", 50*1024+1), time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized query error: %v", err)
	}
}

func TestDatabaseDumpIsPrivateExclusiveAndCleansUpFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.sql")
	fetches := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		fetches++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/managed-databases/"+databaseFixtureID+"/export.sql" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/sql; charset=utf-8")
		fmt.Fprint(w, "-- portable fixture\nSELECT 42;\n")
	}
	out, err := databaseCommandFixture(t, []string{"dump", databaseFixtureID, "--output", path}, handler, "", time.Millisecond)
	if err != nil || fetches != 1 || !strings.Contains(out, path) {
		t.Fatalf("output %s fetches %d error %v", out, fetches, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "-- portable fixture\nSELECT 42;\n" {
		t.Fatalf("dump %q error %v", raw, err)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("private mode %v", info.Mode().Perm())
		}
	}
	if _, err = databaseCommandFixture(t, []string{"dump", databaseFixtureID, "--output", path}, handler, "", time.Millisecond); err == nil || fetches != 1 {
		t.Fatalf("existing dump overwritten or refetched: %d %v", fetches, err)
	}

	for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
		failedPath := filepath.Join(dir, fmt.Sprintf("failed-%d.sql", status))
		_, err = databaseCommandFixture(t, []string{"dump", databaseFixtureID, "--output", failedPath}, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(status)
			fmt.Fprint(w, "private upstream response")
		}, "", time.Millisecond)
		if err == nil {
			t.Fatalf("accepted failed export status %d", status)
		}
		if _, statErr := os.Stat(failedPath); !os.IsNotExist(statErr) {
			t.Fatalf("failed export retained reserved file: %v", statErr)
		}
	}
}

func TestDatabaseFileMigrationPollsToCompletionWithoutRetryingMutation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fixture.sql")
	if err := os.WriteFile(file, []byte("CREATE TABLE fixture(id integer);"), 0600); err != nil {
		t.Fatal(err)
	}
	posts, polls := 0, 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		base := "/api/v1/managed-databases/" + databaseFixtureID
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base+"/import.sql":
			posts++
			raw, _ := io.ReadAll(r.Body)
			if string(raw) != "CREATE TABLE fixture(id integer);" || !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "db-migration-") {
				t.Fatalf("invalid migration request body=%q key=%q", raw, r.Header.Get("Idempotency-Key"))
			}
			fmt.Fprint(w, `{"success":true,"data":{"migration":{"migrationId":"mdbmig_0123456789abcdef0123456789abcdef","databaseId":"db_fixture123","sourceType":"sql","state":"pending"}}}`)
		case r.Method == http.MethodGet && r.URL.Path == base+"/migrations/mdbmig_0123456789abcdef0123456789abcdef":
			polls++
			state := "running"
			if polls == 2 {
				state = "succeeded"
			}
			fmt.Fprintf(w, `{"success":true,"data":{"migration":{"migrationId":"mdbmig_0123456789abcdef0123456789abcdef","databaseId":"db_fixture123","sourceType":"sql","state":"%s","result":{"imported":true}}}}`, state)
		default:
			t.Fatalf("unexpected migration request %s %s", r.Method, r.URL.Path)
		}
	}
	out, err := databaseCommandFixture(t, []string{"migrate", databaseFixtureID, "--file", file}, handler, "", time.Millisecond)
	if err != nil || posts != 1 || polls != 2 || !strings.Contains(out, "Migration completed") {
		t.Fatalf("output %s posts %d polls %d error %v", out, posts, polls, err)
	}
}

func TestDatabaseSourceMigrationUsesNamedEnvironmentAndRedactsErrors(t *testing.T) {
	const source = "postgresql://alice:private-password@source.example/app"
	t.Setenv("SOURCE_DATABASE_URL", source)
	posts := 0
	out, err := databaseCommandFixture(t, []string{"migrate", databaseFixtureID, "--source-env", "SOURCE_DATABASE_URL"}, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), "source.example") {
			t.Fatal("source URL entered request URL")
		}
		if r.Method == http.MethodPost {
			posts++
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["connectionString"] != source {
				t.Fatal("source body changed")
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprintf(w, `{"error":"could not connect to %s password=second-secret"}`, source)
			return
		}
		t.Fatal("failed source migration unexpectedly polled")
	}, "", time.Millisecond)
	if err == nil || posts != 1 {
		t.Fatalf("output %s posts %d error %v", out, posts, err)
	}
	for _, private := range []string{"alice", "private-password", "source.example", "second-secret"} {
		if strings.Contains(out+err.Error(), private) {
			t.Fatalf("source leaked through command error: %s %v", out, err)
		}
	}
}

func TestDatabaseMigrationPollingHonorsDeadlineAndLeavesServerJobRunning(t *testing.T) {
	polls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		polls++
		fmt.Fprint(w, `{"success":true,"data":{"migration":{"migrationId":"mdbmig_0123456789abcdef0123456789abcdef","databaseId":"db_fixture123","sourceType":"sql","state":"pending"}}}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.waitDatabaseMigration(ctx, "fixture-token", databaseFixtureID, "mdbmig_0123456789abcdef0123456789abcdef", 5*time.Millisecond)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "migration continues") || polls < 1 {
		t.Fatalf("polls %d deadline error %v", polls, err)
	}
}
