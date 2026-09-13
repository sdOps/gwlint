package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwversion "github.com/sdOps/gwlint/pkg/version"

	// Blank-imported so every check template registers itself via init(), the
	// same way cmd/gwlint wires it up.
	_ "github.com/sdOps/gwlint/pkg/templates/all"
)

// runLint runs the real lint command over the given paths and returns the
// parsed JSON result alongside the command's error, which is what determines
// gwlint's exit code.
func runLint(t *testing.T, paths ...string) (lintResult, error) {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "result.json")

	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"--format", "json", "--output", outPath}, paths...))
	cmdErr := cmd.Execute()

	contents, err := os.ReadFile(outPath) //nolint:gosec // test-controlled path
	require.NoError(t, err, "lint command wrote no output file")
	var result lintResult
	require.NoError(t, json.Unmarshal(contents, &result))
	return result, cmdErr
}

func messagesFrom(result lintResult) []string {
	messages := make([]string, 0, len(result.Reports))
	for _, report := range result.Reports {
		messages = append(messages, report.Diagnostic.Message)
	}
	return messages
}

// copyManifest copies one example manifest into dstDir. gosec flags the
// tempdir-derived paths as tainted; they are test-controlled.
func copyManifest(t *testing.T, srcDir, dstDir, name string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(srcDir, name)) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	//nolint:gosec // test-controlled path
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, name), contents, 0o600))
}

func examplePath(t *testing.T, parts ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "..", "examples"}, parts...)...))
	require.NoError(t, err)
	return path
}

func TestLintFlagsTheFailingExamples(t *testing.T) {
	result, err := runLint(t, examplePath(t, "failing"))

	// A non-nil error is what gives gwlint its non-zero exit code.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found 6 lint errors")

	require.Len(t, result.Reports, 6)
	assert.Equal(t, gwversion.Get(), result.Summary.KubeLinterVersion)
	for _, report := range result.Reports {
		assert.Contains(t,
			[]string{"fqdn-backend-cold-start", "dangling-parent-ref", "dangling-backend-ref"}, report.Check)
		assert.NotEmpty(t, report.Remediation)
		assert.NotEmpty(t, report.Object.Metadata.FilePath)
	}

	assert.ElementsMatch(t, []string{
		`BackendTrafficPolicy has no health check, but targets HTTPRoute "partner-api-route", ` +
			`which routes to Backend "partner-api-upstream" with FQDN endpoint "partner-api.example.com"; ` +
			`this backend resolves via DNS at startup and can 503 before the name resolves`,
		`BackendTrafficPolicy has no health check, but targets Gateway "public-gateway", ` +
			`whose attached HTTPRoute "billing-api-route" routes to Backend "billing-api-upstream" ` +
			`with FQDN endpoint "billing-api.example.com"; this backend resolves via DNS at startup ` +
			`and can 503 before the name resolves`,
		// A Gateway-level policy reaching a route in another namespace: the
		// backendRef omits a namespace, so it resolves in the route's.
		`BackendTrafficPolicy has no health check, but targets Gateway "edge-gateway", ` +
			`whose attached HTTPRoute "ledger-route" routes to Backend "ledger-upstream" ` +
			`with FQDN endpoint "ledger.example.com"; this backend resolves via DNS at startup ` +
			`and can 503 before the name resolves`,
		// A TCPRoute, authored as v1alpha2: both the route kind and the API
		// version were invisible to gwlint before.
		`BackendTrafficPolicy has no health check, but targets TCPRoute "database-route", ` +
			`which routes to Backend "database-upstream" with FQDN endpoint "database.example.com"; ` +
			`this backend resolves via DNS at startup and can 503 before the name resolves`,
		// A parentRef naming a Gateway that is not there.
		`HTTPRoute "reporting-route" attaches to Gateway "reportng-gateway" in namespace "reporting", ` +
			`which is not present; the route will never be programmed and its traffic will not be served`,
		// A backendRef naming a Service that is not there.
		`HTTPRoute "storefront-route" routes to Service "storefront-chekout" in namespace "storefront", ` +
			`which is not present; requests matching that rule have nowhere to go`,
	}, messagesFrom(result))
}

func TestLintPassesThePassingExamples(t *testing.T) {
	result, err := runLint(t, examplePath(t, "passing"))

	require.NoError(t, err)
	assert.Empty(t, result.Reports)
}

