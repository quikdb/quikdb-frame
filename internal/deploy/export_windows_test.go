package deploy

import (
	"golang.org/x/sys/windows"
	"io"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestArchiveWindowsPrivateFileAllowsRewindAndBlocksInheritedAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.tar.gz")
	file, err := createPrivateArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("synthetic source"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	bytes, err := io.ReadAll(file)
	if err != nil || string(bytes) != "synthetic source" {
		t.Fatal("private archive is not readable for upload")
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("archive inherited broad permissions")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("archive permits an unexpected principal")
	}
	if replacement, err := createPrivateArchive(path); err == nil {
		replacement.Close()
		t.Fatal("archive overwrote an existing file")
	}
}

func TestExportWindowsBlocksInheritedAccessAndAllowsOnlyCurrentUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.env")
	file, err := createPrivateExport(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("SYNTHETIC=fixture\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("inherited ACL was not blocked: %v", err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatalf("export has an unexpected ACL: %v", err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err = windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || sid.String() != user.User.Sid.String() {
		t.Fatal("export permits a principal other than its user")
	}
}
