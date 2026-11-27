//go:build guardian_setup

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const serviceName = "YouTubeGuardian"

type installPaths struct {
	program string
	data    string
	config  string
}

func systemPaths() (installPaths, error) {
	program, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return installPaths{}, err
	}
	data, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		return installPaths{}, err
	}
	data = filepath.Join(data, serviceName)
	return installPaths{filepath.Join(program, serviceName), data, filepath.Join(data, "guardian.json")}, nil
}

const dataACL = "O:BAG:BAD:P(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)(A;OICI;0x1301bf;;;LS)"

func createDataDirectory(path string) error {
	sd, err := windows.SecurityDescriptorFromString(dataACL)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.CreateDirectory(name, &windows.SecurityAttributes{
		Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd,
	})
}

func plainPath(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attr, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if attr&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("unexpected link or reparse point at %s", path)
	}
	return nil
}

func verifyDataDirectory(path string) error {
	if err := verifyDataItem(path); err != nil {
		return err
	}
	items, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.IsDir() {
			return fmt.Errorf("unexpected directory in Guardian data: %s", item.Name())
		}
		if err := verifyDataItem(filepath.Join(path, item.Name())); err != nil {
			return err
		}
	}
	return nil
}

func verifyDataItem(path string) error {
	if err := plainPath(path); err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	trusted := func(sid *windows.SID) bool {
		return sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) ||
			sid.IsWellKnown(windows.WinLocalSystemSid) ||
			sid.IsWellKnown(windows.WinLocalServiceSid)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	// Legacy PowerShell installs can leave files owned by the installing administrator.
	// Accept a different owner only when Windows confirms it is the current elevated user.
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if owner == nil || (!trusted(owner) && !owner.Equals(tokenUser.User.Sid)) {
		return fmt.Errorf("unexpected owner at %s", path)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return fmt.Errorf("missing access controls at %s", path)
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !trusted((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			return fmt.Errorf("unexpected access rules at %s", path)
		}
	}
	return nil
}

func (p installPaths) hasConfiguration() (bool, error) {
	err := plainPath(p.data)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := verifyDataDirectory(p.data); err != nil {
		return false, err
	}
	if err := plainPath(p.config); err != nil {
		return false, fmt.Errorf("existing data needs a valid guardian.json: %w", err)
	}
	return true, nil
}
