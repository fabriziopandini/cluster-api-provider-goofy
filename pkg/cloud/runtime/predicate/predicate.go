package predicate

import (
	cevent "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/event"
)

// Predicate filters events before enqueuing the keys.
type Predicate interface {
	// Create returns true if the Create event should be processed
	Create(cevent.CreateEvent) bool

	// Delete returns true if the Delete event should be processed
	Delete(cevent.DeleteEvent) bool

	// Update returns true if the Update event should be processed
	Update(cevent.UpdateEvent) bool

	// Generic returns true if the Generic event should be processed
	Generic(cevent.GenericEvent) bool
}
