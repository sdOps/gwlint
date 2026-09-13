package objectkinds_test

import (
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	klobjectkinds "golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

func gatewayAPIGVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

// Each object kind gwlint registers has to match its own GVK and nothing
// else, since kube-linter's engine uses the matcher to decide which objects a
// check even sees.
func TestRegisteredKindsMatchTheirOwnGVK(t *testing.T) {
	cases := []struct {
		kind string
		gvk  schema.GroupVersionKind
	}{
		{gwobjectkinds.Gateway, gatewayAPIGVK("Gateway")},
		{gwobjectkinds.HTTPRoute, gatewayAPIGVK("HTTPRoute")},
		{gwobjectkinds.GRPCRoute, gatewayAPIGVK("GRPCRoute")},
		{gwobjectkinds.ReferenceGrant, gatewayAPIGVK("ReferenceGrant")},
		{gwobjectkinds.Backend, egv1a1.GroupVersion.WithKind(egv1a1.KindBackend)},
		{gwobjectkinds.BackendTrafficPolicy, egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy)},
	}

	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			matcher, err := klobjectkinds.ConstructMatcher(c.kind)
			require.NoError(t, err, "object kind %q is not registered", c.kind)
			assert.True(t, matcher.Matches(c.gvk), "matcher did not match its own GVK %s", c.gvk)

			for _, other := range cases {
				if other.kind == c.kind {
					continue
				}
				assert.False(t, matcher.Matches(other.gvk), "matcher also matched %s", other.gvk)
			}
		})
	}
}

// A Gateway API kind at a different API version is a different object, and a
// core Kubernetes object is not a Gateway API one.
func TestRegisteredKindsRejectOtherGroupsAndVersions(t *testing.T) {
	matcher, err := klobjectkinds.ConstructMatcher(gwobjectkinds.HTTPRoute)
	require.NoError(t, err)

	assert.False(t, matcher.Matches(
		schema.GroupVersion{Group: gatewayv1.GroupName, Version: "v1beta1"}.WithKind("HTTPRoute")))
	assert.False(t, matcher.Matches(
		schema.GroupVersion{Group: "some.vendor.io", Version: "v1"}.WithKind("HTTPRoute")))
	assert.False(t, matcher.Matches(
		schema.GroupVersion{Group: "", Version: "v1"}.WithKind("Service")))
}
