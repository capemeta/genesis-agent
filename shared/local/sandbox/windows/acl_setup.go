//go:build windows

package windowssandbox

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetNamedSecurityInfoW = windows.NewLazySystemDLL("advapi32.dll").NewProc("GetNamedSecurityInfoW")
	procSetNamedSecurityInfoW = windows.NewLazySystemDLL("advapi32.dll").NewProc("SetNamedSecurityInfoW")
)

const (
	writeAllowMask = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE | 0x00000040 // 0x40 is FILE_DELETE_CHILD
	readDenyMask   = windows.FILE_GENERIC_READ | windows.GENERIC_READ
)

// GetWorkspaceCapabilitySID generates a stable deterministic capability SID for a given workspace path.
func GetWorkspaceCapabilitySID(workspacePath string) (string, error) {
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		absPath = workspacePath
	}
	// Normalize path separators and lower case for stability across spellings
	normalized := strings.ToLower(filepath.Clean(absPath))
	
	// Hash the normalized path
	hash := sha256.Sum256([]byte(normalized))
	
	// Convert hash bytes to four 32-bit unsigned integers
	r1 := binary.LittleEndian.Uint32(hash[0:4])
	r2 := binary.LittleEndian.Uint32(hash[4:8])
	r3 := binary.LittleEndian.Uint32(hash[8:12])
	r4 := binary.LittleEndian.Uint32(hash[12:16])
	
	// Return a SID in the S-1-5-21 (local/domain authority) namespace
	return fmt.Sprintf("S-1-5-21-%d-%d-%d-%d", r1, r2, r3, r4), nil
}

func getNamedSecurityInfo(path string) (uintptr, uintptr, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var pDacl uintptr
	var pSecDesc uintptr
	// SE_FILE_OBJECT = 1
	// DACL_SECURITY_INFORMATION = 4
	r1, _, _ := procGetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		1, // SE_FILE_OBJECT
		4, // DACL_SECURITY_INFORMATION
		0, 0,
		uintptr(unsafe.Pointer(&pDacl)),
		0,
		uintptr(unsafe.Pointer(&pSecDesc)),
	)
	if r1 != 0 {
		return 0, 0, fmt.Errorf("GetNamedSecurityInfoW failed with error code %d", r1)
	}
	return pDacl, pSecDesc, nil
}

func setNamedSecurityInfo(path string, pDacl uintptr) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// SE_FILE_OBJECT = 1
	// DACL_SECURITY_INFORMATION = 4
	r1, _, _ := procSetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		1, // SE_FILE_OBJECT
		4, // DACL_SECURITY_INFORMATION
		0, 0,
		pDacl,
		0,
	)
	if r1 != 0 {
		return fmt.Errorf("SetNamedSecurityInfoW failed with error code %d", r1)
	}
	return nil
}

func grantModifyAccessLowLevel(path string, sid *windows.SID) error {
	pDacl, pSecDesc, err := getNamedSecurityInfo(path)
	if err != nil {
		return err
	}
	defer windows.LocalFree(windows.Handle(pSecDesc))

	entries := []EXPLICIT_ACCESS_W{
		{
			grfAccessPermissions: writeAllowMask,
			grfAccessMode:        2, // SET_ACCESS
			grfInheritance:       windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE,
			Trustee: TRUSTEE_W{
				pMultipleTrustee:         0,
				MultipleTrusteeOperation: 0,
				TrusteeForm:              0, // TRUSTEE_IS_SID
				TrusteeType:              0, // TRUSTEE_IS_UNKNOWN
				ptstrName:                uintptr(unsafe.Pointer(sid)),
			},
		},
	}

	var newAcl uintptr
	r1, _, _ := procSetEntriesInAclW.Call(
		uintptr(len(entries)),
		uintptr(unsafe.Pointer(&entries[0])),
		pDacl,
		uintptr(unsafe.Pointer(&newAcl)),
	)
	if r1 != 0 {
		return fmt.Errorf("SetEntriesInAclW failed: %d", r1)
	}
	defer windows.LocalFree(windows.Handle(newAcl))

	return setNamedSecurityInfo(path, newAcl)
}

