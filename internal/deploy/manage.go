package deploy

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Decode only public configuration fields; environment values never enter these results.
type ManagementConfig struct {
	RepositoryBranch  *string           `json:"repositoryBranch,omitempty"`
	BuildCommand      *string           `json:"buildCommand,omitempty"`
	StartCommand      *string           `json:"startCommand,omitempty"`
	InstallCommand    *string           `json:"installCommand,omitempty"`
	Port              *int              `json:"port,omitempty"`
	HealthCheck       *string           `json:"healthCheck,omitempty"`
	Subdomain         *string           `json:"subdomain,omitempty"`
	AutoDeployEnabled *bool             `json:"autoDeployEnabled,omitempty"`
	AutoDeployBranch  *string           `json:"autoDeployBranch,omitempty"`
	Resources         *ResourceSettings `json:"resources,omitempty"`
}
type ResourceSettings struct {
	CPU     float64 `json:"cpuCores"`
	RAM     int     `json:"ram"`
	Storage int     `json:"storage,omitempty"`
}
type managementOptions struct {
	command, action, id, key, file, valueFile, output, level, phase, provider string
	json, yes, stdin, public, follow                                          bool
	limit, replica, version, ram                                              int
	cpu                                                                       float64
}

var resourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var objectID = regexp.MustCompile(`^[a-fA-F0-9]{24}$`)
var envKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,254}$`)

// Accept flags before or after IDs, but only flags defined for this command.
func managementArgs(f *flag.FlagSet, args []string) ([]string, error) {
	var options, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name, _, assigned := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		option := f.Lookup(name)
		if option == nil {
			return nil, fmt.Errorf("unknown option --%s", name)
		}
		options = append(options, arg)
		boolean, isBoolean := option.Value.(interface{ IsBoolFlag() bool })
		if !assigned && !(isBoolean && boolean.IsBoolFlag()) {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("missing value for %s", arg)
			}
			i++
			options = append(options, args[i])
		}
	}
	return append(options, positional...), nil
}

func parseManagement(command string, args []string) (managementOptions, error) {
	o := managementOptions{command: command, replica: -1, version: -1}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.BoolVar(&o.json, "json", false, "JSON output")
	want := 1
	switch command {
	case "status", "inspect", "history":
		if command == "status" {
			want = -1
		}
	case "stop", "restart", "redeploy", "wake":
	case "delete":
		f.BoolVar(&o.yes, "yes", false, "Confirm deletion of this ID")
	case "rollback":
		f.IntVar(&o.version, "version", -1, "Retained previous version index, 0 = most recent")
	case "logs":
		f.IntVar(&o.limit, "limit", 100, "Up to 500 recent records")
		f.IntVar(&o.replica, "replica", -1, "Replica index")
		f.StringVar(&o.level, "level", "", "info, warn, error or debug")
		f.StringVar(&o.phase, "phase", "", "build, deploy or runtime")
		f.BoolVar(&o.follow, "follow", false, "Poll new records for up to 30 minutes")
	case "config", "resources", "env", "domains":
		if len(args) == 0 {
			return o, fmt.Errorf("%s requires a subcommand", command)
		}
		o.action, args = args[0], args[1:]
		switch command + "/" + o.action {
		case "config/get", "resources/get", "env/list", "domains/list":
		case "config/set":
			f.StringVar(&o.file, "file", "", "JSON configuration patch")
		case "resources/set":
			f.Float64Var(&o.cpu, "cpu", 0, "Positive vCPU allocation")
			f.IntVar(&o.ram, "ram", 0, "Positive memory in MB")
		case "env/set":
			want = 2
			f.StringVar(&o.valueFile, "value-file", "", "Read value from this file, never command arguments")
			f.BoolVar(&o.stdin, "stdin", false, "Read value from stdin")
			f.BoolVar(&o.public, "public", false, "Store an explicitly nonsecret value")
		case "env/remove", "domains/remove", "domains/verify", "domains/repair":
			want = 2
		case "env/export":
			f.StringVar(&o.output, "output", "", "Create private output file; existing files refused")
		case "domains/add":
			want = 2
			f.StringVar(&o.provider, "provider-connection", "", "Existing dashboard provider connection ID")
		default:
			return o, fmt.Errorf("unsupported %s subcommand %s", command, o.action)
		}
	default:
		return o, fmt.Errorf("unsupported management command %s", command)
	}
	reordered, err := managementArgs(f, args)
	if err != nil {
		return o, err
	}
	if err = f.Parse(reordered); err != nil {
		return o, err
	}
	positions := f.Args()
	if (want >= 0 && len(positions) != want) || (want == -1 && len(positions) > 1) {
		return o, fmt.Errorf("%s requires %d ID/key arguments (status allows zero or one ID)", command, want)
	}
	if len(positions) > 0 {
		o.id = positions[0]
		if !resourceID.MatchString(o.id) {
			return o, fmt.Errorf("use an exact deployment ID, not a name, URL or path")
		}
	}
	if len(positions) > 1 {
		o.key = positions[1]
	}
	if command == "delete" && !o.yes {
		return o, fmt.Errorf("delete requires an exact deployment ID and --yes")
	}
	if command == "rollback" && o.version < 0 {
		return o, fmt.Errorf("rollback requires --version with a non-negative integer")
	}
	if command == "logs" && (o.limit < 1 || o.limit > 500 || o.replica < -1 ||
		(o.level != "" && !strings.Contains("|info|warn|error|debug|", "|"+o.level+"|")) ||
		(o.phase != "" && !strings.Contains("|build|deploy|runtime|", "|"+o.phase+"|"))) {
		return o, fmt.Errorf("invalid log limit, replica, level or phase")
	}
	if command == "config" && o.action == "set" && o.file == "" {
		return o, fmt.Errorf("config set requires --file")
	}
	if command == "resources" && o.action == "set" {
		invalid := math.IsNaN(o.cpu) || math.IsInf(o.cpu, 0) || (o.cpu == 0 && o.ram == 0)
		f.Visit(func(option *flag.Flag) {
			if option.Name == "cpu" && o.cpu <= 0 || option.Name == "ram" && o.ram <= 0 {
				invalid = true
			}
		})
		if invalid {
			return o, fmt.Errorf("supply positive finite --cpu and/or integer --ram")
		}
	}
	if command == "env" && (o.action == "set" || o.action == "remove") && !envKey.MatchString(o.key) {
		return o, fmt.Errorf("invalid environment key")
	}
	if command == "env" && o.action == "set" && ((o.valueFile == "") == !o.stdin) {
		return o, fmt.Errorf("env set requires exactly one of --value-file or --stdin")
	}
	if command == "env" && o.action == "export" && o.output == "" {
		return o, fmt.Errorf("env export requires --output; values are never printed")
	}
	if command == "domains" && o.action != "list" && o.action != "add" && !resourceID.MatchString(o.key) {
		return o, fmt.Errorf("invalid domain ID")
	}
	if command == "domains" && o.action == "add" {
		if len(o.key) > 253 || !regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$`).MatchString(o.key) {
			return o, fmt.Errorf("invalid domain hostname")
		}
		if o.provider != "" && !objectID.MatchString(o.provider) {
			return o, fmt.Errorf("invalid provider connection ID")
		}
	}
	return o, nil
}

