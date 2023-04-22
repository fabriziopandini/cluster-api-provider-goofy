package handler

import (
	cevent "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/event"
	creconciler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/reconciler"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ EventHandler = &EnqueueRequestForObject{}

type EnqueueRequestForObject struct{}

func (e *EnqueueRequestForObject) Create(evt cevent.CreateEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}
	q.Add(creconciler.Request{
		ResourceGroup: evt.ResourceGroup,
		NamespacedName: types.NamespacedName{
			Namespace: evt.Object.GetNamespace(),
			Name:      evt.Object.GetName(),
		},
	})
}

func (e *EnqueueRequestForObject) Update(evt cevent.UpdateEvent, q workqueue.RateLimitingInterface) {
	switch {
	case evt.ObjectNew != nil:
		q.Add(creconciler.Request{
			ResourceGroup: evt.ResourceGroup,
			NamespacedName: types.NamespacedName{
				Namespace: evt.ObjectNew.GetNamespace(),
				Name:      evt.ObjectNew.GetName(),
			},
		})
	case evt.ObjectOld != nil:
		if evt.ObjectNew != nil && client.ObjectKeyFromObject(evt.ObjectNew) == client.ObjectKeyFromObject(evt.ObjectOld) {
			return
		}
		q.Add(creconciler.Request{
			ResourceGroup: evt.ResourceGroup,
			NamespacedName: types.NamespacedName{
				Namespace: evt.ObjectOld.GetNamespace(),
				Name:      evt.ObjectOld.GetName(),
			},
		})
	}
}

func (e *EnqueueRequestForObject) Delete(evt cevent.DeleteEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}
	q.Add(creconciler.Request{
		ResourceGroup: evt.ResourceGroup,
		NamespacedName: types.NamespacedName{
			Namespace: evt.Object.GetNamespace(),
			Name:      evt.Object.GetName(),
		},
	})
}

func (e *EnqueueRequestForObject) Generic(evt cevent.GenericEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}
	q.Add(creconciler.Request{
		ResourceGroup: evt.ResourceGroup,
		NamespacedName: types.NamespacedName{
			Namespace: evt.Object.GetNamespace(),
			Name:      evt.Object.GetName(),
		},
	})
}
