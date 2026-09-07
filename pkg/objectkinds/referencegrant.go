package objectkinds

import (
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ReferenceGrant represents Gateway API ReferenceGrant objects.
const ReferenceGrant = "ReferenceGrant"

var referenceGrantGVK = schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("ReferenceGrant")

func init() {
	objectkinds.RegisterObjectKind(ReferenceGrant, objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk == referenceGrantGVK
	}))
}
