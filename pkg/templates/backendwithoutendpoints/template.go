// Package backendwithoutendpoints flags an Envoy Gateway Backend that
// declares no endpoints. Every backendRef resolving to it programs an Envoy
// cluster with no members, so each request through it fails at the gateway
// and never reaches anything.
//
// A schema validator does not see this. The Backend CRD requires only
// spec, and its MinItems=1 on spec.endpoints applies only when the list is
// present, so a Backend rendered with the endpoints block missing entirely
// applies cleanly. That is the usual shape of the bug: a Helm values key that
// was renamed or left unset renders `spec: {}` and nothing complains.
//
// Nothing here resolves a reference, so the partial-manifest-set guard other
// checks need does not apply: the finding is decided entirely from the object
// in front of the check, and linting a Backend on its own is enough to judge
// it.
package backendwithoutendpoints

import (
	"fmt"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "backend-without-endpoints"

func init() {
	templates.Register(check.Template{
		HumanName: "Backend Without Endpoints",
		Key:       templateKey,
		Description: "Flag Envoy Gateway Backends that declare no endpoints, so every backendRef " +
			"resolving to them has nothing to route to",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.Backend},
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	backend, ok := object.K8sObject.(*egv1a1.Backend)
	if !ok {
		return nil
	}
	// A DynamicResolver Backend takes its upstream address from the request's
	// Host header, so carrying no endpoints is not just allowed, the CRD
	// forbids anything else.
	if backendType(backend) == egv1a1.BackendTypeDynamicResolver {
		return nil
	}
	if len(backend.Spec.Endpoints) > 0 {
		return nil
	}
	return []diagnostic.Diagnostic{{
		Message: fmt.Sprintf(
			"Backend %q is of type %q but declares no endpoints; "+
				"it programs a cluster with no members, so every backendRef resolving to it fails at the gateway",
			backend.Name, egv1a1.BackendTypeEndpoints),
	}}
}

// backendType reports the Backend's type with the CRD's default applied. The
// field is a pointer with a kubebuilder default of Endpoints, so an omitted
// type is an endpoints-based Backend rather than an unknown one.
func backendType(backend *egv1a1.Backend) egv1a1.BackendType {
	if backend.Spec.Type == nil {
		return egv1a1.BackendTypeEndpoints
	}
	return *backend.Spec.Type
}
