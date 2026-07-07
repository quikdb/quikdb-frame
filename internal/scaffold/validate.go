package scaffold

import (
	"fmt"
	"regexp"
)

// validName allows letters, numbers, hyphens, underscores.
// Must start with a letter or number. Max 50 chars.
// Rejects path traversal attempts like ../../etc or /absolute/path.
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,49}$`)

func validateName(name, label string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid %s %q: use letters, numbers, hyphens, and underscores only (no slashes or dots)", label, name)
	}
	return nil
}
