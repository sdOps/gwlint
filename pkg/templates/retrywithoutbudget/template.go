// Package retrywithoutbudget flags a BackendTrafficPolicy that raises
// spec.retry.numRetries without bounding what a retry costs. Envoy applies no
// per-attempt deadline of its own, so with neither spec.retry.perRetry.timeout
// nor a spec.retry.perRetry.backOff interval every request becomes numRetries+1
// attempts that can each run for the full route timeout, back to back. The
// failure mode only shows up under load: when the backend starts erroring, the
// gateway multiplies the traffic reaching it by numRetries+1 and keeps the
// backend down. Envoy Gateway has no retry budget field, so the per-retry
// timeout and the backoff interval are the only caps available.
//
// A schema validator cannot see this. Every field here is optional and the
// manifest is entirely valid; what is wrong is the combination.
//
// The check reads one object and resolves no references, so no partial-set
// guard applies: the policy carries everything needed to judge it.
package retrywithoutbudget

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

const templateKey = "retry-without-budget"

func init() {
	templates.Register(check.Template{
		HumanName: "Retry Without Budget",
		Key:       templateKey,
		Description: "Flag BackendTrafficPolicies that set spec.retry.numRetries with no per-retry timeout " +
			"and no backoff interval, so retries can amplify load on a backend that is already failing",
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

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	policy, ok := object.K8sObject.(*egv1a1.BackendTrafficPolicy)
	if !ok {
		return nil
	}
	retry := policy.Spec.Retry
	if retry == nil {
		return nil
	}
	// An omitted numRetries leaves Envoy Gateway's own default of 2, which is
	// the implementation's choice rather than an amplification factor somebody
	// picked, and numRetries: 0 turns retries off outright.
	if retry.NumRetries == nil || *retry.NumRetries == 0 {
		return nil
	}
	if hasPerRetryTimeout(retry) || hasBackOffInterval(retry) {
		return nil
	}

	numRetries := *retry.NumRetries
	return []diagnostic.Diagnostic{{
		Message: fmt.Sprintf(
			"BackendTrafficPolicy sets spec.retry.numRetries to %d with no spec.retry.perRetry.timeout "+
				"and no spec.retry.perRetry.backOff interval, so one request can become %d unbounded "+
				"attempts and retries will amplify load on a backend that is already failing",
			numRetries, numRetries+1),
	}}
}

// hasPerRetryTimeout reports whether each attempt is deadlined. Without it an
// attempt runs until the route timeout, so numRetries attempts stack serially.
func hasPerRetryTimeout(retry *egv1a1.Retry) bool {
	return retry.PerRetry != nil && retry.PerRetry.Timeout != nil && *retry.PerRetry.Timeout != ""
}

// hasBackOffInterval reports whether retries are spaced out at all. Envoy
// Gateway defaults maxInterval to ten times baseInterval, so either field on
// its own already bounds the spacing; an empty backOff block sets neither and
// changes nothing.
func hasBackOffInterval(retry *egv1a1.Retry) bool {
	if retry.PerRetry == nil || retry.PerRetry.BackOff == nil {
		return false
	}
	backOff := retry.PerRetry.BackOff
	return (backOff.BaseInterval != nil && *backOff.BaseInterval != "") ||
		(backOff.MaxInterval != nil && *backOff.MaxInterval != "")
}
