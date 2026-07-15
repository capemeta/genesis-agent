//go:build windows

package windowssandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func secretsFilePath() string {
	return filepath.Join(sandboxDir(), "secrets", "GenesisSandboxUser.secret")
}

// WriteSecret encrypts the password using DPAPI and saves it to the local secrets file
func WriteSecret(password string) error {
	encrypted, err := encryptPassword(password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}

	secretFile := secretsFilePath()
	secretDir := filepath.Dir(secretFile)
	if err := os.MkdirAll(secretDir, 0700); err != nil {
		return fmt.Errorf("failed to create secrets dir: %w", err)
	}

	if err := os.WriteFile(secretFile, encrypted, 0600); err != nil {
		return fmt.Errorf("failed to write secret file: %w", err)
	}
	return nil
}

// ReadSecret reads the local secrets file and decrypts the password using DPAPI
func ReadSecret() (string, error) {
	secretFile := secretsFilePath()
	encrypted, err := os.ReadFile(secretFile)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}

	password, err := decryptPassword(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt password: %w", err)
	}
	return password, nil
}

func encryptPassword(password string) ([]byte, error) {
	bytes := []byte(password)
	if len(bytes) == 0 {
		return nil, fmt.Errorf("password cannot be empty")
	}
	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(bytes))
	inBlob.Data = &bytes[0]

	var outBlob windows.DataBlob
	err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, 0, &outBlob)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	outBytes := make([]byte, outBlob.Size)
	copy(outBytes, unsafe.Slice(outBlob.Data, outBlob.Size))
	return outBytes, nil
}

func decryptPassword(encrypted []byte) (string, error) {
	if len(encrypted) == 0 {
		return "", fmt.Errorf("encrypted password cannot be empty")
	}
	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(encrypted))
	inBlob.Data = &encrypted[0]

	var outBlob windows.DataBlob
	err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, 0, &outBlob)
	if err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	outBytes := make([]byte, outBlob.Size)
	copy(outBytes, unsafe.Slice(outBlob.Data, outBlob.Size))
	return string(outBytes), nil
}
