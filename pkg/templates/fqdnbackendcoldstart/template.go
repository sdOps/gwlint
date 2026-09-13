// Package fqdnbackendcoldstart flags BackendTrafficPolicies that leave a
// cold-start gap: no health check, covering a route whose backend resolves
// by FQDN. Envoy Gateway automatically puts FQDN-endpoint Backends on the
// same DNS-resolving cluster path the legacy STRICT_DNS Envoy cluster type
// used, so a request that arrives before the name resolves 503s, and with
// no health check there is nothing to route around it during that window.
// This mirrors a real incident: a cold-start DNS resolution gap caused 503s
// on FQDN-backed routes. See the README for the incident writeup and remediation.
package fqdnbackendcoldstart

import (
	"fmt"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
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
		for _, route := range resolveRoutes(lintCtx, policy.Namespace, ref) {
			for _, backendRef := range route.BackendRefs {
				if !isEnvoyGatewayBackendRef(backendRef) {
					continue
				}
				// Gateway API defaults an omitted backendRef namespace to the
				// namespace of the route doing the referring, which is not
				// always the policy's: a Gateway-level policy reaches routes in
				// other namespaces that attach to it via parentRefs.
				backendNamespace := route.Namespace
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
							"BackendTrafficPolicy has no health check, but %s Backend %q with FQDN "+
								"endpoint %q; this backend resolves via DNS at startup and can 503 "+
								"before the name resolves",
							describeCoverage(ref, route), backend.Name, hostname),
					})
				}
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

// resolvedRoute is an HTTPRoute or GRPCRoute this check has resolved a
// BackendTrafficPolicy's targetRef down to, along with the backendRefs to
// inspect for FQDN-endpoint Backends.
type resolvedRoute struct {
	Kind        gatewayv1.Kind
	Namespace   string
	Name        gatewayv1.ObjectName
	ParentRefs  []gatewayv1.ParentReference
	BackendRefs []gatewayv1.BackendRef
}

// resolveRoutes returns the routes a BackendTrafficPolicy's targetRef
// actually covers. A route-kind targetRef (HTTPRoute/GRPCRoute) resolves to
// that route directly. A Gateway-kind targetRef resolves to every route
// attached to that Gateway via parentRefs, since a Gateway-level policy
// applies to all of them by default (Gateway API's policy attachment
// hierarchy). TargetSelectors and ListenerSet targets are not resolved:
// doing so needs its own investigation and is left for a follow-on check.
func resolveRoutes(lintCtx lintcontext.LintContext, policyNamespace string, ref targetRef) []resolvedRoute {
	switch ref.Kind {
	case "HTTPRoute", "GRPCRoute":
		obj := findRoute(lintCtx, policyNamespace, string(ref.Name), ref.Kind)
		if obj == nil {
			return nil
		}
		return []resolvedRoute{*obj}
	case "Gateway":
		return routesAttachedToGateway(lintCtx, policyNamespace, string(ref.Name))
	default:
		return nil
	}
}

func findRoute(lintCtx lintcontext.LintContext, namespace, name string, kind gatewayv1.Kind) *resolvedRoute {
	for _, obj := range lintCtx.Objects() {
		if obj.K8sObject.GetNamespace() != namespace || obj.K8sObject.GetName() != name {
			continue
		}
		if route, ok := asResolvedRoute(obj.K8sObject); ok && route.Kind == kind {
			return &route
		}
	}
	return nil
}

// routesAttachedToGateway returns every HTTPRoute/GRPCRoute in lintCtx whose
// parentRefs name the given Gateway.
func routesAttachedToGateway(lintCtx lintcontext.LintContext, gatewayNamespace, gatewayName string) []resolvedRoute {
	var routes []resolvedRoute
	for _, obj := range lintCtx.Objects() {
		route, ok := asResolvedRoute(obj.K8sObject)
		if !ok {
			continue
		}
		if parentRefsMatchGateway(route.ParentRefs, route.Namespace, gatewayNamespace, gatewayName) {
			routes = append(routes, route)
		}
	}
	return routes
}

func parentRefsMatchGateway(parentRefs []gatewayv1.ParentReference, routeNamespace, gatewayNamespace, gatewayName string) bool {
	for _, p := range parentRefs {
		if p.Group != nil && string(*p.Group) != gatewayv1.GroupName {
			continue
		}
		if p.Kind != nil && string(*p.Kind) != "Gateway" {
			continue
		}
		ns := routeNamespace
		if p.Namespace != nil {
			ns = string(*p.Namespace)
		}
		if ns == gatewayNamespace && string(p.Name) == gatewayName {
			return true
		}
	}
	return false
}

// asResolvedRoute extracts the kind, name, and backendRefs from obj if it is
// an HTTPRoute or GRPCRoute, and reports whether obj was one of those kinds.
func asResolvedRoute(obj k8sutil.Object) (resolvedRoute, bool) {
	switch route := obj.(type) {
	case *gatewayv1.HTTPRoute:
		var refs []gatewayv1.BackendRef
		for _, rule := range route.Spec.Rules {
			for _, br := range rule.BackendRefs {
				refs = append(refs, br.BackendRef)
			}
		}
		return newResolvedRoute("HTTPRoute", route.Namespace, route.Name, route.Spec.ParentRefs, refs), true
	case *gatewayv1.GRPCRoute:
		var refs []gatewayv1.BackendRef
		for _, rule := range route.Spec.Rules {
			for _, br := range rule.BackendRefs {
				refs = append(refs, br.BackendRef)
			}
		}
		return newResolvedRoute("GRPCRoute", route.Namespace, route.Name, route.Spec.ParentRefs, refs), true
	default:
		return resolvedRoute{}, false
	}
}

func newResolvedRoute(kind gatewayv1.Kind, namespace, name string, parentRefs []gatewayv1.ParentReference, backendRefs []gatewayv1.BackendRef) resolvedRoute {
	return resolvedRoute{
		Kind:        kind,
		Namespace:   namespace,
		Name:        gatewayv1.ObjectName(name),
		ParentRefs:  parentRefs,
		BackendRefs: backendRefs,
	}
}

// describeCoverage renders how the policy reaches the FQDN-backed route, as
// the clause leading into the Backend name in the diagnostic message. For a
// Gateway-kind targetRef the route carrying the FQDN backend is named too,
// since the policy itself never mentions it.
func describeCoverage(ref targetRef, route resolvedRoute) string {
	if ref.Kind == "Gateway" {
		return fmt.Sprintf("targets Gateway %q, whose attached %s %q routes to", ref.Name, route.Kind, route.Name)
	}
	return fmt.Sprintf("targets %s %q, which routes to", ref.Kind, ref.Name)
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
