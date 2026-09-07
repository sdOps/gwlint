// Package fqdnbackendcoldstart flags BackendTrafficPolicies that leave a
// cold-start gap: no health check, covering a route whose backend resolves
// by FQDN. Envoy Gateway automatically puts FQDN-endpoint Backends on the
// same DNS-resolving cluster path the legacy STRICT_DNS Envoy cluster type
// used, so a request that arrives before the name resolves 503s, and with
// no health check there is nothing to route around it during that window.
// This mirrors a real incident: a cold-start DNS resolution gap caused 503s
// on SSO routes. See the README for the incident writeup and remediation.
package fqdnbackendcoldstart

import (
	"fmt"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "fqdn-backend-cold-start"

func init() {
	templates.Register(check.Template{
		HumanName: "FQDN Backend Cold-Start Risk",
		Key:       templateKey,
		Description: "Flag BackendTrafficPolicies with no health check that cover a route whose backend " +
			"resolves via FQDN, which can 503 before DNS resolves at startup",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.BackendTrafficPolicy},
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	policy, ok := object.K8sObject.(*egv1a1.BackendTrafficPolicy)
	if !ok {
		return nil
	}
	if hasHealthCheck(policy) {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, ref := range policyTargetRefs(policy) {
		if !isRouteKind(ref.Kind) {
			continue
		}
		for _, backendRef := range routeBackendRefs(lintCtx, policy.Namespace, string(ref.Name), ref.Kind) {
			if !isEnvoyGatewayBackendRef(backendRef) {
				continue
			}
			backendNamespace := policy.Namespace
			if backendRef.Namespace != nil {
				backendNamespace = string(*backendRef.Namespace)
			}
			backend := findBackend(lintCtx, backendNamespace, string(backendRef.Name))
			if backend == nil {
				continue
			}
			if hostname, ok := fqdnHostname(backend); ok {
				diagnostics = append(diagnostics, diagnostic.Diagnostic{
					Message: fmt.Sprintf(
						"BackendTrafficPolicy has no health check, but targets %s %q, which routes to "+
							"Backend %q with FQDN endpoint %q; this backend resolves via DNS at startup "+
							"and can 503 before the name resolves",
						ref.Kind, ref.Name, backend.Name, hostname),
				})
			}
		}
	}
	return diagnostics
}

func hasHealthCheck(policy *egv1a1.BackendTrafficPolicy) bool {
	hc := policy.Spec.HealthCheck
	return hc != nil && (hc.Active != nil || hc.Passive != nil)
}

// targetRef is a normalized view of BackendTrafficPolicy's deprecated single
// TargetRef and its current TargetRefs, which share the same Kind/Name shape.
type targetRef struct {
	Kind gatewayv1.Kind
	Name gatewayv1.ObjectName
}

func policyTargetRefs(policy *egv1a1.BackendTrafficPolicy) []targetRef {
	var refs []targetRef
	//nolint:staticcheck // deprecated field still seen in the wild; must still be checked
	if ref := policy.Spec.TargetRef; ref != nil {
		refs = append(refs, targetRef{Kind: ref.Kind, Name: ref.Name})
	}
	for _, r := range policy.Spec.TargetRefs {
		refs = append(refs, targetRef{Kind: r.Kind, Name: r.Name})
	}
	return refs
}

// isRouteKind reports whether kind is a route kind this check can look up
// backendRefs on directly. Gateway-kind and label-selector targets are not
// followed: doing so requires resolving every route attached to a Gateway
// via parentRefs, which is out of scope for this check.
func isRouteKind(kind gatewayv1.Kind) bool {
	return kind == "HTTPRoute" || kind == "GRPCRoute"
}

// routeBackendRefs returns the backendRefs of the named HTTPRoute or
// GRPCRoute in the given namespace.
func routeBackendRefs(lintCtx lintcontext.LintContext, namespace, name string, kind gatewayv1.Kind) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, obj := range lintCtx.Objects() {
		if obj.K8sObject.GetNamespace() != namespace || obj.K8sObject.GetName() != name {
			continue
		}
		switch kind {
		case "HTTPRoute":
			route, ok := obj.K8sObject.(*gatewayv1.HTTPRoute)
			if !ok {
				continue
			}
			for _, rule := range route.Spec.Rules {
				for _, br := range rule.BackendRefs {
					refs = append(refs, br.BackendRef)
				}
			}
		case "GRPCRoute":
			route, ok := obj.K8sObject.(*gatewayv1.GRPCRoute)
			if !ok {
				continue
			}
			for _, rule := range route.Spec.Rules {
				for _, br := range rule.BackendRefs {
					refs = append(refs, br.BackendRef)
				}
			}
		}
	}
	return refs
}

// isEnvoyGatewayBackendRef reports whether ref points at an Envoy Gateway
// Backend object, as opposed to the default (a core Service).
func isEnvoyGatewayBackendRef(ref gatewayv1.BackendRef) bool {
	if ref.Kind == nil || string(*ref.Kind) != egv1a1.KindBackend {
		return false
	}
	return ref.Group != nil && string(*ref.Group) == egv1a1.GroupName
}

func findBackend(lintCtx lintcontext.LintContext, namespace, name string) *egv1a1.Backend {
	for _, obj := range lintCtx.Objects() {
		backend, ok := obj.K8sObject.(*egv1a1.Backend)
		if !ok {
			continue
		}
		if backend.Namespace == namespace && backend.Name == name {
			return backend
		}
	}
	return nil
}

func fqdnHostname(backend *egv1a1.Backend) (string, bool) {
	for _, endpoint := range backend.Spec.Endpoints {
		if endpoint.FQDN != nil && endpoint.FQDN.Hostname != "" {
			return endpoint.FQDN.Hostname, true
		}
	}
	return "", false
}
