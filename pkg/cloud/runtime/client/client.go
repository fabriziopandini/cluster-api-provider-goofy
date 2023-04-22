package client

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Reader interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

type Writer interface {
	Create(ctx context.Context, obj client.Object) error
	Delete(ctx context.Context, obj client.Object) error
	Update(ctx context.Context, obj client.Object) error
}

type Client interface {
	Reader
	Writer
}
