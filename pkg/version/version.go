// Package version holds gwlint's own version string, independent of the
// kube-linter release gwlint is built against.
package version

// version is gwlint's version, stamped at build time via
//
//	go build -ldflags "-X github.com/sdOps/gwlint/pkg/version.version=v1.2.3"
//
// mise.toml's build task stamps it from `git describe` for local builds, and
// the release workflow stamps it from the tag being released. A build that
// skips ldflags entirely, such as a plain `go build` or `go run`, falls back
// to "dev" rather than claiming a version it cannot back up.
var version = "dev"

// Get returns gwlint's version string.
func Get() string {
	return version
}
