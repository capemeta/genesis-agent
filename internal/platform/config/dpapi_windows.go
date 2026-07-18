//go:build windows

package config

import (
	"encoding/base64"
	"syscall"
	"unsafe"
)

var (
	dllcrypt32  = syscall.NewLazyDLL("crypt32.dll")
	procEncrypt = dllcrypt32.NewProc("CryptProtectData")
	procDecrypt = dllcrypt32.NewProc("CryptUnprotectData")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func Encrypt(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	var inBlob, outBlob dataBlob
	inBlob.cbData = uint32(len(data))
	inBlob.pbData = &data[0]

	r, _, err := procEncrypt.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return "", err
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(outBlob.pbData)))

	encrypted := make([]byte, outBlob.cbData)
	copy(encrypted, (*[1 << 30]byte)(unsafe.Pointer(outBlob.pbData))[:outBlob.cbData])

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func Decrypt(encryptedStr string) (string, error) {
	if encryptedStr == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(encryptedStr)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}

	var inBlob, outBlob dataBlob
	inBlob.cbData = uint32(len(data))
	inBlob.pbData = &data[0]

	r, _, err := procDecrypt.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return "", err
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(outBlob.pbData)))

	decrypted := make([]byte, outBlob.cbData)
	copy(decrypted, (*[1 << 30]byte)(unsafe.Pointer(outBlob.pbData))[:outBlob.cbData])

	return string(decrypted), nil
}
