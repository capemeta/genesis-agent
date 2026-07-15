//go:build windows

package windowssandbox

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	restrictedTokenDisableMaxPrivilege = 0x00000001
	restrictedTokenLua                 = 0x00000004

	restrictedTokenAccess = windows.TOKEN_ASSIGN_PRIMARY |
		windows.TOKEN_DUPLICATE |
		windows.TOKEN_QUERY |
		windows.TOKEN_ADJUST_DEFAULT |
		windows.TOKEN_ADJUST_SESSIONID
)

var (
	procCreateRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")
	procSetEntriesInAclW      = windows.NewLazySystemDLL("advapi32.dll").NewProc("SetEntriesInAclW")
)

type EXPLICIT_ACCESS_W struct {
	grfAccessPermissions uint32
	grfAccessMode        uint32 // GRANT_ACCESS = 1
	grfInheritance       uint32
	Trustee              TRUSTEE_W
}

type TRUSTEE_W struct {
	pMultipleTrustee         uintptr
	MultipleTrusteeOperation uint32
	TrusteeForm              uint32 // TRUSTEE_IS_SID = 0
	TrusteeType              uint32 // TRUSTEE_IS_UNKNOWN = 0
	ptstrName                uintptr // PSID
}

func getLogonSID(token windows.Token) (*windows.SID, error) {
	var regSize uint32
	err := windows.GetTokenInformation(token, windows.TokenGroups, nil, 0, &regSize)
	if err != nil && err != windows.ERROR_INSUFFICIENT_BUFFER {
		return nil, err
	}
	buf := make([]byte, regSize)
	err = windows.GetTokenInformation(token, windows.TokenGroups, &buf[0], regSize, &regSize)
	if err != nil {
		return nil, err
	}
	tokenGroups := (*windows.Tokengroups)(unsafe.Pointer(&buf[0]))

	// Extract Logon SID
	groups := (*[1 << 20]windows.SIDAndAttributes)(unsafe.Pointer(&tokenGroups.Groups[0]))[:tokenGroups.GroupCount:tokenGroups.GroupCount]
	for _, g := range groups {
		if (g.Attributes & windows.SE_GROUP_LOGON_ID) == windows.SE_GROUP_LOGON_ID {
			return g.Sid.Copy()
		}
	}
	return nil, fmt.Errorf("Logon SID not found in token")
}

func getEveryoneSID() (*windows.SID, error) {
	return windows.CreateWellKnownSid(windows.WinWorldSid)
}

func setDefaultDacl(token windows.Token, sids []*windows.SID) error {
	if len(sids) == 0 {
		return nil
	}
	var entries []EXPLICIT_ACCESS_W
	for _, sid := range sids {
		entries = append(entries, EXPLICIT_ACCESS_W{
			grfAccessPermissions: windows.GENERIC_ALL,
			grfAccessMode:        1, // GRANT_ACCESS
			grfInheritance:       0,
			Trustee: TRUSTEE_W{
				pMultipleTrustee:         0,
				MultipleTrusteeOperation: 0,
				TrusteeForm:              0, // TRUSTEE_IS_SID
				TrusteeType:              0, // TRUSTEE_IS_UNKNOWN
				ptstrName:                uintptr(unsafe.Pointer(sid)),
			},
		})
	}

	var newAcl uintptr
	r1, _, _ := procSetEntriesInAclW.Call(
		uintptr(len(entries)),
		uintptr(unsafe.Pointer(&entries[0])),
		0,
		uintptr(unsafe.Pointer(&newAcl)),
	)
	if r1 != 0 {
		return fmt.Errorf("SetEntriesInAclW failed: %d", r1)
	}
	defer windows.LocalFree(windows.Handle(newAcl))

	type TOKEN_DEFAULT_DACL struct {
		DefaultDacl uintptr
	}
	info := TOKEN_DEFAULT_DACL{DefaultDacl: newAcl}

	err := windows.SetTokenInformation(
		token,
		windows.TokenDefaultDacl,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return fmt.Errorf("SetTokenInformation default DACL failed: %w", err)
	}
	return nil
}

func createRestrictedPrimaryToken(capabilitySids []string) (windows.Token, error) {
	var current windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), restrictedTokenAccess, &current); err != nil {
		return 0, err
	}
	defer current.Close()

	var restricted windows.Token
	flags := uintptr(restrictedTokenDisableMaxPrivilege | restrictedTokenLua)

	var restrictingSids []windows.SIDAndAttributes
	if len(capabilitySids) > 0 {
		flags |= 0x00000008 // WRITE_RESTRICTED (0x08)

		// 1. Add capability SIDs
		for _, sidStr := range capabilitySids {
			sid, err := windows.StringToSid(sidStr)
			if err != nil {
				return 0, fmt.Errorf("invalid capability SID %q: %w", sidStr, err)
			}
			restrictingSids = append(restrictingSids, windows.SIDAndAttributes{
				Sid:        sid,
				Attributes: 0,
			})
		}

		// 2. Add Logon SID
		logonSid, err := getLogonSID(current)
		if err != nil {
			return 0, fmt.Errorf("failed to get logon SID: %w", err)
		}
		restrictingSids = append(restrictingSids, windows.SIDAndAttributes{
			Sid:        logonSid,
			Attributes: 0,
		})

		// 3. Add Everyone SID
		everyoneSid, err := getEveryoneSID()
		if err != nil {
			return 0, fmt.Errorf("failed to get everyone SID: %w", err)
		}
		restrictingSids = append(restrictingSids, windows.SIDAndAttributes{
			Sid:        everyoneSid,
			Attributes: 0,
		})
	}

	var restrictingSidCount uintptr
	var restrictingSidsPtr uintptr
	if len(restrictingSids) > 0 {
		restrictingSidCount = uintptr(len(restrictingSids))
		restrictingSidsPtr = uintptr(unsafe.Pointer(&restrictingSids[0]))
	}

	r1, _, e1 := procCreateRestrictedToken.Call(
		uintptr(current),
		flags,
		0, 0,
		0, 0,
		restrictingSidCount,
		restrictingSidsPtr,
		uintptr(unsafe.Pointer(&restricted)),
	)
	if r1 == 0 {
		if e1 != syscall.Errno(0) {
			return 0, e1
		}
		return 0, syscall.EINVAL
	}

	if len(restrictingSids) > 0 {
		var daclSids []*windows.SID
		for _, sa := range restrictingSids {
			daclSids = append(daclSids, sa.Sid)
		}
		if err := setDefaultDacl(restricted, daclSids); err != nil {
			restricted.Close()
			return 0, fmt.Errorf("setDefaultDacl failed: %w", err)
		}
	}

	return restricted, nil
}
