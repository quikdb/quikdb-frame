package deploy

import (
	"golang.org/x/sys/windows"
	"os"
	"runtime"
	"unsafe"
)

// CREATE_NEW prevents replacement; the protected DACL is installed at creation,
// before any secret is fetched, including in directories with broad inherited ACLs.
func createPrivateExport(path string) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: sd}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, &attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	runtime.KeepAlive(sd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
