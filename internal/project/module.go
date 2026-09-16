package project

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ValidateGoModule verifies that generated services resolve shared packages
// through the one project-root module declared by quikdb.yaml.
func ValidateGoModule(filename, expected string) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("read project Go module: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("project Go module must be a regular file")
	}
	if info.Size() > maxManifestBytes {
		return fmt.Errorf("project Go module exceeds %d bytes", maxManifestBytes)
	}
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("read project Go module: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "module" {
			continue
		}
		module := fields[1]
		if unquoted, err := strconv.Unquote(module); err == nil {
			module = unquoted
		}
		if module != expected {
			return fmt.Errorf("project go.mod module %q does not match quikdb.yaml goModule %q", module, expected)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read project Go module: %w", err)
	}
	return fmt.Errorf("project go.mod has no module directive")
}
