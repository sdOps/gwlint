package decoder_test

import (
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/decoder"
)

// decode runs a manifest through the decoder exactly the way kube-linter's
// lintcontext does when parsing a YAML document off disk.
func decode(t *testing.T, manifest string) any {
	t.Helper()
	obj, _, err := decoder.Decoder.Decode([]byte(manifest), nil, nil)
	require.NoError(t, err)
	return obj
}

// The core Kubernetes types must still decode: gwlint layers the Gateway API
// and Envoy Gateway schemes onto client-go's base scheme rather than replacing
// it, and routes can point at a plain Service.
func TestDecodesCoreKubernetesObject(t *testing.T) {
	obj := decode(t, `
apiVersion: v1
kind: Service
metadata:
  name: partner-api-service
  namespace: default
spec:
  ports:
    - port: 80
      targetPort: 8080
`)

	svc, ok := obj.(*corev1.Service)
	require.True(t, ok, "expected *corev1.Service, got %T", obj)
	assert.Equal(t, "partner-api-service", svc.Name)
	assert.Equal(t, int32(80), svc.Spec.Ports[0].Port)
}

func TestDecodesHTTPRoute(t *testing.T) {
	obj := decode(t, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: partner-api-route
  namespace: default
spec:
  parentRefs:
    - name: public-gateway
  rules:
    - backendRefs:
        - group: gateway.envoyproxy.io
          kind: Backend
          name: partner-api-upstream
`)

	route, ok := obj.(*gatewayv1.HTTPRoute)
	require.True(t, ok, "expected *gatewayv1.HTTPRoute, got %T", obj)
	assert.Equal(t, gatewayv1.ObjectName("public-gateway"), route.Spec.ParentRefs[0].Name)

	backendRef := route.Spec.Rules[0].BackendRefs[0].BackendRef
	require.NotNil(t, backendRef.Kind)
	assert.Equal(t, gatewayv1.Kind("Backend"), *backendRef.Kind)
	assert.Equal(t, gatewayv1.ObjectName("partner-api-upstream"), backendRef.Name)
}

func TestDecodesGRPCRoute(t *testing.T) {
	obj := decode(t, `
apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: partner-api-grpc-route
  namespace: default
spec:
  rules:
    - backendRefs:
        - group: gateway.envoyproxy.io
          kind: Backend
          name: partner-api-upstream
`)

	route, ok := obj.(*gatewayv1.GRPCRoute)
	require.True(t, ok, "expected *gatewayv1.GRPCRoute, got %T", obj)
	assert.Equal(t, "partner-api-grpc-route", route.Name)
}

func TestDecodesGateway(t *testing.T) {
	obj := decode(t, `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: public-gateway
  namespace: default
spec:
  gatewayClassName: eg
  listeners:
    - name: http
      port: 80
      protocol: HTTP
`)

	gateway, ok := obj.(*gatewayv1.Gateway)
	require.True(t, ok, "expected *gatewayv1.Gateway, got %T", obj)
	assert.Equal(t, gatewayv1.ObjectName("eg"), gateway.Spec.GatewayClassName)
}

func TestDecodesReferenceGrant(t *testing.T) {
	obj := decode(t, `
apiVersion: gateway.networking.k8s.io/v1
kind: ReferenceGrant
metadata:
  name: allow-partner-api
  namespace: upstreams
spec:
  from:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      namespace: default
  to:
    - group: ""
      kind: Service
`)

	grant, ok := obj.(*gatewayv1.ReferenceGrant)
	require.True(t, ok, "expected *gatewayv1.ReferenceGrant, got %T", obj)
	assert.Equal(t, gatewayv1.Kind("HTTPRoute"), grant.Spec.From[0].Kind)
}

func TestDecodesBackendWithFQDNEndpoint(t *testing.T) {
	obj := decode(t, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: Backend
metadata:
  name: partner-api-upstream
  namespace: default
spec:
  endpoints:
    - fqdn:
        hostname: partner-api.example.com
        port: 443
`)

	backend, ok := obj.(*egv1a1.Backend)
	require.True(t, ok, "expected *egv1a1.Backend, got %T", obj)
	require.NotNil(t, backend.Spec.Endpoints[0].FQDN)
	assert.Equal(t, "partner-api.example.com", backend.Spec.Endpoints[0].FQDN.Hostname)
}

func TestDecodesBackendTrafficPolicy(t *testing.T) {
	obj := decode(t, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: BackendTrafficPolicy
metadata:
  name: partner-api-route-policy
  namespace: default
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: partner-api-route
  healthCheck:
    passive:
      maxEjectionPercent: 50
`)

	policy, ok := obj.(*egv1a1.BackendTrafficPolicy)
	require.True(t, ok, "expected *egv1a1.BackendTrafficPolicy, got %T", obj)
	require.Len(t, policy.Spec.TargetRefs, 1)
	assert.Equal(t, gatewayv1.Kind("HTTPRoute"), policy.Spec.TargetRefs[0].Kind)
	require.NotNil(t, policy.Spec.HealthCheck)
	assert.NotNil(t, policy.Spec.HealthCheck.Passive)
}

// An unregistered CRD must surface as a decode error, which kube-linter
// records as an invalid object rather than silently dropping.
func TestReturnsErrorForUnregisteredKind(t *testing.T) {
	_, _, err := decoder.Decoder.Decode([]byte(`
apiVersion: some.vendor.io/v1
kind: NotARealKind
metadata:
  name: mystery
`), nil, nil)

	require.Error(t, err)
}