func Manage(command string, args []string) error {
	o, err := parseManagement(command, args)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	duration := time.Minute
	if o.follow {
		duration = 30 * time.Minute
	}
	ctx, timeout := context.WithTimeout(ctx, duration)
	defer timeout()
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	return executeManagement(ctx, deployClientFactory(), token, o, os.Stdin, os.Stdout)
}

func emitManagement(w io.Writer, jsonOutput bool, value interface{}) error {
	encoder := json.NewEncoder(w)
	if !jsonOutput {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("input exceeds size limit")
	}
	return raw, nil
}
func readConfigPatch(file string) (map[string]interface{}, error) {
	input, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	raw, err := readBounded(input, 65536)
	if err != nil {
		return nil, err
	}
	var patch map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err = decoder.Decode(&patch); err != nil || len(patch) == 0 {
		return nil, fmt.Errorf("configuration must be a nonempty JSON object")
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("configuration must contain exactly one JSON object")
	}
	for key, value := range patch {
		switch key {
		case "port":
			n, ok := value.(float64)
			if !ok || n < 1 || n > 65535 || n != float64(int(n)) {
				return nil, fmt.Errorf("port must be an integer 1–65535")
			}
		case "autoDeployEnabled":
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("autoDeployEnabled must be boolean")
			}
		case "buildCommand", "startCommand", "installCommand":
			if value == nil {
				continue
			}
			fallthrough
		case "repositoryBranch", "healthCheck", "subdomain", "autoDeployBranch":
			s, ok := value.(string)
			if !ok || len(s) > 4096 {
				return nil, fmt.Errorf("invalid configuration field %s", key)
			}
		default:
			return nil, fmt.Errorf("unsupported configuration field %s; use env commands for environment values", key)
		}
	}
	return patch, nil
}

