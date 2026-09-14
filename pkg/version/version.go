// Package version holds gwlint's own version string, independent of the
// kube-linter release gwlint is built against.
package version

// version is gwlint's version, stamped at build time via
//
//	go build -ldflags "-X github.com/sdOps/gwlint/pkg/version.version=v1.2.3"
//
// mise.toml's build task stamps it from `git describe` for local builds, and
// the release workflow's build job stamps it directly from the tag it just
// created, for the binaries it cross-compiles and attaches to the GitHub
// Release. A build that skips ldflags entirely falls back to "dev" rather
// than claiming a version it cannot back up: that includes a plain `go
// build`/`go run`, and also `go install .../cmd/gwlint@<tag>`, which builds
// from the module cache rather than a git checkout and so has no `.git` for
// `git describe` to read and gets no ldflags either.
var version = "dev"

// Get returns gwlint's version string.
func Get() string {
	return version
}
