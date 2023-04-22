package cache

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"
)

const lastSyncTimeAnnotation = "internal.goofy.cluster.x-k8s.io/last-sync"

type resyncRequest struct {
	resourceGroup string
	gvk           schema.GroupVersionKind
	key           types.NamespacedName
}

func (c *cache) startSyncer(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx).WithValues("controller", "syncer") // TODO: consider if to use something different than controller
	ctx = ctrl.LoggerInto(ctx, log)

	log.Info("Starting syncer queue")
	c.syncQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	go func() {
		<-ctx.Done()
		c.syncQueue.ShutDown()
		// log.Info("Garbage syncer queue stopped")
	}()

	syncLoopStarted := false
	go func() {
		log.Info("Starting sync loop")
		syncLoopStarted = true
		for {
			select {
			case <-time.After(c.syncPeriod / 4):
				c.syncGroup(ctx)
			case <-ctx.Done():
				// log.Info("Sync loop stopped")
				return
			}
		}
	}()

	workers := 0
	go func() {
		log.Info("Starting sync workers", "count", c.syncConcurrency)
		wg := &sync.WaitGroup{}
		wg.Add(c.syncConcurrency)
		for i := 0; i < c.syncConcurrency; i++ {
			go func() {
				workers += 1
				defer wg.Done()
				for c.processSyncWorkItem(ctx) {
				}
			}()
		}
		<-ctx.Done()
		// log.Info("Shutdown signal received, waiting for all workers to finish")
		wg.Wait()
		// log.Info("All workers finished")
	}()

	if err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		if !syncLoopStarted {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to start sync loop: %v", err)
	}

	if err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		if workers < c.syncConcurrency {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to start sync workers: %v", err)
	}
	return nil
}

func (c *cache) syncGroup(ctx context.Context) {
	log := ctrl.LoggerFrom(ctx)

	c.lock.RLock()
	defer c.lock.RUnlock()
	i := 0
	for resourceGroup, tracker := range c.resourceGroups {
		i += c.syncResourceGroupTracker(ctx, resourceGroup, tracker)
	}
	log.Info("Sync loop", "queuedResources", i)
}

func (c *cache) syncResourceGroupTracker(_ context.Context, resourceGroup string, tracker *resourceGroupTracker) int {
	// log := ctrl.LoggerFrom(ctx)

	tracker.lock.RLock()
	defer tracker.lock.RUnlock()

	syncBeforeTime := time.Now().UTC().Add(-c.syncPeriod)
	i := 0
	for gvk, objects := range tracker.objects {
		for key, obj := range objects {
			if lastSync, ok := lastSyncTimeAnnotationValue(obj); ok {
				if !lastSync.Before(syncBeforeTime) {
					// log.Info("Resource", "resourceGroup", resourceGroup, gvk.Kind, res.GetName(), "timeToSync", lastSync.Sub(syncBeforeTime).String())
					continue
				}
			}
			i++
			c.syncQueue.Add(resyncRequest{
				gvk:           gvk,
				resourceGroup: resourceGroup,
				key:           key,
			})
		}
	}
	return i
}

func (c *cache) processSyncWorkItem(ctx context.Context) bool {
	log := ctrl.LoggerFrom(ctx)

	item, shutdown := c.syncQueue.Get()
	if shutdown {
		return false
	}

	defer func() {
		c.syncQueue.Forget(item)
		c.syncQueue.Done(item)
	}()

	rr, ok := item.(resyncRequest)
	if !ok {
		return true
	}

	tracker := c.resourceGroupTracker(rr.resourceGroup)
	if tracker == nil {
		return true
	}

	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	objects, ok := tracker.objects[rr.gvk]
	if !ok {
		return true
	}

	obj, ok := objects[rr.key]
	if !ok {
		return true
	}

	now := time.Now().UTC()
	obj.SetAnnotations(appendAnnotations(obj, lastSyncTimeAnnotation, now.Format(time.RFC3339)))
	tracker.objects[rr.gvk][rr.key] = obj

	log.Info("Object sync triggered", "resourceGroup", rr.resourceGroup, rr.gvk.Kind, rr.key)
	c.informSync(rr.resourceGroup, obj)
	return true
}

func lastSyncTimeAnnotationValue(obj client.Object) (time.Time, bool) {
	value, ok := obj.GetAnnotations()[lastSyncTimeAnnotation]
	if !ok {
		return time.Time{}, false
	}

	valueTime, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return valueTime, true
}