func executeManagement(ctx context.Context, c *APIClient, token string, o managementOptions, stdin io.Reader, stdout io.Writer) error {
	if o.command == "status" && o.id == "" {
		entries, err := c.List(ctx, token)
		if err != nil {
			return err
		}
		if o.json {
			return emitManagement(stdout, true, entries)
		}
		if len(entries) == 0 {
			_, err = fmt.Fprintln(stdout, "No deployments found.")
			return err
		}
		for _, entry := range entries {
			if _, err = fmt.Fprintf(stdout, "%-25s %-15s %s (ID: %s)\n", entry.ApplicationName, entry.Status, entry.PublicURL, entry.DeploymentID); err != nil {
				return err
			}
		}
		return nil
	}
	base := "/api/v1/deployment/" + o.id
	if o.command == "logs" {
		return managementLogs(ctx, c, token, base, o, stdout)
	}
	if o.command == "env" {
		return managementEnv(ctx, c, token, o, stdin, stdout)
	}
	if o.command == "domains" {
		return managementDomains(ctx, c, token, o, stdout)
	}
	var patch interface{}
	if o.command == "config" && o.action == "set" {
		var err error
		patch, err = readConfigPatch(o.file)
		if err != nil {
			return err
		}
	}
	if o.command == "resources" && o.action == "set" {
		values := map[string]interface{}{}
		if o.cpu > 0 {
			values["cpuCores"] = o.cpu
		}
		if o.ram > 0 {
			values["ram"] = o.ram
		}
		patch = values
	}
	// Resolve the owned ID before a lifecycle/settings mutation. Server authorizes again.
	d, err := c.Get(ctx, token, o.id)
	if err != nil {
		return err
	}
	switch o.command {
	case "status", "inspect":
		return emitManagement(stdout, o.json, d)
	case "config", "resources":
		if o.action == "get" {
			if o.command == "resources" {
				if d.Configuration == nil {
					return fmt.Errorf("resource configuration unavailable")
				}
				return emitManagement(stdout, o.json, d.Configuration.Resources)
			}
			if d.Configuration == nil {
				return fmt.Errorf("deployment configuration unavailable")
			}
			configuration := *d.Configuration
			configuration.RepositoryBranch, configuration.Subdomain = &d.RepositoryBranch, &d.Subdomain
			if d.AutoDeploy != nil {
				configuration.AutoDeployEnabled, configuration.AutoDeployBranch = &d.AutoDeploy.Enabled, &d.AutoDeploy.Branch
			}
			return emitManagement(stdout, o.json, configuration)
		}
		method := http.MethodPut
		if o.command == "resources" {
			method = http.MethodPatch
		}
		response, err := c.jsonRequest(ctx, token, method, base+"/"+o.command, patch)
		if err != nil {
			return err
		}
		return emitManagement(stdout, o.json, response)
	case "history":
		response, err := c.jsonRequest(ctx, token, http.MethodGet, base+"/history", nil)
		if err != nil {
			return err
		}
		var result struct {
			DeploymentID string `json:"deploymentId"`
			History      []struct {
				Status     string  `json:"status"`
				CommitHash string  `json:"commitHash"`
				DeployedAt string  `json:"deployedAt"`
				ReplacedAt *string `json:"replacedAt"`
			} `json:"history"`
		}
		if err := json.Unmarshal(response.Data, &result); err != nil || result.DeploymentID != o.id || result.History == nil {
			return fmt.Errorf("invalid deployment history response")
		}
		return emitManagement(stdout, o.json, map[string]interface{}{"deploymentId": o.id, "history": result.History,
			"rollbackIndexConvention": "0 = most recent retained previous version; timeline rows are not rollback indices"})
	default:
		method, path := http.MethodPost, base+"/"+o.command
		patch = struct{}{}
		if o.command == "delete" {
			method, path = http.MethodDelete, base
			patch = nil
		}
		if o.command == "rollback" {
			patch = map[string]int{"versionIndex": o.version}
		}
		response, err := c.jsonRequest(ctx, token, method, path, patch)
		if err != nil {
			return fmt.Errorf("%s failed or response was lost; inspect deployment %s before retrying: %w", o.command, o.id, err)
		}
		return emitManagement(stdout, o.json, response)
	}
}

