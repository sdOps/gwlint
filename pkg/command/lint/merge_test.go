package lint

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// contextWith builds a LintContext holding one object per given name, standing
// in for a context kube-linter loaded from a single directory.
func contextWith(names ...string) lintcontext.LintContext {
	ctx := &mergedLintContext{}
	for _, name := range names {
		ctx.objects = append(ctx.objects, lintcontext.Object{
			Metadata:  lintcontext.ObjectMetadata{FilePath: name + ".yaml"},
			K8sObject: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}},
		})
	}
	return ctx
}

func objectNames(t *testing.T, ctx lintcontext.LintContext) []string {
	t.Helper()
	names := make([]string, 0, len(ctx.Objects()))
	for _, obj := range ctx.Objects() {
		names = append(names, obj.K8sObject.GetName())
	}
	return names
}

func TestMergeContextsCombinesObjectsAcrossDirectories(t *testing.T) {
	merged := mergeContexts([]lintcontext.LintContext{
		contextWith("gateway", "backendtrafficpolicy"),
		contextWith("httproute"),
		contextWith("backend"),
	})

	// The whole point of merging: a check correlating a policy with the route
	// it covers sees all of them, whichever directory each came from.
	assert.Equal(t, []string{"gateway", "backendtrafficpolicy", "httproute", "backend"}, objectNames(t, merged))
	assert.Empty(t, merged.InvalidObjects())
}

func TestMergeContextsPreservesInvalidObjects(t *testing.T) {
	first := &mergedLintContext{invalidObjects: []lintcontext.InvalidObject{{
		Metadata: lintcontext.ObjectMetadata{FilePath: "broken.yaml"},
		LoadErr:  errors.New("bad yaml"),
	}}}
	second := &mergedLintContext{invalidObjects: []lintcontext.InvalidObject{{
		Metadata: lintcontext.ObjectMetadata{FilePath: "also-broken.yaml"},
		LoadErr:  errors.New("unknown kind"),
	}}}

	merged := mergeContexts([]lintcontext.LintContext{first, second})

	require.Len(t, merged.InvalidObjects(), 2)
	assert.Equal(t, "broken.yaml", merged.InvalidObjects()[0].Metadata.FilePath)
	assert.Equal(t, "also-broken.yaml", merged.InvalidObjects()[1].Metadata.FilePath)
	assert.Empty(t, merged.Objects())
}

func TestMergeContextsHandlesEmptyInput(t *testing.T) {
	merged := mergeContexts(nil)

	assert.Empty(t, merged.Objects())
	assert.Empty(t, merged.InvalidObjects())
}

func TestMergeContextsSkipsEmptyContexts(t *testing.T) {
	merged := mergeContexts([]lintcontext.LintContext{
		&mergedLintContext{},
		contextWith("httproute"),
		&mergedLintContext{},
	})

	assert.Equal(t, []string{"httproute"}, objectNames(t, merged))
}
