package seatbelt

import (
	"errors"
	"strconv"
	"strings"
)

var ErrInvalidCommand = errors.New("seatbelt command argv不能为空")

// BuildProfile 构造最小可用 SBPL profile。
func BuildProfile(fs FileSystemPolicy, network NetworkPolicy, proxyPorts []int, allowUnixSockets []string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default (with message \"GENESIS_SANDBOX\"))\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("\n; Network\n")
	writeNetworkPolicy(&b, network, proxyPorts, allowUnixSockets)
	b.WriteString("\n; File read\n")
	writeReadPolicy(&b, fs)
	b.WriteString("\n; File write\n")
	writeWritePolicy(&b, fs)
	return b.String()
}

func writeNetworkPolicy(b *strings.Builder, network NetworkPolicy, proxyPorts []int, allowUnixSockets []string) {
	switch network {
	case NetworkFullAccess:
		b.WriteString("(allow network*)\n")
	case NetworkLoopback:
		b.WriteString("(allow network-bind (local ip \"localhost:*\"))\n")
		b.WriteString("(allow network-inbound (local ip \"localhost:*\"))\n")
		b.WriteString("(allow network-outbound (remote ip \"localhost:*\"))\n")
	case NetworkProxyOnly:
		for _, port := range proxyPorts {
			b.WriteString("(allow network-outbound (remote ip \"localhost:")
			b.WriteString(strconv.Itoa(port))
			b.WriteString("\"))\n")
		}
	}
	for _, socket := range allowUnixSockets {
		b.WriteString("(allow system-socket (socket-domain AF_UNIX))\n")
		b.WriteString("(allow network-outbound (remote unix-socket (subpath ")
		b.WriteString(quote(socket))
		b.WriteString(")))\n")
	}
}

func writeReadPolicy(b *strings.Builder, fs FileSystemPolicy) {
	if fs.AllowFullDiskRead || len(fs.ReadableRoots) == 0 {
		b.WriteString("(allow file-read*)\n")
	} else {
		for _, root := range fs.ReadableRoots {
			b.WriteString("(allow file-read* (subpath ")
			b.WriteString(quote(root))
			b.WriteString("))\n")
		}
	}
	for _, path := range fs.UnreadablePaths {
		b.WriteString("(deny file-read* (subpath ")
		b.WriteString(quote(path))
		b.WriteString(") (with message \"GENESIS_SANDBOX\"))\n")
		b.WriteString("(deny file-write-unlink (subpath ")
		b.WriteString(quote(path))
		b.WriteString(") (with message \"GENESIS_SANDBOX\"))\n")
	}
}

func writeWritePolicy(b *strings.Builder, fs FileSystemPolicy) {
	b.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	if fs.AllowFullDiskWrite {
		b.WriteString("(allow file-write*)\n")
	} else {
		for _, root := range fs.WritableRoots {
			b.WriteString("(allow file-write* (subpath ")
			b.WriteString(quote(root))
			b.WriteString("))\n")
		}
	}
	for _, path := range append(fs.ReadOnlyRoots, fs.ProtectedMetadataPaths...) {
		b.WriteString("(deny file-write* (subpath ")
		b.WriteString(quote(path))
		b.WriteString(") (with message \"GENESIS_SANDBOX\"))\n")
		b.WriteString("(deny file-write-unlink (subpath ")
		b.WriteString(quote(path))
		b.WriteString(") (with message \"GENESIS_SANDBOX\"))\n")
	}
}

func quote(value string) string { return strconv.Quote(value) }
