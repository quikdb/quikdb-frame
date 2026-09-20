package deploy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"
)

const databaseBodyLimit = 4 << 20

var (
	databaseConnectionURL = regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis(?:s)?|cockroachdb)://[^\s"'<>]+`)
	databaseURLUserInfo   = regexp.MustCompile(`(?i)\bhttps?://[^\s/@:]+:[^\s/@]+@[^\s"'<>]+`)
	databaseSecretValue   = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|authorization|credential|connection[_-]?(?:string|url)|database[_-]?url|username|user|host)\b\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	databaseOpaqueSecret  = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})\b`)
)

type redactedDatabaseError struct {
	cause   error
	message string
}

func (e *redactedDatabaseError) Error() string { return e.message }
func (e *redactedDatabaseError) Unwrap() error { return e.cause }

func redactDatabaseText(value string) string {
	value = databaseURLUserInfo.ReplaceAllString(value, "[REDACTED_URL]")
	value = databaseConnectionURL.ReplaceAllString(value, "[REDACTED_DATABASE_URL]")
	value = databaseSecretValue.ReplaceAllString(value, "$1=[REDACTED]")
	return databaseOpaqueSecret.ReplaceAllString(value, "[REDACTED_SECRET]")
}

func safeDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	message := redactDatabaseText(err.Error())
	if message == err.Error() {
		return err
	}
	return &redactedDatabaseError{cause: err, message: message}
}

type managedDatabase struct {
	DatabaseID    string `json:"databaseId"`
	DisplayName   string `json:"displayName"`
	ProjectID     string `json:"projectId"`
	EngineVersion string `json:"engineVersion"`
	Status        string `json:"status"`
}

type databaseTable struct {
	Schema        string `json:"schema"`
	Name          string `json:"name"`
	EstimatedRows int64  `json:"estimatedRows"`
	SizeBytes     int64  `json:"sizeBytes"`
}

type databaseQueryResult struct {
	Command    string          `json:"command"`
	Columns    []string        `json:"columns"`
	Rows       [][]interface{} `json:"rows"`
	RowCount   int             `json:"rowCount"`
	Truncated  bool            `json:"truncated"`
	DurationMS int             `json:"durationMs"`
}

type databaseMigration struct {
	MigrationID string `json:"migrationId"`
	DatabaseID  string `json:"databaseId"`
	SourceType  string `json:"sourceType"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	Result      *struct {
		TableCount int  `json:"tableCount"`
		RowCount   int  `json:"rowCount"`
		Imported   bool `json:"imported"`
	} `json:"result"`
	ErrorCode string `json:"errorCode"`
}

func (c *APIClient) databaseRequest(ctx context.Context, token, method, path, contentType string, body []byte, extraHeaders ...map[string]string) ([]byte, string, error) {
	if c.TokenProvider != nil {
		current, err := c.TokenProvider()
		if err != nil {
			return nil, "", safeDatabaseError(fmt.Errorf("database authentication: %w", err))
		}
		token = current
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for _, headers := range extraHeaders {
		for key, value := range headers {
			request.Header.Set(key, value)
		}
	}
	client := *c.HTTP
	client.Timeout = 130 * time.Second
	response, err := client.Do(request)
	if err != nil {
		return nil, "", safeDatabaseError(fmt.Errorf("database API request failed: %w", err))
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, databaseBodyLimit+1))
	if err != nil {
		return nil, "", fmt.Errorf("read database API response: %w", err)
	}
	if len(raw) > databaseBodyLimit {
		return nil, "", fmt.Errorf("database response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope apiEnvelope
		_ = json.Unmarshal(raw, &envelope)
		message := envelope.Error
		if message == "" {
			message = envelope.Message
		}
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		if response.StatusCode == http.StatusUnauthorized {
			message = "authentication expired or invalid; run quikdb-frame login"
		}
		return nil, "", safeDatabaseError(fmt.Errorf("database API (%d): %s", response.StatusCode, message))
	}
	return raw, response.Header.Get("Content-Type"), nil
}

func databaseEnvelope[T any](raw []byte) (T, error) {
	var zero T
	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || !envelope.Success || len(envelope.Data) == 0 {
		return zero, fmt.Errorf("invalid database API response")
	}
	var value T
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		return zero, fmt.Errorf("invalid database data: %w", err)
	}
	return value, nil
}

