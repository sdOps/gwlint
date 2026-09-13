package builtinchecks_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.stackrox.io/kube-linter/pkg/checkregistry"
	"golang.stackrox.io/kube-linter/pkg/config"

	"github.com/sdOps/gwlint/pkg/builtinchecks"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"

	// Blank-imported so every check template registers itself via init(), the
	// same way cmd/gwlint wires it up.
	_ "github.com/sdOps/gwlint/pkg/templates/all"
)

func checkByName(t *testing.T, name string) config.Check {
	t.Helper()
	checks, err := builtinchecks.List()
	require.NoError(t, err)
	for _, chk := range checks {
		if chk.Name == name {
			return chk
		}
	}
	t.Fatalf("built-in check %q not found", name)
	return config.Check{}
}

func TestListReturnsEveryEmbeddedCheck(t *testing.T) {
	checks, err := builtinchecks.List()
	require.NoError(t, err)
	require.NotEmpty(t, checks, "no built-in checks loaded; the embedded yamls directory is empty")

	names := make([]string, 0, len(checks))
	for _, chk := range checks {
		names = append(names, chk.Name)
	}
	assert.Contains(t, names, "fqdn-backend-cold-start")
}

// Every embedded check needs a name, a template to instantiate, a scope, and
// remediation text, or kube-linter's engine cannot run it or report it usefully.
func TestEveryBuiltInCheckIsFullySpecified(t *testing.T) {
	checks, err := builtinchecks.List()
	require.NoError(t, err)

	for _, chk := range checks {
		t.Run(chk.Name, func(t *testing.T) {
			assert.NotEmpty(t, chk.Name)
			assert.NotEmpty(t, chk.Template, "check has no template to instantiate")
			assert.NotEmpty(t, chk.Description)
			assert.NotEmpty(t, chk.Remediation)
			assert.NotEmpty(t, chk.Scope.ObjectKinds, "check has no scope, so the engine will not match any object")
		})
	}
}

func TestFQDNBackendColdStartIsScopedToBackendTrafficPolicy(t *testing.T) {
	chk := checkByName(t, "fqdn-backend-cold-start")

	assert.Equal(t, "fqdn-backend-cold-start", chk.Template)
	assert.Equal(t, []string{gwobjectkinds.BackendTrafficPolicy}, chk.Scope.ObjectKinds)
	assert.Contains(t, chk.Remediation, "health check")
}

// A template registration alone does not make the engine run a check; the
// config.Check entry has to load into a registry and instantiate cleanly.
func TestLoadIntoRegistersAndInstantiatesEveryCheck(t *testing.T) {
	registry := checkregistry.New()
	require.NoError(t, builtinchecks.LoadInto(registry))

	checks, err := builtinchecks.List()
	require.NoError(t, err)
	for _, chk := range checks {
		t.Run(chk.Name, func(t *testing.T) {
			instantiated := registry.Load(chk.Name)
			require.NotNil(t, instantiated, "check did not load out of the registry")
			assert.NotNil(t, instantiated.Func, "check instantiated without a check function")
			assert.NotNil(t, instantiated.Matcher, "check instantiated without an object matcher")
		})
	}
}

// List caches behind a sync.Once; repeated calls must not accumulate entries.
func TestListIsStableAcrossCalls(t *testing.T) {
	first, err := builtinchecks.List()
	require.NoError(t, err)
	second, err := builtinchecks.List()
	require.NoError(t, err)

	assert.Equal(t, first, second)
}
