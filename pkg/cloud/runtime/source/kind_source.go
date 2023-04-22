package source

import (
	"context"
	"fmt"
	ccache "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/cache"
	cevent "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/event"
	chandler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/handler"
	cpredicate "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/predicate"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Kind struct {
	Type     client.Object
	Informer ccache.Informer
}

var _ Source = &Kind{}

func (ks *Kind) Start(ctx context.Context, handler chandler.EventHandler, queue workqueue.RateLimitingInterface, prct ...cpredicate.Predicate) error {
	if ks.Type == nil {
		return fmt.Errorf("must specify Kind.Type")
	}

	if ks.Informer == nil {
		return fmt.Errorf("must specify Kind.Informer")
	}

	return ks.Informer.AddEventHandler(informerEventHandler{Queue: queue, EventHandler: handler, Predicates: prct})
}

func (ks *Kind) String() string {
	if ks.Type != nil {
		return fmt.Sprintf("kind source: %T", ks.Type)
	}
	return "kind source: unknown type"
}

var _ ccache.InformEventHandler = informerEventHandler{}

type informerEventHandler struct {
	EventHandler chandler.EventHandler
	Queue        workqueue.RateLimitingInterface
	Predicates   []cpredicate.Predicate
}

func (e informerEventHandler) OnCreate(resourceGroup string, obj client.Object) {
	c := cevent.CreateEvent{
		ResourceGroup: resourceGroup,
		Object:        obj,
	}
	for _, p := range e.Predicates {
		if !p.Create(c) {
			return
		}
	}
	e.EventHandler.Create(c, e.Queue)
}

func (e informerEventHandler) OnUpdate(resourceGroup string, oldObj, newObj client.Object) {
	u := cevent.UpdateEvent{
		ResourceGroup: resourceGroup,
		ObjectOld:     oldObj,
		ObjectNew:     newObj,
	}
	for _, p := range e.Predicates {
		if !p.Update(u) {
			return
		}
	}
	e.EventHandler.Update(u, e.Queue)
}

func (e informerEventHandler) OnDelete(resourceGroup string, obj client.Object) {
	d := cevent.DeleteEvent{
		ResourceGroup: resourceGroup,
		Object:        obj,
	}
	for _, p := range e.Predicates {
		if !p.Delete(d) {
			return
		}
	}
	e.EventHandler.Delete(d, e.Queue)
}

func (e informerEventHandler) OnGeneric(resourceGroup string, obj client.Object) {
	d := cevent.GenericEvent{
		ResourceGroup: resourceGroup,
		Object:        obj,
	}
	for _, p := range e.Predicates {
		if !p.Generic(d) {
			return
		}
	}
	e.EventHandler.Generic(d, e.Queue)
}
