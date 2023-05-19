package event

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateEvent is an event where a cloud object was created.
type CreateEvent struct {
	ResourceGroup string
	Object        client.Object
}

// UpdateEvent is an event where a cloud object was updated.
type UpdateEvent struct {
	ResourceGroup string
	ObjectOld     client.Object
	ObjectNew     client.Object
}

// DeleteEvent is an event where a cloud object was deleted.
type DeleteEvent struct {
	ResourceGroup string
	Object        client.Object
}

// GenericEvent is an event where the operation type is unknown (e.g. periodic sync).
type GenericEvent struct {
	ResourceGroup string
	Object        client.Object
}
