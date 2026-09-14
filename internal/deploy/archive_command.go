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
)

func commandArchive(o DeployOptions) error {
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
		return err
	}
	if o.JSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"mode": "as-is", "deployment": result})
	}
	fmt.Printf("%s: %s — %s (ID: %s)\n", o.Name, result.Status, result.PublicURL, result.DeploymentID)
	return nil
}
