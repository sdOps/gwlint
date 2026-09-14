package retrywithoutbudget

import (
	"fmt"
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	policyName = "checkout-retry-policy"
	namespace  = "default"
)

func TestRetryWithoutBudget(t *testing.T) {
	suite.Run(t, new(RetryWithoutBudgetTestSuite))
}

type RetryWithoutBudgetTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *RetryWithoutBudgetTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func int32Ptr(i int32) *int32 { return &i }

func durationPtr(d gatewayv1.Duration) *gatewayv1.Duration { return &d }

func (s *RetryWithoutBudgetTestSuite) addPolicy(name string, retry *egv1a1.Retry) {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: egv1a1.BackendTrafficPolicySpec{
			ClusterSettings: egv1a1.ClusterSettings{Retry: retry},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject(name, policy)
}

func want(numRetries int32) string {
	return fmt.Sprintf(
		"BackendTrafficPolicy sets spec.retry.numRetries to %d with no spec.retry.perRetry.timeout "+
			"and no spec.retry.perRetry.backOff interval, so one request can become %d unbounded "+
			"attempts and retries will amplify load on a backend that is already failing",
		numRetries, numRetries+1)
}

func (s *RetryWithoutBudgetTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *RetryWithoutBudgetTestSuite) TestFlagsNumRetriesWithNoCapAtAll() {
	s.addPolicy(policyName, &egv1a1.Retry{NumRetries: int32Ptr(5)})

	s.expect(map[string][]string{policyName: {want(5)}})
}

// An empty perRetry block configures neither a deadline nor a spacing, so it
// caps nothing.
func (s *RetryWithoutBudgetTestSuite) TestFlagsAnEmptyPerRetryBlock() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(3),
		PerRetry:   &egv1a1.PerRetryPolicy{},
	})

	s.expect(map[string][]string{policyName: {want(3)}})
}

func (s *RetryWithoutBudgetTestSuite) TestFlagsAnEmptyBackOffBlock() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(3),
		PerRetry:   &egv1a1.PerRetryPolicy{BackOff: &egv1a1.BackOffPolicy{}},
	})

	s.expect(map[string][]string{policyName: {want(3)}})
}

func (s *RetryWithoutBudgetTestSuite) TestPassesWithAPerRetryTimeout() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(5),
		PerRetry:   &egv1a1.PerRetryPolicy{Timeout: durationPtr("2s")},
	})

	s.expect(nil)
}

// maxInterval defaults to ten times baseInterval, so a baseInterval on its own
// already bounds how far the retries spread out.
func (s *RetryWithoutBudgetTestSuite) TestPassesWithABaseInterval() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(5),
		PerRetry: &egv1a1.PerRetryPolicy{
			BackOff: &egv1a1.BackOffPolicy{BaseInterval: durationPtr("100ms")},
		},
	})

	s.expect(nil)
}

func (s *RetryWithoutBudgetTestSuite) TestPassesWithAMaxIntervalAlone() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(5),
		PerRetry: &egv1a1.PerRetryPolicy{
			BackOff: &egv1a1.BackOffPolicy{MaxInterval: durationPtr("10s")},
		},
	})

	s.expect(nil)
}

// An empty duration string is a field that was written but says nothing, which
// is no more of a cap than leaving it out.
func (s *RetryWithoutBudgetTestSuite) TestTreatsAnEmptyTimeoutAsUnset() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(4),
		PerRetry:   &egv1a1.PerRetryPolicy{Timeout: durationPtr("")},
	})

	s.expect(map[string][]string{policyName: {want(4)}})
}

func (s *RetryWithoutBudgetTestSuite) TestPassesWhenNoRetryIsConfigured() {
	s.addPolicy(policyName, nil)

	s.expect(nil)
}

// An omitted numRetries leaves Envoy Gateway's default of 2, which nobody
// chose, so the policy is not the thing amplifying anything.
func (s *RetryWithoutBudgetTestSuite) TestPassesWhenNumRetriesIsNotSet() {
	s.addPolicy(policyName, &egv1a1.Retry{
		RetryOn: &egv1a1.RetryOn{Triggers: []egv1a1.TriggerEnum{egv1a1.ConnectFailure}},
	})

	s.expect(nil)
}

func (s *RetryWithoutBudgetTestSuite) TestPassesWhenRetriesAreTurnedOff() {
	s.addPolicy(policyName, &egv1a1.Retry{NumRetries: int32Ptr(0)})

	s.expect(nil)
}

// Other fields on the retry block do not bound what a retry costs, so they do
// not close the gap.
func (s *RetryWithoutBudgetTestSuite) TestRetryOnAloneDoesNotCount() {
	s.addPolicy(policyName, &egv1a1.Retry{
		NumRetries: int32Ptr(2),
		RetryOn:    &egv1a1.RetryOn{Triggers: []egv1a1.TriggerEnum{egv1a1.Error5XX}},
	})

	s.expect(map[string][]string{policyName: {want(2)}})
}

func (s *RetryWithoutBudgetTestSuite) TestJudgesEveryPolicyIndependently() {
	s.addPolicy(policyName, &egv1a1.Retry{NumRetries: int32Ptr(3)})
	s.addPolicy("orders-retry-policy", &egv1a1.Retry{
		NumRetries: int32Ptr(3),
		PerRetry:   &egv1a1.PerRetryPolicy{Timeout: durationPtr("1s")},
	})

	s.expect(map[string][]string{policyName: {want(3)}})
}
