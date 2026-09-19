package deploy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

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
	if !strings.Contains(text, "answer") || !strings.Contains(text, "42") || !strings.Contains(text, "credentials") {
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
		fmt.Fprint(w, `{"error":"The source database could not be reached","code":"managed_database_source_unreachable"}`)
	})
	_, _, err := c.databaseRequest(context.Background(), "fixture-token", http.MethodPost,
		"/api/v1/managed-databases/db_fixture123/migrations", "application/json",
		[]byte(`{"connectionString":"postgresql://user:secret@source.example/app"}`))
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "source.example") {
		t.Fatalf("unsafe error: %v", err)
	}
}
