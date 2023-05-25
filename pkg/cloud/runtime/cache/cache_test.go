package cache

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
)

var scheme = runtime.NewScheme()

func init() {
	_ = cloudv1.AddToScheme(scheme)
}

func Test_cache_scale(t *testing.T) {
	t.Skip()
	ctrl.SetLogger(klog.Background())

	resourceGroups := 1000
	objectsForResourceGroups := 500
	operationFrequencyForResourceGroup := 10 * time.Millisecond
	testDuration := 2 * time.Minute

	var createCount uint64
	var getCount uint64
	var listCount uint64
	var deleteCount uint64

	rand.Seed(time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	c := NewCache(scheme).(*cache)
	c.syncPeriod = testDuration / 10                        // force a shorter sync period
	c.garbageCollectorRequeueAfter = 500 * time.Millisecond // force a shorter gc requeueAfter
	err := c.Start(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return c.started
	}, 5*time.Second, 200*time.Millisecond, "manager should start")

	machineName := func(j int) string {
		return fmt.Sprintf("machine-%d", j)
	}

	for i := 0; i < resourceGroups; i++ {
		resourceGroup := fmt.Sprintf("resourceGroup-%d", i)
		c.AddResourceGroup(resourceGroup)

		go func() {
			for {
				select {
				case <-time.After(wait.Jitter(operationFrequencyForResourceGroup, 1)):
					operation := rand.Intn(3)
					item := rand.Intn(objectsForResourceGroups)
					switch operation {
					case 0: // create or get
						machine := &cloudv1.CloudMachine{
							ObjectMeta: metav1.ObjectMeta{
								Name: machineName(item),
							},
						}
						err := c.Create(resourceGroup, machine)
						if apierrors.IsAlreadyExists(err) {
							if err = c.Get(resourceGroup, types.NamespacedName{Name: machineName(item)}, machine); err == nil {
								atomic.AddUint64(&getCount, 1)
								continue
							}
						}
						require.NoError(t, err)
						atomic.AddUint64(&createCount, 1)
					case 1: // list
						obj := &cloudv1.CloudMachineList{}
						err := c.List(resourceGroup, obj)
						require.NoError(t, err)
						atomic.AddUint64(&listCount, 1)
					case 2: // delete
						require.NoError(t, err)
						machine := &cloudv1.CloudMachine{
							ObjectMeta: metav1.ObjectMeta{
								Name: machineName(item),
							},
						}
						err := c.Delete(resourceGroup, machine)
						if apierrors.IsNotFound(err) {
							continue
						}
						require.NoError(t, err)
						atomic.AddUint64(&deleteCount, 1)
					}

				case <-ctx.Done():
					return
				}
			}
		}()
	}

	time.Sleep(1 * time.Minute)

	t.Log("createCount", createCount, "getCount", getCount, "listCount", listCount, "deleteCount", deleteCount)

	cancel()
}
