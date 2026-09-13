// Package decoder builds the runtime.Decoder gwlint passes to kube-linter's
// lintcontext.Options.CustomDecoder. kube-linter's own default decoder (built
// in an unexported init() in its pkg/lintcontext package) cannot be extended
// from outside that package, and lintcontext.CreateContextsWithOptions treats
// a supplied CustomDecoder as a full replacement, not an overlay. So this
// package starts from the same base client-go scheme kube-linter itself
// starts from, and layers Gateway API and Envoy Gateway's own types on top,
// to decode both plain Kubernetes objects and Gateway API/Envoy Gateway CRDs
// through a single decoder.
package decoder

import (
	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1a3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Decoder decodes YAML/JSON Kubernetes manifests into typed Go objects,
// covering the built-in Kubernetes API, every served version of the Gateway
// API, and the Envoy Gateway CRDs gwlint's checks operate on.
var Decoder runtime.Decoder

func init() {
	clientScheme := scheme.Scheme
	// Every served Gateway API version, not just v1: TCPRoute, TLSRoute and
	// UDPRoute are experimental-channel resources still routinely authored as
	// v1alpha2, and ReferenceGrant as v1beta1. Registering only v1 means those
	// manifests fail to decode and land in InvalidObjects, where a check never
	// sees them and the user sees nothing without --verbose.
	schemeBuilder := runtime.NewSchemeBuilder(
		gatewayv1.Install,
		gatewayv1b1.Install,
		gatewayv1a2.Install,
		gatewayv1a3.Install,
		egv1a1.AddToScheme,
	)
	if err := schemeBuilder.AddToScheme(clientScheme); err != nil {
		panic("gwlint: failed to add Gateway API/Envoy Gateway types to scheme: " + err.Error())
	}
	Decoder = serializer.NewCodecFactory(clientScheme).UniversalDeserializer()
}
