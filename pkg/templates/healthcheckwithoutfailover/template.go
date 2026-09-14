// Package healthcheckwithoutfailover flags a BackendTrafficPolicy that
// configures a health check over a route whose only destination is a Backend
// with a single endpoint. Outlier detection and active health checking work by
// ejecting an unhealthy endpoint from the load balancing set. Eject the only
// endpoint and the set is empty, so every request fails immediately instead of
// timing out against a sick upstream. That is a faster failure, not a more
// available one, and it is usually not what whoever added the health check
// believed they were buying.
//
// A schema validator cannot see this. The policy is valid, the Backend is
// valid, and the problem only exists in the relationship between them: it
// takes resolving the policy's targets to routes, the routes' backendRefs to
// Backends, and counting what those Backends actually contain.
//
// This is the complement of fqdn-backend-cold-start, which reports the policy
// that configures no health check at all. Exactly one of the two can fire on a
// given policy, since this one returns early unless a health check is present
// and that one returns early unless it is absent.
//
// Guard for partial manifest sets: a Backend that is not in the set is never a
// finding. Routes and the Backends they name routinely live in separate repos,
// and this check needs to read a Backend's endpoints to say anything at all,
// so an unresolved backendRef is skipped rather than assumed empty. Likewise a
// backendRef to a Service is skipped, because how many endpoints sit behind a
// Service is not visible in a manifest set.
package healthcheckwithoutfailover

import (
	"fmt"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "health-check-without-failover"

func init() {
	templates.Register(check.Template{
		HumanName: "Health Check Without Failover",
		Key:       templateKey,
		Description: "Flag BackendTrafficPolicies whose health check covers a route served by a single " +
			"Backend endpoint, where ejecting it leaves nowhere to fail over to",
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
	// No health check means nothing ejects anything, which is
	// fqdn-backend-cold-start's finding rather than this one.
	if !hasHealthCheck(policy) {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, coverage := range gatewayapi.BackendTrafficPolicyCoverage(lintCtx, policy) {
		// An inherited policy is displaced entirely by one that names the route,
		// so describing the inherited one's health check would describe config
		// that never applies. The more specific policy is judged on its own.
		if coverage.Inherited && gatewayapi.MoreSpecificPolicyCovers(lintCtx, coverage.Route, nil, policy) {
			continue
		}
		backend, endpoints := soleBackend(lintCtx, coverage)
		if backend == nil || endpoints != 1 {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"BackendTrafficPolicy configures a health check and %s Backend %q, whose only endpoint is %s; "+
					"ejecting it empties the load balancing set, so requests fail fast instead of failing over",
				describeCoverage(coverage), backend.Name, describeEndpoint(backend.Spec.Endpoints[0])),
		})
	}
	return diagnostics
}

// soleBackend returns the single Envoy Gateway Backend a covered route sends
// traffic to, with its endpoint count, or nil when the route's destinations
// are not something this check can judge.
//
// More than one backendRef means the rule has other destinations. Whether
// Envoy actually shifts traffic onto them after an ejection depends on weights
// and on the policy's retry settings, which this check does not model, so
// rather than guess it says nothing. That also covers the deliberate pattern
// of pairing a primary Backend with one marked spec.fallback, which is two
// backendRefs by construction.
func soleBackend(lintCtx lintcontext.LintContext, coverage gatewayapi.CoveredRoute) (*egv1a1.Backend, int) {
	var only *gatewayapi.BackendRef
	for i, ref := range coverage.Route.BackendRefs {
		// A sectionName on a route targetRef narrows the policy to one rule, so
		// backendRefs from the rules it does not name are not this policy's.
		if !gatewayapi.SectionCoversRule(coverage.RuleSection, ref.Rule) {
			continue
		}
		if only != nil {
			return nil, 0
		}
		only = &coverage.Route.BackendRefs[i]
	}
	if only == nil || !isEnvoyGatewayBackendRef(only.Ref) {
		return nil, 0
	}
	// Gateway API defaults an omitted backendRef namespace to the namespace of
	// the route doing the referring, which is not always the policy's: a
	// Gateway-level policy reaches routes in other namespaces.
	backend := findBackend(lintCtx, only.Namespace(coverage.Route.Namespace), string(only.Ref.Name))
	if backend == nil {
		return nil, 0
	}
	// A DynamicResolver Backend has no endpoints to eject; it resolves the
	// upstream per request from the Host header.
	if backend.Spec.Type != nil && *backend.Spec.Type == egv1a1.BackendTypeDynamicResolver {
		return nil, 0
	}
	return backend, len(backend.Spec.Endpoints)
}

func hasHealthCheck(policy *egv1a1.BackendTrafficPolicy) bool {
	hc := policy.Spec.HealthCheck
	return hc != nil && (hc.Active != nil || hc.Passive != nil)
}

// describeCoverage renders how the policy reaches the single-endpoint Backend.
// For a Gateway or ListenerSet target the route is named too, since the policy
// itself never mentions it.
func describeCoverage(coverage gatewayapi.CoveredRoute) string {
	target, route := coverage.Target, coverage.Route
	switch {
	case target.ViaSelector:
		return fmt.Sprintf("selects %s %q, which routes to", route.Kind, route.Name)
	case gatewayapi.IsParentKind(target.Kind):
		return fmt.Sprintf("targets %s %q, whose attached %s %q routes to",
			target.Kind, target.Name, route.Kind, route.Name)
	default:
		return fmt.Sprintf("targets %s %q, which routes to", target.Kind, target.Name)
	}
}

// describeEndpoint names the endpoint the health check would eject, which is
// what makes the finding actionable without opening the Backend.
func describeEndpoint(endpoint egv1a1.BackendEndpoint) string {
	switch {
	case endpoint.FQDN != nil:
		return fmt.Sprintf("FQDN %q on port %d", endpoint.FQDN.Hostname, endpoint.FQDN.Port)
	case endpoint.IP != nil:
		return fmt.Sprintf("IP %q on port %d", endpoint.IP.Address, endpoint.IP.Port)
	case endpoint.Unix != nil:
		return fmt.Sprintf("unix socket %q", endpoint.Unix.Path)
	default:
		return "an endpoint of no recognized type"
	}
}

// isEnvoyGatewayBackendRef reports whether ref points at an Envoy Gateway
// Backend, as opposed to the Gateway API default of a core Service.
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