// The README promises that manifests split across directories, charts, or
// repos correlate as one set. Splitting one scenario's three manifests into
// three separate directories is the E2E version of that promise: kube-linter's
// own per-directory contexts would see nothing here.
func TestLintCorrelatesObjectsAcrossSeparateDirectories(t *testing.T) {
	root := t.TempDir()
	source := examplePath(t, "failing", "direct-route")
	var dirs []string
	for i, name := range []string{"backendtrafficpolicy.yaml", "httproute.yaml", "backend.yaml"} {
		dir := filepath.Join(root, string(rune('a'+i)))
		require.NoError(t, os.MkdirAll(dir, 0o750))
		copyManifest(t, source, dir, name)
		dirs = append(dirs, dir)
	}

	result, err := runLint(t, dirs...)

	require.Error(t, err)
	require.Len(t, result.Reports, 1)
	assert.Contains(t, result.Reports[0].Diagnostic.Message, `Backend "partner-api-upstream"`)
}

// The flip side: a policy whose route and backend were never passed to gwlint
// must not be flagged, since the check cannot see what it resolves to.
func TestLintDoesNotFlagPolicyWithoutItsRouteAndBackend(t *testing.T) {
	dir := t.TempDir()
	copyManifest(t, examplePath(t, "failing", "direct-route"), dir, "backendtrafficpolicy.yaml")

	result, err := runLint(t, dir)

	require.NoError(t, err)
	assert.Empty(t, result.Reports)
}

func TestLintWithNoObjectsFoundIsQuietByDefault(t *testing.T) {
	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{t.TempDir()})

	assert.NoError(t, cmd.Execute())
}

func TestLintWithNoObjectsFoundFailsWhenAsked(t *testing.T) {
	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--fail-if-no-objects-found", t.TempDir()})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid objects found")
}

func TestLintRejectsAnUnknownFormat(t *testing.T) {
	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--format", "yaml", examplePath(t, "passing")})

	require.Error(t, cmd.Execute())
}

// The plain-text report is what a user sees by default, so it has to carry
// gwlint's own version rather than kube-linter's, and name the check.
func TestLintPlainOutputNamesGwlintAndTheCheck(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "result.txt")

	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--output", outPath, examplePath(t, "failing")})
	require.Error(t, cmd.Execute())

	contents, err := os.ReadFile(outPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)

	assert.Contains(t, string(contents), "gwlint "+gwversion.Get())
	assert.Contains(t, string(contents), "check: fqdn-backend-cold-start")
}

// A clean result and a result where nothing relevant was ever loaded both
// printed "No lint errors found!", which gave a reader no way to tell whether
// the manifests were checked or merely read.
func TestPlainOutputReportsWhatWasScanned(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "result.txt")

	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--output", outPath, examplePath(t, "passing")})
	require.NoError(t, cmd.Execute())

	contents, err := os.ReadFile(outPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)

	assert.Contains(t, string(contents), "No lint errors found.")
	assert.Regexp(t, `Checked \d+ objects: .*BackendTrafficPolicy.* \(\d+ objects loaded from \d+ files\)\.`, string(contents))
}

// The case the summary exists for: objects loaded, but none a check applies to.
func TestPlainOutputCallsOutWhenNothingIsInScope(t *testing.T) {
	// A Service alone: nothing any check is scoped to.
	dir := t.TempDir()
	copyManifest(t, examplePath(t, "passing", "service-backend"), dir, "service.yaml")
	outPath := filepath.Join(t.TempDir(), "result.txt")

	cmd := Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--output", outPath, dir})
	require.NoError(t, cmd.Execute())

	contents, err := os.ReadFile(outPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)

	assert.Contains(t, string(contents), "found, so nothing was checked")
	assert.Contains(t, string(contents), "BackendTrafficPolicy")
	assert.Contains(t, string(contents), "HTTPRoute")
}

// The counts have to be in the machine-readable output too, and adding them
// must not disturb the keys that were already there.
func TestJSONOutputCarriesTheScanSummary(t *testing.T) {
	result, err := runLint(t, examplePath(t, "failing"))
	require.Error(t, err)

	assert.Equal(t, map[string]int{"BackendTrafficPolicy": 4, "HTTPRoute": 5, "TCPRoute": 1},
		result.Scanned.CheckedByKind, "one policy per failing scenario, plus every route")
	assert.Equal(t, 10, result.Scanned.Checked)
	assert.Equal(t,
		[]string{"BackendTrafficPolicy", "GRPCRoute", "HTTPRoute", "TCPRoute", "TLSRoute", "UDPRoute"},
		result.Scanned.LooksFor)
	assert.Positive(t, result.Scanned.Objects)
	assert.Positive(t, result.Scanned.Files)
	assert.Zero(t, result.Scanned.Unparsed)
	assert.GreaterOrEqual(t, result.Scanned.Objects, result.Scanned.Checked)
	// Still the kube-linter-shaped fields.
	assert.NotEmpty(t, result.Reports)
	assert.NotEmpty(t, result.Checks)
	assert.Equal(t, gwversion.Get(), result.Summary.KubeLinterVersion)
}
