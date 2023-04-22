package source

import (
	"context"
	chandler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/handler"
	cpredicate "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/predicate"
	"k8s.io/client-go/util/workqueue"
)

type Source interface {
	Start(context.Context, chandler.EventHandler, workqueue.RateLimitingInterface, ...cpredicate.Predicate) error
}
