package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// AsGateway extracts a Gateway from obj, whichever served version it was
// written in, and reports whether it was one. Gateway is served at v1 and
// v1beta1 only, and v1beta1's is a distinct Go type over the same struct
// rather than an alias, so it converts directly. A check that type-switched on
// *gatewayv1.Gateway alone would silently skip every v1beta1 manifest, which
// is still what a lot of charts emit.
func AsGateway(obj k8sutil.Object) (*gatewayv1.Gateway, bool) {
	switch gateway := obj.(type) {
	case *gatewayv1.Gateway:
		return gateway, true
	case *gatewayv1b1.Gateway:
		return (*gatewayv1.Gateway)(gateway), true
	default:
		return nil, false
	}
}