func denyReadAccessLowLevel(path string, sid *windows.SID) error {
	pDacl, pSecDesc, err := getNamedSecurityInfo(path)
	if err != nil {
		return err
	}
	defer windows.LocalFree(windows.Handle(pSecDesc))

	entries := []EXPLICIT_ACCESS_W{
		{
			grfAccessPermissions: readDenyMask,
			grfAccessMode:        3, // DENY_ACCESS
			grfInheritance:       windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE,
			Trustee: TRUSTEE_W{
				pMultipleTrustee:         0,
				MultipleTrusteeOperation: 0,
				TrusteeForm:              0, // TRUSTEE_IS_SID
				TrusteeType:              0, // TRUSTEE_IS_UNKNOWN
				ptstrName:                uintptr(unsafe.Pointer(sid)),
			},
		},
	}

	var newAcl uintptr
	r1, _, _ := procSetEntriesInAclW.Call(
		uintptr(len(entries)),
		uintptr(unsafe.Pointer(&entries[0])),
		pDacl,
		uintptr(unsafe.Pointer(&newAcl)),
	)
	if r1 != 0 {
		return fmt.Errorf("SetEntriesInAclW failed: %d", r1)
	}
	defer windows.LocalFree(windows.Handle(newAcl))

	return setNamedSecurityInfo(path, newAcl)
}

// ApplyWorkspaceACLs grants modify access to writable roots, and denies read access to unreadable paths.
func ApplyWorkspaceACLs(workspacePath string, writables []string, unreadables []string) error {
	sidStr, err := GetWorkspaceCapabilitySID(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to generate capability SID: %w", err)
	}
	sid, err := windows.StringToSid(sidStr)
	if err != nil {
		return fmt.Errorf("failed to parse capability SID: %w", err)
	}

	// 1. Grant modify access to writable roots
	for _, path := range writables {
		if path == "" {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}
		
		err = grantModifyAccessLowLevel(absPath, sid)
		if err != nil {
			return fmt.Errorf("grant modify failed for %s: %w", absPath, err)
		}
	}

	// 2. Deny read access to unreadable paths
	for _, path := range unreadables {
		if path == "" {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}
		
		err = denyReadAccessLowLevel(absPath, sid)
		if err != nil {
			return fmt.Errorf("deny read failed for %s: %w", absPath, err)
		}
	}

	return nil
}

// LookupUserSID resolves a local account name to its windows SID pointer
func LookupUserSID(username string) (*windows.SID, error) {
	sid, _, _, err := windows.LookupSID("", username)
	if err != nil {
		return nil, err
	}
	return sid, nil
}

// ApplyWorkspaceACLsForUser grants modify access to writable roots and the workspace itself for a specific user account.
func ApplyWorkspaceACLsForUser(workspacePath string, username string, writables []string, unreadables []string) error {
	sid, err := LookupUserSID(username)
	if err != nil {
		return fmt.Errorf("failed to lookup SID for user %q: %w", username, err)
	}

	// 1. Grant modify access to the workspace root itself so the user can navigate it
	absWorkspace, err := filepath.Abs(workspacePath)
	if err == nil {
		_ = grantModifyAccessLowLevel(absWorkspace, sid)
	}

	// 2. Grant modify access to writable roots
	for _, path := range writables {
		if path == "" {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}

		err = grantModifyAccessLowLevel(absPath, sid)
		if err != nil {
			return fmt.Errorf("grant modify failed for %s: %w", absPath, err)
		}
	}

	// 3. Deny read access to unreadable paths
	for _, path := range unreadables {
		if path == "" {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}

		err = denyReadAccessLowLevel(absPath, sid)
		if err != nil {
			return fmt.Errorf("deny read failed for %s: %w", absPath, err)
		}
	}

	return nil
}


