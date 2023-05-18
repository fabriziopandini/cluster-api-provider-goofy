package reconcile

import (
	"context"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Reconciler is provided to Controllers at creation time as the API implementation.
type Reconciler interface {
	// Reconcile performs a full reconciliation for the resource referred to by the Request.
	// The Controller will requeue the Request to be processed again if an error is non-nil or
	// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
	Reconcile(context.Context, Request) (Result, error)
}

// Request contains the information necessary to reconcile a resource. This includes the
// information to uniquely identify the resource - the resourceGroup it belongs to, its Name and Namespace.
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

// Result contains the result of a Reconciler invocation.
type Result = reconcile.Result
