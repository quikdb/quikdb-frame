package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/quikdb/quikdb-frame/internal/convert"
)

func commandArchive(o DeployOptions) error {
	if o.Mode == "frame" {
		return commandConvertedArchive(o)
	}
	config, version, err := readDeploymentConfiguration(o.Config, false)
	if err != nil {
		return err
	}
	o.explicitConfiguration = config
	o.manifestVersion = version
	if o.Name == "" {
		absolute, err := filepath.Abs(o.Source)
		if err != nil {
			return fmt.Errorf("invalid source directory")
		}
		o.Name = strings.ToLower(filepath.Base(absolute))
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`).MatchString(o.Name) {
		return fmt.Errorf("choose --name using 1–63 lowercase letters, numbers or hyphens")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	request, err := planAsIs(ctx, nil, "", o)
	if err != nil {
		return err
	}
	archive, err := packageSource(ctx, o.Source)
	if err != nil {
		return err
	}
	defer archive.Close()
	request.SourceSHA256 = archive.SHA256
	if o.DryRun {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"mode": "as-is", "source": "local archive", "applicationName": o.Name, "sha256": archive.SHA256, "size": archive.Size, "entries": archive.Entries, "subdirectory": o.Subdirectory, "manifestVersion": version, "configSource": request.Configuration["configSource"], "appType": request.Configuration["appType"], "port": request.Configuration["port"]})
	}
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	client := deployClientFactory()
	existing, err := client.List(ctx, token)
	if err != nil {
		return fmt.Errorf("deployment preflight failed: %w", err)
	}
	capability, err := client.request(ctx, token, "GET", "/source-capabilities", nil)
	var available struct {
		ArchiveDeployment    bool  `json:"archiveDeployment"`
		ArchiveUploadVersion int   `json:"archiveUploadVersion"`
		MaxArchiveBytes      int64 `json:"maxArchiveBytes"`
		IdempotentUploads    bool  `json:"idempotentUploads"`
	}
	if err != nil || json.Unmarshal(capability, &available) != nil || !available.ArchiveDeployment || available.ArchiveUploadVersion != 1 || available.MaxArchiveBytes != archiveUploadLimit || !available.IdempotentUploads {
		return fmt.Errorf("archive deployment is not active yet; use Git deployment or --dry-run to review your local archive")
	}
	byName := map[string]Deployment{}
	for _, deployment := range existing {
		if deployment.ApplicationName != o.Name {
			continue
		}
		if _, ok := byName[o.Name]; ok {
			return fmt.Errorf("multiple applications share this name; manage them by ID in Compute")
		}
		byName[o.Name] = deployment
	}
	if _, exists := byName[o.Name]; !exists {
		request.SourceID, err = client.uploadSource(ctx, token, archive)
		if err != nil {
			return err
		}
	}
	if !o.JSON {
		fmt.Printf("Deploying %s as-is from the reviewed local archive. Runtime values are configured separately.\n", o.Name)
	}
	result, err := deployApplication(ctx, client, token, request, byName, o.JSON)
	if err != nil {
		if request.SourceID != "" {
			_ = client.releaseSource(ctx, token, request.SourceID)
		}
		return err
	}
	if o.JSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"mode": "as-is", "deployment": result})
	}
	fmt.Printf("%s: %s — %s (ID: %s)\n", o.Name, result.Status, result.PublicURL, result.DeploymentID)
	return nil
}

// commandConvertedArchive is the deliberately narrow deployment-time conversion
// path. The user must choose --mode frame and name the source framework. Failed
// qualification never falls back to an as-is deployment or mutates the source.
func commandConvertedArchive(o DeployOptions) error {
	plan, err := convert.Run(convert.Options{SourcePath: o.Source, Framework: o.From})
	if err != nil {
		return err
	}
	if o.Name == "" {
		o.Name = plan.ProjectName + "-frame"
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`).MatchString(o.Name) {
		return fmt.Errorf("choose --name using 1–63 lowercase letters, numbers or hyphens")
	}
	review := map[string]interface{}{
		"mode": "frame", "qualification": "eligible", "converter": plan.Converter,
		"framework": plan.Framework, "applicationName": o.Name, "sourceDigest": plan.SourceDigest,
		"routes": len(plan.Routes), "staticMounts": len(plan.StaticMounts),
		"rollback": map[string]interface{}{
			"mode": plan.Rollback.Mode, "source": "unchanged original local source",
			"sourceDigest": plan.SourceDigest,
			"configuration": map[string]interface{}{
				"runtime": "nodejs", "runtimeVersion": plan.Original.NodeVersion,
				"framework": plan.Framework, "installCommand": plan.Original.InstallCommand,
				"buildCommand": "", "startCommand": plan.Original.StartCommand,
				"port": plan.Original.Port,
			},
		},
	}
	if o.DryRun {
		return json.NewEncoder(os.Stdout).Encode(review)
	}

	parent, err := os.MkdirTemp("", "quikdb-frame-deploy-")
	if err != nil {
		return fmt.Errorf("prepare private conversion workspace")
	}
	defer os.RemoveAll(parent)
	output := filepath.Join(parent, "application")
	plan, err = convert.Run(convert.Options{SourcePath: o.Source, Framework: o.From, OutputPath: output, Apply: true})
	if err != nil {
		return err
	}
	// The current archive deployment contract accepts one build context and its
	// root Dockerfile. The converter's native manifest keeps the richer service
	// path for future Frame-aware runners, so prepare a private root copy only for
	// this submission.
	dockerfile, err := os.ReadFile(filepath.Join(output, "services", "api", "Dockerfile"))
	if err != nil {
		return fmt.Errorf("converted deployment Dockerfile unavailable")
	}
	if err := os.WriteFile(filepath.Join(output, "Dockerfile"), dockerfile, 0600); err != nil {
		return fmt.Errorf("prepare converted deployment Dockerfile")
	}
	archive, err := packageSource(context.Background(), output)
	if err != nil {
		return err
	}
	defer archive.Close()
	request := DeployRequest{
		ApplicationName: o.Name,
		SourceSHA256:    archive.SHA256,
		Configuration: map[string]interface{}{
			"appType": "go", "framework": "quikdb-frame", "configSource": "dockerfile",
			"port": plan.Original.Port, "internalPort": plan.Original.Port,
			"deploymentApproach": "frame", "converter": plan.Converter,
			"originalSourceDigest": plan.SourceDigest,
		},
	}
	return submitArchive(o, request, archive, review)
}

