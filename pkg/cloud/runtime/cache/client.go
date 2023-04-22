package cache

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

func (c *cache) Get(resourceGroup string, key client.ObjectKey, obj client.Object) error {
	if resourceGroup == "" {
		return errors.NewBadRequest("resourceGroup must not be empty")
	}

	if key.Name == "" {
		return errors.NewBadRequest("key.Name must not be empty")
	}

	if obj == nil {
		return errors.NewBadRequest("object must not be nil")
	}

	gvk, err := c.gvkGetAndSet(obj)
	if err != nil {
		return err
	}

	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return errors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.RLock()
	defer tracker.lock.RUnlock()

	objects, ok := tracker.objects[gvk]
	if !ok {
		return errors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	trackedObj, ok := objects[key]
	if !ok {
		return errors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if err := c.scheme.Convert(trackedObj, obj, nil); err != nil {
		return errors.NewInternalError(err)
	}
	obj.GetObjectKind().SetGroupVersionKind(trackedObj.GetObjectKind().GroupVersionKind())

	return nil
}

func (c *cache) List(resourceGroup string, list client.ObjectList, opts ...client.ListOption) error {
	if resourceGroup == "" {
		return errors.NewBadRequest("resourceGroup must not be empty")
	}

	if list == nil {
		return errors.NewBadRequest("list must not be be nil")
	}

	gvk, err := c.gvkGetAndSet(list)
	if err != nil {
		return err
	}

	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return errors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.RLock()
	defer tracker.lock.RUnlock()

	items := make([]runtime.Object, 0)
	objects, ok := tracker.objects[unsafeGuessObjectKindFromList(gvk)]
	if ok {
		listOpts := client.ListOptions{}
		listOpts.ApplyOptions(opts)

		for _, obj := range objects {
			if listOpts.LabelSelector != nil && !listOpts.LabelSelector.Empty() {
				objMeta, err := meta.Accessor(obj)
				if err != nil {
					return errors.NewInternalError(err)
				}

				metaLabels := labels.Set(objMeta.GetLabels())
				if !listOpts.LabelSelector.Matches(metaLabels) {
					continue
				}
			}

			obj := obj.DeepCopyObject().(client.Object)
			items = append(items, obj)
		}
	}

	if err := meta.SetList(list, items); err != nil {
		return errors.NewInternalError(err)
	}
	return nil
}

func (c *cache) Create(resourceGroup string, obj client.Object) error {
	return c.store(resourceGroup, obj, false)
}

func (c *cache) Update(resourceGroup string, obj client.Object) error {
	return c.store(resourceGroup, obj, true)
}

func (c *cache) store(resourceGroup string, obj client.Object, replaceExisting bool) error {
	if resourceGroup == "" {
		return errors.NewBadRequest("resourceGroup must not be empty")
	}

	if obj == nil {
		return errors.NewBadRequest("object must not be nil")
	}

	gvk, err := c.gvkGetAndSet(obj)
	if err != nil {
		return err
	}

	if replaceExisting && obj.GetName() == "" {
		return errors.NewBadRequest("object name must not be empty")
	}

	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return errors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	obj = obj.DeepCopyObject().(client.Object)
	for _, o := range obj.GetOwnerReferences() {
		oRef, err := newOwnReferenceFromOwnerReference(obj.GetNamespace(), o)
		if err != nil {
			return err
		}
		objects, ok := tracker.objects[oRef.gvk]
		if !ok {
			return errors.NewBadRequest(fmt.Sprintf("ownerReference %s, Name=%s does not exist", oRef.gvk, oRef.key.Name))
		}
		if _, ok := objects[oRef.key]; !ok {
			return errors.NewBadRequest(fmt.Sprintf("ownerReference %s, Name=%s does not exist", oRef.gvk, oRef.key.Name))
		}
	}

	_, ok := tracker.objects[gvk]
	if !ok {
		tracker.objects[gvk] = make(map[types.NamespacedName]client.Object)
	}

	key := client.ObjectKeyFromObject(obj)
	objRef := ownReference{gvk: gvk, key: key}
	if trackedObj, ok := tracker.objects[gvk][key]; ok {
		if replaceExisting {
			if trackedObj.GetResourceVersion() != obj.GetResourceVersion() {
				return errors.NewConflict(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String(), fmt.Errorf("object has been modified"))
			}

			if err := c.beforeUpdate(resourceGroup, trackedObj, obj); err != nil {
				return err
			}
			tracker.objects[gvk][key] = obj
			updateTrackerOwnerReferences(tracker, trackedObj, obj, objRef)
			c.afterUpdate(resourceGroup, trackedObj, obj)
			return nil
		}
		return errors.NewAlreadyExists(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if replaceExisting {
		return errors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if err := c.beforeCreate(resourceGroup, obj); err != nil {
		return err
	}
	tracker.objects[gvk][key] = obj
	updateTrackerOwnerReferences(tracker, nil, obj, objRef)
	c.afterCreate(resourceGroup, obj)
	return nil
}

func updateTrackerOwnerReferences(tracker *resourceGroupTracker, trackedObj client.Object, obj client.Object, objRef ownReference) {
	if trackedObj != nil {
		for _, oldO := range trackedObj.GetOwnerReferences() {
			found := false
			for _, newO := range obj.GetOwnerReferences() {
				if oldO == newO {
					found = true
					break
				}
			}
			if !found {
				oldRef, _ := newOwnReferenceFromOwnerReference(obj.GetNamespace(), oldO)
				if _, ok := tracker.ownedObjects[*oldRef]; !ok {
					continue
				}
				delete(tracker.ownedObjects[*oldRef], objRef)
				if len(tracker.ownedObjects[*oldRef]) == 0 {
					delete(tracker.ownedObjects, *oldRef)
				}
			}
		}
	}
	for _, newO := range obj.GetOwnerReferences() {
		found := false
		if trackedObj != nil {
			for _, oldO := range trackedObj.GetOwnerReferences() {
				if newO == oldO {
					found = true
					break
				}
			}
		}
		if !found {
			newRef, _ := newOwnReferenceFromOwnerReference(obj.GetNamespace(), newO)
			if _, ok := tracker.ownedObjects[*newRef]; !ok {
				tracker.ownedObjects[*newRef] = map[ownReference]struct{}{}
			}
			tracker.ownedObjects[*newRef][objRef] = struct{}{}
		}
	}
}

func (c *cache) Delete(resourceGroup string, obj client.Object) error {
	if resourceGroup == "" {
		return errors.NewBadRequest("resourceGroup must not be empty")
	}

	if obj == nil {
		return errors.NewBadRequest("object must not be nil")
	}

	if obj.GetName() == "" {
		return errors.NewBadRequest("object name must not be empty")
	}

	gvk, err := c.gvkGetAndSet(obj)
	if err != nil {
		return err
	}

	obj = obj.DeepCopyObject().(client.Object)

	key := client.ObjectKeyFromObject(obj)
	deleted, err := c.tryDelete(resourceGroup, gvk, key)
	if err != nil {
		return err
	}
	if !deleted {
		c.garbageCollectorQueue.Add(gcRequest{
			resourceGroup: resourceGroup,
			gvk:           gvk,
			key:           key,
		})
	}
	return nil
}

func (c *cache) tryDelete(resourceGroup string, gvk schema.GroupVersionKind, key types.NamespacedName) (bool, error) {
	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return true, errors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	return c.doTryDelete(resourceGroup, tracker, gvk, key)
}

func (c *cache) doTryDelete(resourceGroup string, tracker *resourceGroupTracker, gvk schema.GroupVersionKind, key types.NamespacedName) (bool, error) {
	objects, ok := tracker.objects[gvk]
	if !ok {
		return true, errors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	obj, ok := tracker.objects[gvk][key]
	if !ok {
		return true, errors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if ownedReferences, ok := tracker.ownedObjects[ownReference{gvk: gvk, key: key}]; ok {
		for ref, _ := range ownedReferences {
			deleted, err := c.doTryDelete(resourceGroup, tracker, ref.gvk, ref.key)
			if err != nil {
				return false, err
			}
			if !deleted {
				c.garbageCollectorQueue.Add(gcRequest{
					resourceGroup: resourceGroup,
					gvk:           ref.gvk,
					key:           ref.key,
				})
			}
		}
		delete(tracker.ownedObjects, ownReference{gvk: gvk, key: key})
	}

	if len(obj.GetFinalizers()) > 0 {
		if !obj.GetDeletionTimestamp().IsZero() {
			return false, nil
		}
		if err := c.beforeDelete(resourceGroup, obj); err != nil {
			return false, errors.NewBadRequest(err.Error())
		}

		oldObj := obj.DeepCopyObject().(client.Object)
		now := metav1.Time{Time: time.Now().UTC()}
		obj.SetDeletionTimestamp(&now)
		if err := c.beforeUpdate(resourceGroup, oldObj, obj); err != nil {
			return false, errors.NewBadRequest(err.Error())
		}
		// Required to override default beforeUpdate behaviour
		// that prevent changes to automatically managed fields.
		obj.SetDeletionTimestamp(&now)

		objects[key] = obj
		c.afterUpdate(resourceGroup, oldObj, obj)

		return false, nil
	}

	delete(objects, key)
	c.afterDelete(resourceGroup, obj)
	return true, nil
}
