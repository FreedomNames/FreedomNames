// Package bind classifies listener bind failures so callers can turn them into
// advice instead of a bare error. Both the DNS server and the HTTP API open
// listeners and hit the same two cases.
package bind

import (
	"errors"
	"os"
	"strings"
	"syscall"
)

// IsPrivilegedPort reports whether err is a permission-denied bind, which for a
// server almost always means the configured port is privileged (<1024).
func IsPrivilegedPort(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied")
}

// IsAddrInUse reports whether err is an "address already in use" bind failure.
func IsAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use")
}