func submitArchive(o DeployOptions, request DeployRequest, archive *sourceArchive, review map[string]interface{}) error {
	request.SourceSHA256 = archive.SHA256
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	client := deployClientFactory()
	existing, err := client.List(ctx, token)
	if err != nil {
		return fmt.Errorf("deployment preflight failed: %w", err)
	}
	capability, err := client.request(ctx, token, "GET", "/source-capabilities", nil)
	var available struct {
		ArchiveDeployment    bool  `json:"archiveDeployment"`
		ArchiveUploadVersion int   `json:"archiveUploadVersion"`
		MaxArchiveBytes      int64 `json:"maxArchiveBytes"`
		IdempotentUploads    bool  `json:"idempotentUploads"`
	}
	if err != nil || json.Unmarshal(capability, &available) != nil || !available.ArchiveDeployment || available.ArchiveUploadVersion != 1 || available.MaxArchiveBytes != archiveUploadLimit || !available.IdempotentUploads {
		return fmt.Errorf("archive deployment is not active yet; use --dry-run to review conversion or deploy the original through Git")
	}
	byName := map[string]Deployment{}
	for _, deployment := range existing {
		if deployment.ApplicationName != o.Name {
			continue
		}
		if _, ok := byName[o.Name]; ok {
			return fmt.Errorf("multiple applications share this name; manage them by ID in Compute")
		}
		byName[o.Name] = deployment
	}
	if _, exists := byName[o.Name]; !exists {
		request.SourceID, err = client.uploadSource(ctx, token, archive)
		if err != nil {
			return err
		}
	}
	if !o.JSON {
		fmt.Printf("Deploying %s after explicit %s conversion. The unchanged original source remains the rollback authority.\n", o.Name, review["converter"])
	}
	result, err := deployApplication(ctx, client, token, request, byName, o.JSON)
	if err != nil {
		if request.SourceID != "" {
			_ = client.releaseSource(ctx, token, request.SourceID)
		}
		return err
	}
	if o.JSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"mode": o.Mode, "qualification": review["qualification"], "converter": review["converter"], "rollback": review["rollback"], "deployment": result})
	}
	fmt.Printf("%s: %s — %s (ID: %s)\n", o.Name, result.Status, result.PublicURL, result.DeploymentID)
	return nil
}
