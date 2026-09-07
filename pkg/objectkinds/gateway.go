package objectkinds

import (
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// Gateway represents Gateway API Gateway objects.
const Gateway = "Gateway"

var gatewayGVK = schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("Gateway")

func init() {
	objectkinds.RegisterObjectKind(Gateway, objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk == gatewayGVK
	}))
}
