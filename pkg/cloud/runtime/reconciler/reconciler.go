package reconciler

import (
	"context"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Reconciler interface {
	Reconcile(context.Context, Request) (Result, error)
}

type Request struct {
	ResourceGroup string
	types.NamespacedName
}

func (r Request) String() string {
	if r.ResourceGroup == "" {
		return r.NamespacedName.String()
	}
	return "%s://" + r.ResourceGroup + string(types.Separator) + r.NamespacedName.String()
}

type Result = reconcile.Result
