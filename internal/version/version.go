// Package version reports which build of the node is running.
//
// The value is stamped in at release build time so /health and /info can tell a
// spawning host (e.g. LibreWeb) exactly what it launched.
package version

// defaultVersion is the fallback for a plain `go build`. It deliberately never
// looks like a release version, so an unstamped build can't masquerade as one.
const defaultVersion = "0.0.0-dev"

// Version is injected by the release build via
//
//	-ldflags "-X gitlab.melroy.org/freedom-names/freedom-names/internal/version.Version=<tag>"
//
// (see scripts/build-release.sh). It is empty in a plain `go build`, in which
// case String falls back to defaultVersion.
//
// Note that -X matches this symbol by its full import path. A wrong path fails
// silently — the linker patches nothing and every binary reports the fallback —
// so scripts/build.sh is worth a `--version` check after changing it.
var Version string

// String returns the effective version string reported by the node.
func String() string {
	if Version != "" {
		return Version
	}
	return defaultVersion
}
