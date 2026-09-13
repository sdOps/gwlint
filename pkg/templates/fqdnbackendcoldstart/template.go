// Package fqdnbackendcoldstart flags BackendTrafficPolicies that leave a
// cold-start gap: no health check, covering a route whose backend resolves
// by FQDN. Envoy Gateway automatically puts FQDN-endpoint Backends on the
// same DNS-resolving cluster path the legacy STRICT_DNS Envoy cluster type
// used, so a request that arrives before the name resolves 503s, and with
// no health check there is nothing to route around it during that window.
// This mirrors a real incident: a cold-start DNS resolution gap caused 503s
// on FQDN-backed routes. See the README for the incident writeup and remediation.
//
// Route resolution lives in routes.go and policy-to-route resolution in
// coverage.go.
package fqdnbackendcoldstart

import (
	"fmt"
	"strings"

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
	for _, coverage := range policyCoverage(lintCtx, policy) {
		ref, route := coverage.ref, coverage.route
		for _, backendRef := range route.BackendRefs {
			// A sectionName on a route targetRef narrows the policy to one rule.
			if !sectionCoversRule(coverage.ruleSection, backendRef.Rule) {
				continue
			}
			if !isEnvoyGatewayBackendRef(backendRef.Ref) {
				continue
			}
			// An inherited policy is displaced by a more specific one that does
			// configure a health check, so the gap is already closed there.
			if coverage.inherited && isOverridden(lintCtx, route, backendRef.Rule, policy) {
				continue
			}
			// Gateway API defaults an omitted backendRef namespace to the
			// namespace of the route doing the referring, which is not always
			// the policy's: a Gateway-level policy reaches routes in other
			// namespaces that attach to it via parentRefs.
			backendNamespace := route.Namespace
			if backendRef.Ref.Namespace != nil {
				backendNamespace = string(*backendRef.Ref.Namespace)
			}
			backend := findBackend(lintCtx, backendNamespace, string(backendRef.Ref.Name))
			if backend == nil {
				continue
			}
			if hostnames := fqdnHostnames(backend); len(hostnames) > 0 {
				diagnostics = append(diagnostics, diagnostic.Diagnostic{
					Message: fmt.Sprintf(
						"BackendTrafficPolicy has no health check, but %s Backend %q with %s; "+
							"this backend resolves via DNS at startup and can 503 "+
							"before the name resolves",
						describeCoverage(ref, route), backend.Name, describeEndpoints(hostnames)),
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

// describeCoverage renders how the policy reaches the FQDN-backed route, as
// the clause leading into the Backend name in the diagnostic message. For a
// Gateway or ListenerSet targetRef the route carrying the FQDN backend is
// named too, since the policy itself never mentions it.
func describeCoverage(ref targetRef, route resolvedRoute) string {
	switch {
	case ref.viaSelector:
		return fmt.Sprintf("selects %s %q, which routes to", route.Kind, route.Name)
	case isParentKind(ref.Kind):
		return fmt.Sprintf("targets %s %q, whose attached %s %q routes to", ref.Kind, ref.Name, route.Kind, route.Name)
	default:
		return fmt.Sprintf("targets %s %q, which routes to", ref.Kind, ref.Name)
	}
}

// describeEndpoints names every FQDN endpoint on the backend, since a backend
// with several of them has several names to resolve, not one.
func describeEndpoints(hostnames []string) string {
	quoted := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		quoted = append(quoted, fmt.Sprintf("%q", h))
	}
	if len(quoted) == 1 {
		return "FQDN endpoint " + quoted[0]
	}
	return fmt.Sprintf("FQDN endpoints %s and %s",
		strings.Join(quoted[:len(quoted)-1], ", "), quoted[len(quoted)-1])
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

// fqdnHostnames returns every FQDN endpoint hostname on the backend. A Backend
// can carry several endpoints, and any one of them resolving by name is a
// cold-start risk, so all of them are reported rather than just the first.
func fqdnHostnames(backend *egv1a1.Backend) []string {
	var hostnames []string
	for _, endpoint := range backend.Spec.Endpoints {
		if endpoint.FQDN != nil && endpoint.FQDN.Hostname != "" {
			hostnames = append(hostnames, endpoint.FQDN.Hostname)
		}
	}
	return hostnames
}
