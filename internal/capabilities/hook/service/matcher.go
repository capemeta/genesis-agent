package service

import (
	"path"
	"regexp"
	"strings"
)

func matches(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" || pattern == "*" {
		return true
	}
	for _, candidate := range strings.Split(pattern, "|") {
		candidate = strings.TrimSpace(candidate)
		if candidate == value {
			return true
		}
		if strings.HasPrefix(candidate, "/") && strings.HasSuffix(candidate, "/") && len(candidate) > 2 {
			if re, err := regexp.Compile(candidate[1 : len(candidate)-1]); err == nil && re.MatchString(value) {
				return true
			}
			continue
		}
		if ok, err := path.Match(candidate, value); err == nil && ok {
			return true
		}
	}
	return false
}
