package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateGoModule(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "go.mod")
	if err := os.WriteFile(filename, []byte("module example.com/team/app\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGoModule(filename, "example.com/team/app"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGoModule(filename, "other"); err == nil {
		t.Fatal("accepted a mismatched module")
	}
	link := filepath.Join(directory, "linked.mod")
	if err := os.Symlink(filename, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGoModule(link, "example.com/team/app"); err == nil {
		t.Fatal("accepted a symlinked project module")
	}
}
