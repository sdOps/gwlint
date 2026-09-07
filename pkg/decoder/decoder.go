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
)

// Decoder decodes YAML/JSON Kubernetes manifests into typed Go objects,
// covering both the built-in Kubernetes API and the Gateway API/Envoy
// Gateway CRDs gwlint's checks operate on.
var Decoder runtime.Decoder

func init() {
	clientScheme := scheme.Scheme
	schemeBuilder := runtime.NewSchemeBuilder(gatewayv1.Install, egv1a1.AddToScheme)
	if err := schemeBuilder.AddToScheme(clientScheme); err != nil {
		panic("gwlint: failed to add Gateway API/Envoy Gateway types to scheme: " + err.Error())
	}
	Decoder = serializer.NewCodecFactory(clientScheme).UniversalDeserializer()
}
