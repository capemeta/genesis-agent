package service

import (
	"testing"
)

func TestEnvSanitizer(t *testing.T) {
	sanitizer := NewEnvSanitizer()

	input := map[string]string{
		"PATH":                  "/usr/bin:/bin",
		"AWS_SECRET_ACCESS_KEY": "supersecretkey",
		"DATABASE_PASSWORD":     "123456",
		"HOME":                  "/home/user",
		"API_TOKEN":             "abcdef",
	}

	got := sanitizer.Sanitize(input)

	if _, exists := got["AWS_SECRET_ACCESS_KEY"]; exists {
		t.Errorf("AWS_SECRET_ACCESS_KEY should have been stripped")
	}
	if _, exists := got["DATABASE_PASSWORD"]; exists {
		t.Errorf("DATABASE_PASSWORD should have been stripped")
	}
	if _, exists := got["API_TOKEN"]; exists {
		t.Errorf("API_TOKEN should have been stripped")
	}

	if got["PATH"] != "/usr/bin:/bin" {
		t.Errorf("PATH was altered, got %v", got["PATH"])
	}
	if got["HOME"] != "/home/user" {
		t.Errorf("HOME was altered, got %v", got["HOME"])
	}
}