func (c *APIClient) listDatabases(ctx context.Context, token string) ([]managedDatabase, error) {
	raw, _, err := c.databaseRequest(ctx, token, http.MethodGet, "/api/v1/managed-databases", "", nil)
	if err != nil {
		return nil, err
	}
	data, err := databaseEnvelope[struct {
		Databases []managedDatabase `json:"databases"`
	}](raw)
	return data.Databases, err
}

func (c *APIClient) listDatabaseTables(ctx context.Context, token, id string) ([]databaseTable, error) {
	raw, _, err := c.databaseRequest(ctx, token, http.MethodGet, "/api/v1/managed-databases/"+url.PathEscape(id)+"/tables", "", nil)
	if err != nil {
		return nil, err
	}
	data, err := databaseEnvelope[struct {
		Tables []databaseTable `json:"tables"`
	}](raw)
	return data.Tables, err
}

func (c *APIClient) databaseQuery(ctx context.Context, token, id, sql string) (databaseQueryResult, error) {
	payload, _ := json.Marshal(map[string]string{"sql": sql})
	raw, _, err := c.databaseRequest(ctx, token, http.MethodPost, "/api/v1/managed-databases/"+url.PathEscape(id)+"/query", "application/json", payload)
	if err != nil {
		return databaseQueryResult{}, err
	}
	return databaseEnvelope[databaseQueryResult](raw)
}

func (c *APIClient) waitDatabaseMigration(ctx context.Context, token, databaseID, migrationID string, pollInterval time.Duration) (databaseMigration, error) {
	path := "/api/v1/managed-databases/" + url.PathEscape(databaseID) + "/migrations/" + url.PathEscape(migrationID)
	for {
		raw, _, err := c.databaseRequest(ctx, token, http.MethodGet, path, "", nil)
		if err != nil {
			return databaseMigration{}, err
		}
		data, err := databaseEnvelope[struct {
			Migration databaseMigration `json:"migration"`
		}](raw)
		if err != nil {
			return databaseMigration{}, err
		}
		switch data.Migration.State {
		case "succeeded":
			return data.Migration, nil
		case "failed":
			return data.Migration, safeDatabaseError(fmt.Errorf("migration failed safely (%s); the destination was left unchanged", data.Migration.ErrorCode))
		case "pending", "running":
		default:
			return data.Migration, fmt.Errorf("migration returned an unknown state")
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return data.Migration, fmt.Errorf("stopped waiting: %w; migration continues on QuikDB", ctx.Err())
		case <-timer.C:
		}
	}
}

type databaseOptions struct {
	action, id, sqlFile, outputFile, sourceEnv string
	jsonOutput, stdin                          bool
}

func parseDatabaseCommand(args []string) (databaseOptions, error) {
	var options databaseOptions
	if len(args) == 0 {
		return options, fmt.Errorf("db requires list, tables, connect, query, dump or migrate")
	}
	options.action, args = args[0], args[1:]
	flags := flag.NewFlagSet("db "+options.action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.jsonOutput, "json", false, "JSON output")
	flags.StringVar(&options.sqlFile, "file", "", "Read SQL from a file")
	flags.BoolVar(&options.stdin, "stdin", false, "Read SQL from stdin")
	flags.StringVar(&options.outputFile, "output", "", "Private output file")
	flags.StringVar(&options.sourceEnv, "source-env", "", "Environment variable containing a source PostgreSQL URL")
	ordered, err := managementArgs(flags, args)
	if err != nil {
		return options, err
	}
	if err = flags.Parse(ordered); err != nil {
		return options, err
	}
	allowed := map[string]map[string]bool{
		"list": {"json": true}, "tables": {"json": true}, "connect": {},
		"query": {"json": true, "file": true, "stdin": true},
		"dump":  {"output": true}, "migrate": {"source-env": true, "file": true},
	}
	validFlags, supported := allowed[options.action]
	if !supported {
		return options, fmt.Errorf("unsupported db command %s", options.action)
	}
	var unsupported string
	flags.Visit(func(option *flag.Flag) {
		if !validFlags[option.Name] {
			unsupported = option.Name
		}
	})
	if unsupported != "" {
		return options, fmt.Errorf("db %s does not support --%s", options.action, unsupported)
	}
	positions := flags.Args()
	wantID := options.action != "list"
	if (wantID && len(positions) != 1) || (!wantID && len(positions) != 0) {
		return options, fmt.Errorf("db %s received invalid arguments", options.action)
	}
	if wantID {
		options.id = positions[0]
		if !strings.HasPrefix(options.id, "db_") || !resourceID.MatchString(options.id) {
			return options, fmt.Errorf("use an exact database ID")
		}
	}
	if options.action == "query" && (options.sqlFile == "") == !options.stdin {
		return options, fmt.Errorf("db query requires exactly one of --file or --stdin")
	}
	if options.action == "dump" && options.outputFile == "" {
		return options, fmt.Errorf("db dump requires --output")
	}
	if options.action == "migrate" && (options.sourceEnv == "") == (options.sqlFile == "") {
		return options, fmt.Errorf("db migrate requires exactly one of --source-env or --file")
	}
	return options, nil
}

