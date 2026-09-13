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

// gatewayAPIKinds is every Gateway API kind gwlint registers.
var gatewayAPIKinds = []string{
	gwobjectkinds.Gateway,
	gwobjectkinds.ListenerSet,
	gwobjectkinds.HTTPRoute,
	gwobjectkinds.GRPCRoute,
	gwobjectkinds.TCPRoute,
	gwobjectkinds.TLSRoute,
	gwobjectkinds.UDPRoute,
	gwobjectkinds.ReferenceGrant,
}

func gatewayAPIGVK(version, kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: version}.WithKind(kind)
}

// Gateway API kinds are served at several versions at once, and which one a
// manifest uses is the author's choice, not a different object. A check must
// see the object either way.
func TestGatewayAPIKindsMatchEveryServedVersion(t *testing.T) {
	for _, kind := range gatewayAPIKinds {
		t.Run(kind, func(t *testing.T) {
			matcher, err := klobjectkinds.ConstructMatcher(kind)
			require.NoError(t, err, "object kind %q is not registered", kind)

			for _, version := range []string{"v1", "v1beta1", "v1alpha2", "v1alpha3"} {
				assert.True(t, matcher.Matches(gatewayAPIGVK(version, kind)),
					"did not match %s at %s", kind, version)
			}
		})
	}
}

func TestGatewayAPIKindsDoNotMatchEachOther(t *testing.T) {
	for _, kind := range gatewayAPIKinds {
		t.Run(kind, func(t *testing.T) {
			matcher, err := klobjectkinds.ConstructMatcher(kind)
			require.NoError(t, err)

			for _, other := range gatewayAPIKinds {
				if other == kind {
					continue
				}
				assert.False(t, matcher.Matches(gatewayAPIGVK("v1", other)), "also matched %s", other)
			}
		})
	}
}

// Matching on group and kind must not extend to a same-named kind in somebody
// else's API group, or to a core Kubernetes object.
func TestGatewayAPIKindsRejectOtherGroups(t *testing.T) {
	matcher, err := klobjectkinds.ConstructMatcher(gwobjectkinds.HTTPRoute)
	require.NoError(t, err)

	assert.False(t, matcher.Matches(
		schema.GroupVersion{Group: "some.vendor.io", Version: "v1"}.WithKind("HTTPRoute")))
	assert.False(t, matcher.Matches(
		schema.GroupVersion{Group: "", Version: "v1"}.WithKind("Service")))
	assert.False(t, matcher.Matches(
		schema.GroupVersion{Group: egv1a1.GroupName, Version: "v1alpha1"}.WithKind("HTTPRoute")))
}

// Envoy Gateway's own CRDs stay pinned to their single served version, since
// gateway.envoyproxy.io serves only v1alpha1 today.
func TestEnvoyGatewayKindsMatchTheirOwnGVK(t *testing.T) {
	cases := []struct {
		kind string
		gvk  schema.GroupVersionKind
	}{
		{gwobjectkinds.Backend, egv1a1.GroupVersion.WithKind(egv1a1.KindBackend)},
		{gwobjectkinds.BackendTrafficPolicy, egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy)},
	}

	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			matcher, err := klobjectkinds.ConstructMatcher(c.kind)
			require.NoError(t, err)
			assert.True(t, matcher.Matches(c.gvk))

			for _, other := range cases {
				if other.kind == c.kind {
					continue
				}
				assert.False(t, matcher.Matches(other.gvk))
			}
			assert.False(t, matcher.Matches(gatewayAPIGVK("v1", c.kind)),
				"matched the same kind in the Gateway API group")
		})
	}
}

// RouteKinds is what the check iterates to decide whether a targetRef or
// selector names a route, so it has to stay in step with the registered kinds.
func TestRouteKindsAreAllRegistered(t *testing.T) {
	require.NotEmpty(t, gwobjectkinds.RouteKinds)
	for _, kind := range gwobjectkinds.RouteKinds {
		_, err := klobjectkinds.ConstructMatcher(kind)
		assert.NoError(t, err, "route kind %q is not registered", kind)
	}
	assert.ElementsMatch(t,
		[]string{"HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute", "UDPRoute"},
		gwobjectkinds.RouteKinds)
}
