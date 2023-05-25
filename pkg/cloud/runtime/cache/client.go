package cache

import (
	"fmt"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *cache) Get(resourceGroup string, key client.ObjectKey, obj client.Object) error {
	if resourceGroup == "" {
		return apierrors.NewBadRequest("resourceGroup must not be empty")
	}

	if key.Name == "" {
		return apierrors.NewBadRequest("key.Name must not be empty")
	}

	if obj == nil {
		return apierrors.NewBadRequest("object must not be nil")
	}

	gvk, err := c.gvkGetAndSet(obj)
	if err != nil {
		return err
	}

	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return apierrors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.RLock()
	defer tracker.lock.RUnlock()

	objects, ok := tracker.objects[gvk]
	if !ok {
		return apierrors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	trackedObj, ok := objects[key]
	if !ok {
		return apierrors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if err := c.scheme.Convert(trackedObj, obj, nil); err != nil {
		return apierrors.NewInternalError(err)
	}
	obj.GetObjectKind().SetGroupVersionKind(trackedObj.GetObjectKind().GroupVersionKind())

	return nil
}

func (c *cache) List(resourceGroup string, list client.ObjectList, opts ...client.ListOption) error {
	if resourceGroup == "" {
		return apierrors.NewBadRequest("resourceGroup must not be empty")
	}

	if list == nil {
		return apierrors.NewBadRequest("list must not be be nil")
	}

	gvk, err := c.gvkGetAndSet(list)
	if err != nil {
		return err
	}

	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return apierrors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.RLock()
	defer tracker.lock.RUnlock()

	items := make([]runtime.Object, 0)
	objects, ok := tracker.objects[unsafeGuessObjectKindFromList(gvk)]
	if ok {
		listOpts := client.ListOptions{}
		listOpts.ApplyOptions(opts)

		for _, obj := range objects {
			if listOpts.Namespace != "" && obj.GetNamespace() != listOpts.Namespace {
				continue
			}

			if listOpts.LabelSelector != nil && !listOpts.LabelSelector.Empty() {
				metaLabels := labels.Set(obj.GetLabels())
				if !listOpts.LabelSelector.Matches(metaLabels) {
					continue
				}
			}

			obj := obj.DeepCopyObject().(client.Object)
			switch list.(type) {
			case *unstructured.UnstructuredList:
				unstructuredObj := &unstructured.Unstructured{}
				if err := c.scheme.Convert(obj, unstructuredObj, nil); err != nil {
					return apierrors.NewInternalError(err)
				}
				items = append(items, unstructuredObj)
			default:
				items = append(items, obj)
			}
		}
	}

	if err := meta.SetList(list, items); err != nil {
		return apierrors.NewInternalError(err)
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
		return apierrors.NewBadRequest("resourceGroup must not be empty")
	}

	if obj == nil {
		return apierrors.NewBadRequest("object must not be nil")
	}

	gvk, err := c.gvkGetAndSet(obj)
	if err != nil {
		return err
	}

	if replaceExisting && obj.GetName() == "" {
		return apierrors.NewBadRequest("object name must not be empty")
	}

	tracker := c.resourceGroupTracker(resourceGroup)
	if tracker == nil {
		return apierrors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	for _, o := range obj.GetOwnerReferences() {
		oRef, err := newOwnReferenceFromOwnerReference(obj.GetNamespace(), o)
		if err != nil {
			return err
		}
		objects, ok := tracker.objects[oRef.gvk]
		if !ok {
			return apierrors.NewBadRequest(fmt.Sprintf("ownerReference %s, Name=%s does not exist", oRef.gvk, oRef.key.Name))
		}
		if _, ok := objects[oRef.key]; !ok {
			return apierrors.NewBadRequest(fmt.Sprintf("ownerReference %s, Name=%s does not exist", oRef.gvk, oRef.key.Name))
		}
	}

	_, ok := tracker.objects[gvk]
	if !ok {
		tracker.objects[gvk] = make(map[types.NamespacedName]client.Object)
	}

	// TODO: if unstructured, convert to typed object

	key := client.ObjectKeyFromObject(obj)
	objRef := ownReference{gvk: gvk, key: key}
	if trackedObj, ok := tracker.objects[gvk][key]; ok {
		if replaceExisting {
			if trackedObj.GetResourceVersion() != obj.GetResourceVersion() {
				return apierrors.NewConflict(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String(), fmt.Errorf("object has been modified"))
			}

			if err := c.beforeUpdate(resourceGroup, trackedObj, obj); err != nil {
				return err
			}
			tracker.objects[gvk][key] = obj.DeepCopyObject().(client.Object)
			updateTrackerOwnerReferences(tracker, trackedObj, obj, objRef)
			c.afterUpdate(resourceGroup, trackedObj, obj)
			return nil
		}
		return apierrors.NewAlreadyExists(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if replaceExisting {
		return apierrors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	if err := c.beforeCreate(resourceGroup, obj); err != nil {
		return err
	}
	tracker.objects[gvk][key] = obj.DeepCopyObject().(client.Object)
	updateTrackerOwnerReferences(tracker, nil, obj, objRef)
	c.afterCreate(resourceGroup, obj)
	return nil
}

func updateTrackerOwnerReferences(tracker *resourceGroupTracker, oldObj client.Object, newObj client.Object, objRef ownReference) {
	if oldObj != nil {
		for _, oldO := range oldObj.GetOwnerReferences() {
			found := false
			for _, newO := range newObj.GetOwnerReferences() {
				if oldO == newO {
					found = true
					break
				}
			}
			if !found {
				oldRef, _ := newOwnReferenceFromOwnerReference(newObj.GetNamespace(), oldO)
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
	for _, newO := range newObj.GetOwnerReferences() {
		found := false
		if oldObj != nil {
			for _, oldO := range oldObj.GetOwnerReferences() {
				if newO == oldO {
					found = true
					break
				}
			}
		}
		if !found {
			newRef, _ := newOwnReferenceFromOwnerReference(newObj.GetNamespace(), newO)
			if _, ok := tracker.ownedObjects[*newRef]; !ok {
				tracker.ownedObjects[*newRef] = map[ownReference]struct{}{}
			}
			tracker.ownedObjects[*newRef][objRef] = struct{}{}
		}
	}
}

func (c *cache) Patch(resourceGroup string, obj client.Object, patch client.Patch) error {
	obj = obj.DeepCopyObject().(client.Object)
	if err := c.Get(resourceGroup, client.ObjectKeyFromObject(obj), obj); err != nil {
		return err
	}

	patchData, err := patch.Data(obj)

	encoder, err := c.getEncoder(obj, obj.GetObjectKind().GroupVersionKind().GroupVersion())
	if err != nil {
		return apierrors.NewInternalError(err)
	}

	originalObjJS, err := runtime.Encode(encoder, obj)
	if err != nil {
		return apierrors.NewInternalError(err)
	}

	var changedJS []byte
	switch patch.Type() {
	case types.MergePatchType:
		changedJS, err = jsonpatch.MergePatch(originalObjJS, patchData)
		if err != nil {
			return apierrors.NewInternalError(err)
		}
	case types.StrategicMergePatchType:
		// NOTE: we are treating StrategicMergePatch as MergePatch; it is an acceptable proxy for this use case.
		changedJS, err = jsonpatch.MergePatch(originalObjJS, patchData)
		if err != nil {
			return apierrors.NewInternalError(err)
		}
	default:
		return apierrors.NewBadRequest(fmt.Sprintf("path of type %s are not supported", patch.Type()))
	}

	codecFactory := serializer.NewCodecFactory(c.scheme)
	err = runtime.DecodeInto(codecFactory.UniversalDecoder(), changedJS, obj)
	if err != nil {
		return apierrors.NewInternalError(err)
	}

	return c.store(resourceGroup, obj, true)
}

func (h *cache) getEncoder(obj runtime.Object, gv runtime.GroupVersioner) (runtime.Encoder, error) {
	codecs := serializer.NewCodecFactory(h.scheme)

	info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), runtime.ContentTypeJSON)
	if !ok {
		return nil, fmt.Errorf("failed to create serializer for %T", obj)
	}

	encoder := codecs.EncoderForVersion(info.Serializer, gv)
	return encoder, nil
}

func (c *cache) Delete(resourceGroup string, obj client.Object) error {
	if resourceGroup == "" {
		return apierrors.NewBadRequest("resourceGroup must not be empty")
	}

	if obj == nil {
		return apierrors.NewBadRequest("object must not be nil")
	}

	if obj.GetName() == "" {
		return apierrors.NewBadRequest("object name must not be empty")
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
		return true, apierrors.NewBadRequest(fmt.Sprintf("resourceGroup %s does not exist", resourceGroup))
	}

	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	return c.doTryDelete(resourceGroup, tracker, gvk, key)
}

func (c *cache) doTryDelete(resourceGroup string, tracker *resourceGroupTracker, gvk schema.GroupVersionKind, key types.NamespacedName) (bool, error) {
	objects, ok := tracker.objects[gvk]
	if !ok {
		return true, apierrors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
	}

	obj, ok := tracker.objects[gvk][key]
	if !ok {
		return true, apierrors.NewNotFound(unsafeGuessGroupVersionResource(gvk).GroupResource(), key.String())
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
			return false, apierrors.NewBadRequest(err.Error())
		}

		oldObj := obj.DeepCopyObject().(client.Object)
		now := metav1.Time{Time: time.Now().UTC()}
		obj.SetDeletionTimestamp(&now)
		if err := c.beforeUpdate(resourceGroup, oldObj, obj); err != nil {
			return false, apierrors.NewBadRequest(err.Error())
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
