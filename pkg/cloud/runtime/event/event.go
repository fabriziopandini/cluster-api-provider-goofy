package event

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CreateEvent struct {
	ResourceGroup string
	Object        client.Object
}

type UpdateEvent struct {
	ResourceGroup string
	ObjectOld     client.Object
	ObjectNew     client.Object
}

type DeleteEvent struct {
	ResourceGroup string
	Object        client.Object
}

type GenericEvent struct {
	ResourceGroup string
	Object        client.Object
}
