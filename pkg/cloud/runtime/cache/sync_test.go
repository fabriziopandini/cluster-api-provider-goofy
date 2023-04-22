package cache

import (
	"context"
	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
	"time"
)

func Test_cache_sync(t *testing.T) {
	// ctrl.SetLogger(NewLoggerWithT(t))

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	c := NewCache(scheme).(*cache)
	c.syncPeriod = 5 * time.Second // force a shorter sync period
	h := &fakeHandler{}
	i, err := c.GetInformer(ctx, &cloudv1.CloudMachine{})
	require.NoError(t, err)
	err = i.AddEventHandler(h)
	require.NoError(t, err)

	err = c.Start(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return c.started
	}, 5*time.Second, 200*time.Millisecond, "manager should start")

	c.AddResourceGroup("foo")

	obj := &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "baz",
		},
	}
	err = c.Create("foo", obj)
	require.NoError(t, err)

	objBefore := &cloudv1.CloudMachine{}
	err = c.Get("foo", types.NamespacedName{Name: "baz"}, objBefore)
	require.NoError(t, err)

	lastSyncBefore, ok := lastSyncTimeAnnotationValue(objBefore)
	require.True(t, ok)

	require.Eventually(t, func() bool {
		objAfter := &cloudv1.CloudMachine{}
		err = c.Get("foo", types.NamespacedName{Name: "baz"}, objAfter)
		if err != nil {
			return false
		}
		lastSyncAfter, ok := lastSyncTimeAnnotationValue(objAfter)
		if !ok {
			return false
		}
		if lastSyncBefore != lastSyncAfter {
			return true
		}
		return false
	}, 10*time.Second, 200*time.Millisecond, "object should be synced")

	require.Contains(t, h.Events(), "foo, CloudMachine=baz, Synced")

	cancel()
}
