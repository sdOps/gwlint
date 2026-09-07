package objectkinds

import (
	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Backend represents Envoy Gateway Backend objects.
const Backend = "Backend"

var backendGVK = egv1a1.GroupVersion.WithKind(egv1a1.KindBackend)

func init() {
	objectkinds.RegisterObjectKind(Backend, objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk == backendGVK
	}))
}
