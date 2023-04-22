package predicate

import (
	cevent "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/event"
)

type Predicate interface {
	Create(cevent.CreateEvent) bool
	Delete(cevent.DeleteEvent) bool
	Update(cevent.UpdateEvent) bool
	Generic(cevent.GenericEvent) bool
}
