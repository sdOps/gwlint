// Package version holds gwlint's own version string, independent of the
// kube-linter release gwlint is built against.
package version

// Version is gwlint's version. It is a plain constant for now; Phase 1 has
// no release process yet.
const Version = "0.1.0-dev"

// Get returns gwlint's version string.
func Get() string {
	return Version
}
