package source

import (
	"context"

	"k8s.io/client-go/util/workqueue"

	chandler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/handler"
	cpredicate "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/predicate"
)

// Source is a source of events (e.g. Create, Update, Delete operations on resources)
// which should be processed by event.EventHandlers to enqueue reconcile.Requests.
type Source interface {
	// Start is internal and should be called only by the Controller to register an EventHandler with the Informer
	// to enqueue reconcile.Requests.
	Start(context.Context, chandler.EventHandler, workqueue.RateLimitingInterface, ...cpredicate.Predicate) error
}
