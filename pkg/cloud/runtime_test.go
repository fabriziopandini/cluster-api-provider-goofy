package cloud

import (
	"context"
	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
	creconciler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/reconciler"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"
)

var scheme = runtime.NewScheme()

func init() {
	cloudv1.AddToScheme(scheme)
}

type simpleReconciler struct {
	mgr Manager
}

func (r *simpleReconciler) SetupWithManager(_ context.Context, mgr Manager) error {
	return NewControllerManagedBy(mgr).
		For(&cloudv1.CloudMachine{}).
		Complete(r)
}

func (r *simpleReconciler) Reconcile(ctx context.Context, req creconciler.Request) (creconciler.Result, error) {
	client := r.mgr.GetResourceGroup(req.ResourceGroup).GetClient()

	obj := &cloudv1.CloudMachine{}
	if err := client.Get(ctx, req.NamespacedName, obj); err != nil {
		return creconciler.Result{}, err
	}

	obj.Annotations = map[string]string{"reconciled": "true"}
	if err := client.Update(ctx, obj); err != nil {
		return creconciler.Result{}, err
	}
	return creconciler.Result{}, nil
}

func TestController_Queue(t *testing.T) {
	ctx, cancelFn := context.WithCancel(context.TODO())

	mgr := NewManager(scheme)
	mgr.AddResourceGroup("foo")

	c := mgr.GetResourceGroup("foo").GetClient()

	err := (&simpleReconciler{
		mgr: mgr,
	}).SetupWithManager(ctx, mgr)
	require.NoError(t, err)

	err = mgr.Start(ctx)
	require.NoError(t, err)

	obj := &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar-reconcile",
		},
	}
	err = c.Create(ctx, obj)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		obj2 := &cloudv1.CloudMachine{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj2); err != nil {
			return false
		}
		return obj2.Annotations["reconciled"] == "true"
	}, 5*time.Second, 100*time.Millisecond)

	cancelFn()
}
