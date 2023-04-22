package handler

import (
	cevent "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/event"
	"k8s.io/client-go/util/workqueue"
)

type EventHandler interface {
	Create(cevent.CreateEvent, workqueue.RateLimitingInterface)
	Update(cevent.UpdateEvent, workqueue.RateLimitingInterface)
	Delete(cevent.DeleteEvent, workqueue.RateLimitingInterface)
	Generic(cevent.GenericEvent, workqueue.RateLimitingInterface)
}
