//go:build !windows
package config

import "errors"

func Encrypt(data []byte) (string, error) {
	return "", errors.New("DPAPI encryption is only supported on Windows")
}

func Decrypt(encryptedStr string) (string, error) {
	return "", errors.New("DPAPI decryption is only supported on Windows")
}
