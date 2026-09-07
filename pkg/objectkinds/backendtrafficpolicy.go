package objectkinds

import (
	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// BackendTrafficPolicy represents Envoy Gateway BackendTrafficPolicy objects.
const BackendTrafficPolicy = "BackendTrafficPolicy"

var backendTrafficPolicyGVK = egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy)

func init() {
	objectkinds.RegisterObjectKind(BackendTrafficPolicy, objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk == backendTrafficPolicyGVK
	}))
}