type managementLog struct {
	ID        string `json:"logId"`
	Replica   int    `json:"replicaIndex"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Phase     string `json:"phase"`
	Message   string `json:"message"`
}

var managementLogInterval = 2 * time.Second

func managementLogs(ctx context.Context, c *APIClient, token, base string, o managementOptions, stdout io.Writer) error {
	query := url.Values{"limit": {fmt.Sprint(o.limit)}}
	if o.replica >= 0 {
		query.Set("replicaIndex", fmt.Sprint(o.replica))
	}
	if o.level != "" {
		query.Set("level", o.level)
	}
	if o.phase != "" {
		query.Set("phase", o.phase)
	}
	seen := map[string]bool{}
	var order []string
	for {
		response, err := c.jsonRequest(ctx, token, http.MethodGet, base+"/logs?"+query.Encode(), nil)
		if err != nil {
			return err
		}
		var result struct {
			DeploymentID string          `json:"deploymentId"`
			Logs         []managementLog `json:"logs"`
		}
		if err = json.Unmarshal(response.Data, &result); err != nil || result.DeploymentID != o.id || result.Logs == nil {
			return fmt.Errorf("invalid deployment logs response")
		}
		if !o.follow {
			return emitManagement(stdout, o.json, result)
		}
		for i := len(result.Logs) - 1; i >= 0; i-- {
			entry := result.Logs[i]
			if entry.ID == "" {
				return fmt.Errorf("log follow requires stable log IDs")
			}
			if seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			order = append(order, entry.ID)
			if len(order) > 10000 {
				delete(seen, order[0])
				order = order[1:]
			}
			if err = emitManagement(stdout, o.json, entry); err != nil {
				return err
			}
		}
		timer := time.NewTimer(managementLogInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type managementEnvEntry struct {
	ID     string `json:"_id"`
	Key    string `json:"key"`
	Secret bool   `json:"isSecret"`
}

func managementEnv(ctx context.Context, c *APIClient, token string, o managementOptions, stdin io.Reader, stdout io.Writer) error {
	var value []byte
	if o.action == "set" {
		input := stdin
		if o.valueFile != "" {
			file, err := os.Open(o.valueFile)
			if err != nil {
				return fmt.Errorf("unable to read environment value file")
			}
			defer file.Close()
			input = file
		}
		var err error
		value, err = readBounded(input, 65536)
		if err != nil || len(value) == 0 || !utf8.Valid(value) || strings.ContainsRune(string(value), 0) {
			return fmt.Errorf("environment value must contain 1–65536 UTF-8 bytes without NUL")
		}
	}
	d, err := c.Get(ctx, token, o.id)
	if err != nil {
		return err
	}
	if !objectID.MatchString(d.ApplicationID) {
		return fmt.Errorf("deployment detail lacks a valid application document ID")
	}
	base := "/api/v1/applications/" + d.ApplicationID + "/env"
	if o.action == "export" {
		// Reserve a new private regular file before fetching plaintext; never overwrite.
		file, err := createPrivateExport(o.output)
		if err != nil {
			return fmt.Errorf("unable to create a new private export file")
		}
		ok := false
		defer func() {
			file.Close()
			if !ok {
				os.Remove(o.output)
			}
		}()
		raw, err := c.rawRequest(ctx, token, http.MethodGet, base+"/export", nil)
		if err != nil {
			return fmt.Errorf("environment export failed")
		}
		if _, err = file.Write(raw); err != nil {
			return fmt.Errorf("unable to write environment export")
		}
		if err = file.Close(); err != nil {
			return err
		}
		ok = true
		return emitManagement(stdout, o.json, map[string]interface{}{"deploymentId": o.id, "output": o.output, "containsSecrets": true})
	}
	response, err := c.jsonRequest(ctx, token, http.MethodGet, base, nil)
	if err != nil {
		return err
	}
	var entries []managementEnvEntry
	if err = json.Unmarshal(response.Data, &entries); err != nil || entries == nil {
		return fmt.Errorf("invalid environment response")
	}
	if o.action == "list" {
		return emitManagement(stdout, o.json, entries)
	}
	var matching *managementEnvEntry
	for i := range entries {
		if entries[i].Key == o.key {
			if matching != nil {
				return fmt.Errorf("duplicate environment key; manage it in Compute")
			}
			matching = &entries[i]
		}
	}
	method, path := http.MethodPost, base
	var payload interface{} = map[string]interface{}{"key": o.key, "value": string(value), "isSecret": !o.public}
	if matching != nil {
		if !objectID.MatchString(matching.ID) {
			return fmt.Errorf("invalid environment record ID")
		}
		method, path = http.MethodPut, base+"/"+matching.ID
	}
	if o.action == "remove" {
		if matching == nil {
			return fmt.Errorf("environment key not found")
		}
		method, payload = http.MethodDelete, nil
	}
	if _, err = c.jsonRequest(ctx, token, method, path, payload); err != nil {
		return fmt.Errorf("environment %s failed; inspect key %s before retrying", o.action, o.key)
	}
	secret := !o.public
	if o.action == "remove" {
		secret = matching.Secret
	}
	return emitManagement(stdout, o.json, map[string]interface{}{"deploymentId": o.id, "key": o.key, "action": o.action, "isSecret": secret, "requiresRedeploy": true})
}

func managementDomains(ctx context.Context, c *APIClient, token string, o managementOptions, stdout io.Writer) error {
	if _, err := c.Get(ctx, token, o.id); err != nil {
		return err
	}
	base := "/api/v1/deployments/" + o.id + "/domains"
	method, path := http.MethodGet, base
	var payload interface{}
	switch o.action {
	case "add":
		method = http.MethodPost
		values := map[string]string{"domain": strings.ToLower(o.key)}
		if o.provider != "" {
			values["providerConnectionId"] = o.provider
		}
		payload = values
	case "remove":
		method, path = http.MethodDelete, base+"/"+o.key
	case "verify", "repair":
		method, path, payload = http.MethodPost, base+"/"+o.key+"/"+o.action, struct{}{}
	}
	response, err := c.jsonRequest(ctx, token, method, path, payload)
	if err != nil {
		return err
	}
	return emitManagement(stdout, o.json, response)
}