func writeDatabaseRows(output io.Writer, result databaseQueryResult) error {
	if len(result.Columns) == 0 {
		_, err := fmt.Fprintf(output, "%s %d row(s) in %d ms\n", result.Command, result.RowCount, result.DurationMS)
		return err
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	for _, column := range result.Columns {
		fmt.Fprintf(table, "%s\t", column)
	}
	fmt.Fprintln(table)
	for _, row := range result.Rows {
		for _, value := range row {
			if value == nil {
				fmt.Fprint(table, "NULL\t")
			} else {
				fmt.Fprintf(table, "%v\t", value)
			}
		}
		fmt.Fprintln(table)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "%s %d row(s) in %d ms%s\n", result.Command, result.RowCount, result.DurationMS,
		map[bool]string{true: " (first 500 shown)", false: ""}[result.Truncated])
	return err
}

func databaseShell(ctx context.Context, client *APIClient, token, id string, input io.Reader, output io.Writer) error {
	fmt.Fprintln(output, "Connected through QuikDB. Credentials and infrastructure details remain hidden. Type \\q to exit or \\dt to list tables.")
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for {
		fmt.Fprint(output, "quikdb> ")
		if !scanner.Scan() {
			break
		}
		command := strings.TrimSpace(scanner.Text())
		if command == "" {
			continue
		}
		if command == "\\q" || command == "quit" || command == "exit" {
			return nil
		}
		if command == "\\dt" {
			tables, err := client.listDatabaseTables(ctx, token, id)
			if err != nil {
				fmt.Fprintf(output, "Error: %v\n", err)
				continue
			}
			for _, item := range tables {
				fmt.Fprintf(output, "%s.%s\t%d rows\n", item.Schema, item.Name, item.EstimatedRows)
			}
			continue
		}
		result, err := client.databaseQuery(ctx, token, id, command)
		if err != nil {
			fmt.Fprintf(output, "Error: %v\n", err)
			continue
		}
		if err = writeDatabaseRows(output, result); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func DatabaseCommand(args []string) error {
	options, err := parseDatabaseCommand(args)
	if err != nil {
		return safeDatabaseError(err)
	}
	ctx, cancelSignal := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancelSignal()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	token, err := RequireAuth()
	if err != nil {
		return safeDatabaseError(err)
	}
	return safeDatabaseError(executeDatabaseCommand(ctx, deployClientFactory(), token, options, os.Stdin, os.Stdout, 2*time.Second))
}

func executeDatabaseCommand(ctx context.Context, client *APIClient, token string, options databaseOptions, input io.Reader, output io.Writer, pollInterval time.Duration) error {
	switch options.action {
	case "list":
		databases, err := client.listDatabases(ctx, token)
		if err != nil {
			return err
		}
		if options.jsonOutput {
			return emitManagement(output, true, databases)
		}
		if len(databases) == 0 {
			_, err = fmt.Fprintln(output, "No databases found.")
			return err
		}
		for _, database := range databases {
			fmt.Fprintf(output, "%-24s %-12s PostgreSQL %s (ID: %s)\n", database.DisplayName, database.Status, database.EngineVersion, database.DatabaseID)
		}
		return nil
	case "tables":
		tables, err := client.listDatabaseTables(ctx, token, options.id)
		if err != nil {
			return err
		}
		if options.jsonOutput {
			return emitManagement(output, true, tables)
		}
		for _, table := range tables {
			fmt.Fprintf(output, "%s.%s\t%d rows\t%d bytes\n", table.Schema, table.Name, table.EstimatedRows, table.SizeBytes)
		}
		return nil
	case "connect":
		return databaseShell(ctx, client, token, options.id, input, output)
	case "query":
		var reader io.Reader = input
		var file *os.File
		var err error
		if options.sqlFile != "" {
			file, err = os.Open(options.sqlFile)
			if err != nil {
				return err
			}
			defer file.Close()
			reader = file
		}
		raw, err := readBounded(reader, 50*1024)
		if err != nil {
			return err
		}
		result, err := client.databaseQuery(ctx, token, options.id, string(raw))
		if err != nil {
			return err
		}
		if options.jsonOutput {
			return emitManagement(output, true, result)
		}
		return writeDatabaseRows(output, result)
	case "dump":
		file, err := createPrivateExport(options.outputFile)
		if err != nil {
			return err
		}
		raw, contentType, requestErr := client.databaseRequest(ctx, token, http.MethodGet, "/api/v1/managed-databases/"+url.PathEscape(options.id)+"/export.sql", "", nil)
		if requestErr == nil && !strings.HasPrefix(contentType, "application/sql") {
			requestErr = fmt.Errorf("unexpected database export response type")
		}
		if requestErr == nil {
			_, requestErr = file.Write(raw)
		}
		closeErr := file.Close()
		if requestErr != nil {
			_ = os.Remove(options.outputFile)
			return requestErr
		}
		if closeErr != nil {
			_ = os.Remove(options.outputFile)
			return closeErr
		}
		fmt.Fprintf(output, "Database export saved to %s\n", options.outputFile)
		return nil
	case "migrate":
		path := "/api/v1/managed-databases/" + url.PathEscape(options.id)
		var payload []byte
		var contentType string
		if options.sourceEnv != "" {
			source := os.Getenv(options.sourceEnv)
			if source == "" {
				return fmt.Errorf("source environment variable is empty")
			}
			payload, _ = json.Marshal(map[string]string{"connectionString": source})
			contentType, path = "application/json", path+"/migrations"
		} else {
			file, err := os.Open(options.sqlFile)
			if err != nil {
				return err
			}
			defer file.Close()
			payload, err = readBounded(file, databaseBodyLimit)
			if err != nil {
				return err
			}
			contentType, path = "application/sql", path+"/import.sql"
		}
		requestKey, err := randomSecret()
		if err != nil {
			return fmt.Errorf("create migration request key: %w", err)
		}
		raw, _, err := client.databaseRequest(ctx, token, http.MethodPost, path, contentType, payload,
			map[string]string{"Idempotency-Key": "db-migration-" + requestKey})
		if err != nil {
			return err
		}
		accepted, err := databaseEnvelope[struct {
			Migration databaseMigration `json:"migration"`
		}](raw)
		if err != nil || accepted.Migration.MigrationID == "" {
			if err == nil {
				err = fmt.Errorf("database API did not return a migration ID")
			}
			return err
		}
		fmt.Fprintf(output, "Migration %s accepted; waiting for completion.\n", accepted.Migration.MigrationID)
		completed, err := client.waitDatabaseMigration(ctx, token, options.id, accepted.Migration.MigrationID, pollInterval)
		if err != nil {
			return err
		}
		if completed.Result != nil && completed.SourceType == "connection" {
			_, err = fmt.Fprintf(output, "Migration completed: %d tables, %d rows.\n", completed.Result.TableCount, completed.Result.RowCount)
		} else {
			_, err = fmt.Fprintln(output, "Migration completed.")
		}
		return err
	default:
		return fmt.Errorf("unsupported db command %s", options.action)
	}
}
