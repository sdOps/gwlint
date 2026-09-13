package root_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sdOps/gwlint/pkg/command/root"
	gwversion "github.com/sdOps/gwlint/pkg/version"

	// Blank-imported so every check template registers itself via init(), the
	// same way cmd/gwlint wires it up.
	_ "github.com/sdOps/gwlint/pkg/templates/all"
)

// runRoot runs the root command and captures os.Stdout. kube-linter's own
// subcommands print there directly rather than through cobra's writer, so
// cmd.SetOut is not enough to see their output.
func runRoot(t *testing.T, args ...string) string {
	t.Helper()
	read, write, err := os.Pipe()
	require.NoError(t, err)

	stdout := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = stdout }()

	cmd := root.Command()
	cmd.SetArgs(args)
	execErr := cmd.Execute()
	require.NoError(t, write.Close())

	var out bytes.Buffer
	_, err = out.ReadFrom(read)
	require.NoError(t, err)
	require.NoError(t, execErr)
	return out.String()
}

func TestRootCommandExposesTheExpectedSubcommands(t *testing.T) {
	names := make([]string, 0)
	for _, sub := range root.Command().Commands() {
		names = append(names, sub.Name())
	}

	assert.Contains(t, names, "lint")
	assert.Contains(t, names, "templates")
	assert.Contains(t, names, "version")
}

// The version subcommand must report gwlint's own version, not the kube-linter
// release it is built against.
func TestVersionPrintsGwlintVersion(t *testing.T) {
	assert.Equal(t, gwversion.Get()+"\n", runRoot(t, "version"))
}

// "templates list" is kube-linter's own command reused as-is; it reads the
// shared template registry, so it must surface gwlint's check and nothing of
// kube-linter's own.
func TestTemplatesListShowsOnlyGwlintTemplates(t *testing.T) {
	out := runRoot(t, "templates", "list")

	assert.Contains(t, out, "fqdn-backend-cold-start")
	assert.Contains(t, out, "Supported Objects: [BackendTrafficPolicy]")
	assert.NotContains(t, out, "run-as-non-root")
}
