package sandbox

import (
	"strings"

	"genesis-agent/shared/local/sandbox/bubblewrap"
	"genesis-agent/shared/local/sandbox/seatbelt"
)

// joinStrings 用 sep 拼接字符串切片。
func joinStrings(items []string, sep string) string {
	return strings.Join(items, sep)
}


func pathRules(rules []PathRule) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.Path != "" {
			out = append(out, rule.Path)
		}
	}
	return out
}

func toBubblewrapMasks(rules []PathRule) []bubblewrap.PathMask {
	out := make([]bubblewrap.PathMask, 0, len(rules))
	for _, rule := range rules {
		if rule.Path == "" {
			continue
		}
		kind := bubblewrap.PathMaskAuto
		switch rule.Kind {
		case "file":
			kind = bubblewrap.PathMaskFile
		case "dir", "directory":
			kind = bubblewrap.PathMaskDir
		}
		out = append(out, bubblewrap.PathMask{Path: rule.Path, Kind: kind})
	}
	return out
}

var defaultProtectedMetadataNames = []string{".git", ".codex", ".agents"}

func protectedMetadataPaths(fs FileSystemPolicy) []string {
	out := append([]string{}, fs.ProtectedMetadataPaths...)
	if len(fs.WritableRoots) == 0 {
		return out
	}
	names := fs.ProtectedMetadataNames
	if len(names) == 0 {
		names = defaultProtectedMetadataNames
	}
	for _, root := range fs.WritableRoots {
		if root == "" {
			continue
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			out = append(out, joinSandboxPath(root, name))
		}
	}
	return out
}

func joinSandboxPath(root, name string) string {
	for len(root) > 1 && (root[len(root)-1] == '/' || root[len(root)-1] == '\\') {
		root = root[:len(root)-1]
	}
	if root == "/" {
		return "/" + name
	}
	return root + "/" + name
}

func applyProxyEnv(env map[string]string, network NetworkMode, proxyEnv map[string]string) map[string]string {
	if network != NetworkProxyOnly || len(proxyEnv) == 0 {
		return env
	}
	out := make(map[string]string, len(env)+len(proxyEnv))
	for k, v := range env {
		out[k] = v
	}
	for k, v := range proxyEnv {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// ParseSeatbeltWarnings 解析 stderr 中的 Seatbelt 沙箱拦截日志。
func ParseSeatbeltWarnings(stderr string) []string {
	return seatbelt.FormatWarnings(stderr)
}
