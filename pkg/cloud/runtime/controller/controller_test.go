package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
	ccache "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/cache"
	chandler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/handler"
	creconciler "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/reconcile"
	csource "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/source"
)

var scheme = runtime.NewScheme()

func init() {
	_ = cloudv1.AddToScheme(scheme)
}

type simpleReconciler struct {
	processed chan creconciler.Request
}

func (r *simpleReconciler) Reconcile(_ context.Context, req creconciler.Request) (_ creconciler.Result, err error) {
	r.processed <- req
	return creconciler.Result{}, nil
}

func TestController_Queue(t *testing.T) {
	ctx, cancelFn := context.WithCancel(context.TODO())

	r := &simpleReconciler{
		processed: make(chan creconciler.Request),
	}

	c, err := New("foo", Options{
		Concurrency: 1,
		Reconciler:  r,
	})
	require.NoError(t, err)

	i := &fakeInformer{}

	err = c.Watch(&csource.Kind{
		Type:     &cloudv1.CloudMachine{},
		Informer: i,
	}, &chandler.EnqueueRequestForObject{})
	require.NoError(t, err)

	err = c.Start(ctx)
	require.NoError(t, err)

	i.InformCreate("foo", &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar-create",
		},
	})

	require.Eventually(t, func() bool {
		r := <-r.processed
		return r == creconciler.Request{
			ResourceGroup:  "foo",
			NamespacedName: types.NamespacedName{Name: "bar-create"},
		}
	}, 5*time.Second, 100*time.Millisecond)

	i.InformUpdate("foo", &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar-update",
		},
	}, &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar-update",
		},
	})

	require.Eventually(t, func() bool {
		r := <-r.processed
		return r == creconciler.Request{
			ResourceGroup:  "foo",
			NamespacedName: types.NamespacedName{Name: "bar-update"},
		}
	}, 5*time.Second, 100*time.Millisecond)

	i.InformDelete("foo", &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar-delete",
		},
	})

	require.Eventually(t, func() bool {
		r := <-r.processed
		return r == creconciler.Request{
			ResourceGroup:  "foo",
			NamespacedName: types.NamespacedName{Name: "bar-delete"},
		}
	}, 5*time.Second, 100*time.Millisecond)

	i.InformGeneric("foo", &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar-generic",
		},
	})

	require.Eventually(t, func() bool {
		r := <-r.processed
		return r == creconciler.Request{
			ResourceGroup:  "foo",
			NamespacedName: types.NamespacedName{Name: "bar-generic"},
		}
	}, 5*time.Second, 100*time.Millisecond)

	close(r.processed)
	cancelFn()
}

var _ ccache.Informer = &fakeInformer{}

type fakeInformer struct {
	handler ccache.InformEventHandler
}

func (i *fakeInformer) AddEventHandler(handler ccache.InformEventHandler) error {
	i.handler = handler
	return nil
}

func (i *fakeInformer) InformCreate(resourceGroup string, obj client.Object) {
	i.handler.OnCreate(resourceGroup, obj)
}

func (i *fakeInformer) InformUpdate(resourceGroup string, oldObj, newObj client.Object) {
	i.handler.OnUpdate(resourceGroup, oldObj, newObj)
}

func (i *fakeInformer) InformDelete(resourceGroup string, res client.Object) {
	i.handler.OnDelete(resourceGroup, res)
}

func (i *fakeInformer) InformGeneric(resourceGroup string, res client.Object) {
	i.handler.OnGeneric(resourceGroup, res)
}
