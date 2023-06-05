/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
)

func Test_cache_client(t *testing.T) {
	t.Run("create objects", func(t *testing.T) {
		c := NewCache(scheme).(*cache)
		h := &fakeHandler{}
		iMachine, err := c.GetInformer(context.TODO(), &cloudv1.CloudMachine{})
		require.NoError(t, err)
		err = iMachine.AddEventHandler(h)
		require.NoError(t, err)

		c.AddResourceGroup("foo")

		t.Run("fails if resourceGroup is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			}
			err := c.Create("", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if obj is nil", func(t *testing.T) {
			err := c.Create("foo", nil)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if unknown kind", func(t *testing.T) {
			// TODO implement test case
		})

		t.Run("fails if resourceGroup does not exist", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			}
			err := c.Create("bar", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("create", func(t *testing.T) {
			obj := createMachine(t, c, "foo", "bar")

			// Check all the computed fields have been updated on the object.
			require.False(t, obj.CreationTimestamp.IsZero())
			require.NotEmpty(t, obj.ResourceVersion)
			require.Contains(t, obj.Annotations, lastSyncTimeAnnotation)

			// Check internal state of the tracker is as expected.
			c.lock.RLock()
			defer c.lock.RUnlock()

			require.Contains(t, c.resourceGroups["foo"].objects, cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must exists in object tracker for foo")
			key := types.NamespacedName{Name: "bar"}
			require.Contains(t, c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)], key, "Object bar must exists in object tracker for foo")

			r := c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)][key]
			require.Equal(t, r.GetObjectKind().GroupVersionKind(), cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must be set")
			require.Equal(t, r.GetName(), "bar", "name must be equal to object tracker key")
			require.Equal(t, r.GetResourceVersion(), "v1", "resourceVersion must be set")
			require.NotZero(t, r.GetCreationTimestamp(), "creation timestamp must be set")
			require.Contains(t, r.GetAnnotations(), lastSyncTimeAnnotation, "last sync annotation must exists")

			require.Contains(t, h.Events(), "foo, CloudMachine=bar, Created")
		})

		t.Run("fails if Object already exists", func(t *testing.T) {
			createMachine(t, c, "foo", "bazzz")

			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bazzz",
				},
			}
			err := c.Create("foo", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsAlreadyExists(err))
		})

		t.Run("Create with owner references", func(t *testing.T) {
			t.Run("fails for invalid owner reference", func(t *testing.T) {
				obj := &cloudv1.CloudMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "child",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "something/not/valid",
								Kind:       "ParentKind",
								Name:       "parent",
							},
						},
					},
				}
				err := c.Create("foo", obj)
				require.Error(t, err)
				require.True(t, apierrors.IsBadRequest(err))
			})
			t.Run("fails if referenced object does not exist", func(t *testing.T) {
				obj := &cloudv1.CloudMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "child",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: cloudv1.GroupVersion.String(),
								Kind:       "CloudMachine",
								Name:       "parentx",
							},
						},
					},
				}
				err := c.Create("foo", obj)
				require.Error(t, err)
				require.True(t, apierrors.IsBadRequest(err))
			})
			t.Run("create updates ownedObjects", func(t *testing.T) {
				createMachine(t, c, "foo", "parent")
				obj := &cloudv1.CloudMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "child",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: cloudv1.GroupVersion.String(),
								Kind:       "CloudMachine",
								Name:       "parent",
							},
						},
					},
				}
				err := c.Create("foo", obj)
				require.NoError(t, err)

				// Check internal state of the tracker is as expected.
				c.lock.RLock()
				defer c.lock.RUnlock()

				parentRef := ownReference{gvk: cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), key: types.NamespacedName{Namespace: "", Name: "parent"}}
				require.Contains(t, c.resourceGroups["foo"].ownedObjects, parentRef, "there should be ownedObjects for parent")
				childRef := ownReference{gvk: cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), key: types.NamespacedName{Namespace: "", Name: "child"}}
				require.Contains(t, c.resourceGroups["foo"].ownedObjects[parentRef], childRef, "parent should own child")
			})
		})
	})

	t.Run("Get objects", func(t *testing.T) {
		c := NewCache(scheme).(*cache)
		c.AddResourceGroup("foo")

		t.Run("fails if resourceGroup is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{}
			err := c.Get("", types.NamespacedName{Name: "foo"}, obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if name is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{}
			err := c.Get("foo", types.NamespacedName{}, obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if Object is nil", func(t *testing.T) {
			err := c.Get("foo", types.NamespacedName{Name: "foo"}, nil)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if unknown kind", func(t *testing.T) {
			// TODO implement test case
		})

		t.Run("fails if resourceGroup doesn't exist", func(t *testing.T) {
			obj := &cloudv1.CloudLoadBalancer{}
			err := c.Get("bar", types.NamespacedName{Name: "bar"}, obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if gvk doesn't exist", func(t *testing.T) {
			obj := &cloudv1.CloudLoadBalancer{}
			err := c.Get("foo", types.NamespacedName{Name: "bar"}, obj)
			require.Error(t, err)
			require.True(t, apierrors.IsNotFound(err))
		})

		t.Run("fails if Object doesn't exist", func(t *testing.T) {
			createMachine(t, c, "foo", "barz")

			obj := &cloudv1.CloudMachine{}
			err := c.Get("foo", types.NamespacedName{Name: "bar"}, obj)
			require.Error(t, err)
			require.True(t, apierrors.IsNotFound(err))
		})

		t.Run("get", func(t *testing.T) {
			createMachine(t, c, "foo", "bar")

			obj := &cloudv1.CloudMachine{}
			err := c.Get("foo", types.NamespacedName{Name: "bar"}, obj)
			require.NoError(t, err)

			// Check all the computed fields are as expected.
			require.Equal(t, obj.GetObjectKind().GroupVersionKind(), cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must be set")
			require.Equal(t, obj.GetName(), "bar", "name must be equal to object tracker key")
			require.Equal(t, obj.GetResourceVersion(), "v1", "resourceVersion must be set")
			require.NotZero(t, obj.GetCreationTimestamp(), "creation timestamp must be set")
			require.Contains(t, obj.GetAnnotations(), lastSyncTimeAnnotation, "last sync annotation must be set")
		})
	})

	t.Run("list objects", func(t *testing.T) {
		c := NewCache(scheme).(*cache)
		c.AddResourceGroup("foo")

		t.Run("fails if resourceGroup is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachineList{}
			err := c.List("", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if Object is nil", func(t *testing.T) {
			err := c.List("foo", nil)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if unknown kind", func(t *testing.T) {
			// TODO implement test case
		})

		t.Run("fails if resourceGroup doesn't exist", func(t *testing.T) {
			obj := &cloudv1.CloudMachineList{}
			err := c.List("bar", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("list", func(t *testing.T) {
			createMachine(t, c, "foo", "bar")
			createMachine(t, c, "foo", "baz")

			obj := &cloudv1.CloudMachineList{}
			err := c.List("foo", obj)
			require.NoError(t, err)

			require.Len(t, obj.Items, 2)

			i1 := obj.Items[0]
			require.Equal(t, i1.GetObjectKind().GroupVersionKind(), cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must be set")
			require.Contains(t, i1.GetAnnotations(), lastSyncTimeAnnotation, "last sync annotation must be present")

			i2 := obj.Items[1]
			require.Equal(t, i2.GetObjectKind().GroupVersionKind(), cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must be set")
			require.Contains(t, i2.GetAnnotations(), lastSyncTimeAnnotation, "last sync annotation must be present")
		})

		// TODO: test filtering by labels
	})

	t.Run("update objects", func(t *testing.T) {
		c := NewCache(scheme).(*cache)
		h := &fakeHandler{}
		i, err := c.GetInformer(context.TODO(), &cloudv1.CloudMachine{})
		require.NoError(t, err)
		err = i.AddEventHandler(h)
		require.NoError(t, err)

		c.AddResourceGroup("foo")

		t.Run("fails if resourceGroup is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{}
			err := c.Update("", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if Object is nil", func(t *testing.T) {
			err := c.Update("foo", nil)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if unknown kind", func(t *testing.T) {
			// TODO implement test case
		})

		t.Run("fails if name is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{}
			err := c.Update("foo", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if resourceGroup doesn't exist", func(t *testing.T) {
			obj := &cloudv1.CloudLoadBalancer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
			}
			err := c.Update("bar", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if Object doesn't exist", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
			}
			err := c.Update("foo", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsNotFound(err))
		})

		t.Run("update - no changes", func(t *testing.T) {
			objBefore := createMachine(t, c, "foo", "bar")

			objUpdate := objBefore.DeepCopy()
			err = c.Update("foo", objUpdate)
			require.NoError(t, err)

			require.Equal(t, objBefore, objUpdate, "obj before and after must be the same")

			require.NotContains(t, h.Events(), "foo, CloudMachine=bar, Updated")
		})

		t.Run("update - with changes", func(t *testing.T) {
			objBefore := createMachine(t, c, "foo", "baz")

			time.Sleep(1 * time.Second)

			objUpdate := objBefore.DeepCopy()
			objUpdate.Labels = map[string]string{"foo": "bar"}
			err = c.Update("foo", objUpdate)
			require.NoError(t, err)

			// Check all the computed fields are as expected.
			require.NotEqual(t, objBefore.GetAnnotations()[lastSyncTimeAnnotation], objUpdate.GetAnnotations()[lastSyncTimeAnnotation], "last sync version must be changed")
			objBefore.Annotations = objUpdate.Annotations
			require.NotEqual(t, objBefore.GetResourceVersion(), objUpdate.GetResourceVersion(), "Object version must be changed")
			objBefore.SetResourceVersion(objUpdate.GetResourceVersion())
			objBefore.Labels = objUpdate.Labels
			require.Equal(t, objBefore, objUpdate, "everything else must be the same")

			require.Contains(t, h.Events(), "foo, CloudMachine=baz, Updated")
		})

		t.Run("update - with conflict", func(t *testing.T) {
			objBefore := createMachine(t, c, "foo", "bazz")

			objUpdate1 := objBefore.DeepCopy()
			objUpdate1.Labels = map[string]string{"foo": "bar"}

			time.Sleep(1 * time.Second)

			err = c.Update("foo", objUpdate1)
			require.NoError(t, err)

			objUpdate2 := objBefore.DeepCopy()
			err = c.Update("foo", objUpdate2)
			require.Error(t, err)
			require.True(t, apierrors.IsConflict(err))

			// TODO: check if it has been informed only once
		})

		t.Run("Update with owner references", func(t *testing.T) {
			t.Run("fails for invalid owner reference", func(t *testing.T) {
				obj := &cloudv1.CloudMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "child",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "something/not/valid",
								Kind:       "ParentKind",
								Name:       "parent",
							},
						},
					},
				}
				err := c.Update("foo", obj)
				require.Error(t, err)
				require.True(t, apierrors.IsBadRequest(err))
			})
			t.Run("fails if referenced object does not exists", func(t *testing.T) {
				objBefore := createMachine(t, c, "foo", "child1")

				objUpdate := objBefore.DeepCopy()
				objUpdate.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion: cloudv1.GroupVersion.String(),
						Kind:       "CloudMachine",
						Name:       "parentx",
					},
				}
				err := c.Update("foo", objUpdate)
				require.Error(t, err)
				require.True(t, apierrors.IsBadRequest(err))
			})
			t.Run("updates takes care of ownedObjects", func(t *testing.T) {
				createMachine(t, c, "foo", "parent1")
				createMachine(t, c, "foo", "parent2")

				objBefore := &cloudv1.CloudMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "child2",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: cloudv1.GroupVersion.String(),
								Kind:       "CloudMachine",
								Name:       "parent1",
							},
						},
					},
				}
				err := c.Create("foo", objBefore)
				require.NoError(t, err)

				objUpdate := objBefore.DeepCopy()
				objUpdate.OwnerReferences[0].Name = "parent2"

				err = c.Update("foo", objUpdate)
				require.NoError(t, err)

				// Check internal state of the tracker
				c.lock.RLock()
				defer c.lock.RUnlock()

				parent1Ref := ownReference{gvk: cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), key: types.NamespacedName{Namespace: "", Name: "parent1"}}
				require.NotContains(t, c.resourceGroups["foo"].ownedObjects, parent1Ref, "there should not be ownedObjects for parent1")
				parent2Ref := ownReference{gvk: cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), key: types.NamespacedName{Namespace: "", Name: "parent2"}}
				require.Contains(t, c.resourceGroups["foo"].ownedObjects, parent2Ref, "there should be ownedObjects for parent2")
				childRef := ownReference{gvk: cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), key: types.NamespacedName{Namespace: "", Name: "child2"}}
				require.Contains(t, c.resourceGroups["foo"].ownedObjects[parent2Ref], childRef, "parent2 should own child")
			})
		})

		// TODO: test system managed fields cannot be updated (see before update)

		// TODO: update list
	})

	// TODO: test patch

	t.Run("delete objects", func(t *testing.T) {
		c := NewCache(scheme).(*cache)
		h := &fakeHandler{}
		i, err := c.GetInformer(context.TODO(), &cloudv1.CloudMachine{})
		require.NoError(t, err)
		err = i.AddEventHandler(h)
		require.NoError(t, err)

		c.AddResourceGroup("foo")

		t.Run("fails if resourceGroup is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{}
			err := c.Delete("", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if Object is nil", func(t *testing.T) {
			err := c.Delete("foo", nil)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if name is empty", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{}
			err := c.Delete("foo", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsBadRequest(err))
		})

		t.Run("fails if unknown kind", func(t *testing.T) {
			// TODO implement test case
		})

		t.Run("fails if gvk doesn't exist", func(t *testing.T) {
			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
			}
			err := c.Delete("foo", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsNotFound(err))
		})

		t.Run("fails if object doesn't exist", func(t *testing.T) {
			createMachine(t, c, "foo", "barz")

			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
			}
			err := c.Delete("foo", obj)
			require.Error(t, err)
			require.True(t, apierrors.IsNotFound(err))
		})

		t.Run("delete", func(t *testing.T) {
			obj := createMachine(t, c, "foo", "bar")

			err := c.Delete("foo", obj)
			require.NoError(t, err)

			c.lock.RLock()
			defer c.lock.RUnlock()

			require.Contains(t, c.resourceGroups["foo"].objects, cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must exist in object tracker for foo")
			require.NotContains(t, c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)], types.NamespacedName{Name: "bar"}, "Object bar must not exist in object tracker for foo")

			require.NotContains(t, h.Events(), "foo, CloudMachine=bar, Deleted")
		})

		t.Run("delete with finalizers", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			c.garbageCollectorQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
			go func() {
				<-ctx.Done()
				c.garbageCollectorQueue.ShutDown()
			}()

			objBefore := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "baz",
					Finalizers: []string{"foo"},
				},
			}
			err := c.Create("foo", objBefore)
			require.NoError(t, err)

			time.Sleep(1 * time.Second)

			err = c.Delete("foo", objBefore)
			require.NoError(t, err)

			objAfterUpdate := &cloudv1.CloudMachine{}
			err = c.Get("foo", types.NamespacedName{Name: "baz"}, objAfterUpdate)
			require.NoError(t, err)

			require.NotEqual(t, objBefore.GetDeletionTimestamp(), objAfterUpdate.GetDeletionTimestamp(), "deletion timestamp must be changed")
			objBefore.DeletionTimestamp = objAfterUpdate.DeletionTimestamp
			require.NotEqual(t, objBefore.GetAnnotations()[lastSyncTimeAnnotation], objAfterUpdate.GetAnnotations()[lastSyncTimeAnnotation], "last sync version must be changed")
			objBefore.Annotations = objAfterUpdate.Annotations
			require.NotEqual(t, objBefore.GetResourceVersion(), objAfterUpdate.GetResourceVersion(), "Object version must be changed")
			objBefore.SetResourceVersion(objAfterUpdate.GetResourceVersion())
			objBefore.Labels = objAfterUpdate.Labels
			require.Equal(t, objBefore, objAfterUpdate, "everything else must be the same")

			require.Contains(t, h.Events(), "foo, CloudMachine=baz, Deleted")

			cancel()
		})

		t.Run("delete with owner reference", func(t *testing.T) {
			createMachine(t, c, "foo", "parent3")

			obj := &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "child3",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: cloudv1.GroupVersion.String(),
							Kind:       "CloudMachine",
							Name:       "parent3",
						},
					},
				},
			}
			err := c.Create("foo", obj)
			require.NoError(t, err)

			obj = &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "grandchild3",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: cloudv1.GroupVersion.String(),
							Kind:       "CloudMachine",
							Name:       "child3",
						},
					},
				},
			}
			err = c.Create("foo", obj)
			require.NoError(t, err)

			obj = &cloudv1.CloudMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "parent3",
				},
			}
			err = c.Delete("foo", obj)
			require.NoError(t, err)

			c.lock.RLock()
			defer c.lock.RUnlock()

			require.Contains(t, c.resourceGroups["foo"].objects, cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must exist in object tracker for foo")
			require.NotContains(t, c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)], types.NamespacedName{Name: "parent3"}, "Object parent3 must not exist in object tracker for foo")
			require.NotContains(t, c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)], types.NamespacedName{Name: "child3"}, "Object child3 must not exist in object tracker for foo")
			require.NotContains(t, c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)], types.NamespacedName{Name: "grandchild3"}, "Object grandchild3 must not exist in object tracker for foo")
		})

		// TODO: test finalizers and ownner references together
	})
}

func createMachine(t *testing.T, c *cache, resourceGroup, name string) *cloudv1.CloudMachine {
	obj := &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := c.Create(resourceGroup, obj)
	require.NoError(t, err)

	return obj
}

var _ Informer = &fakeInformer{}

type fakeInformer struct {
	handler InformEventHandler
}

func (i *fakeInformer) AddEventHandler(handler InformEventHandler) error {
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

var _ InformEventHandler = &fakeHandler{}

type fakeHandler struct {
	events []string
}

func (h *fakeHandler) Events() []string {
	return h.events
}

func (h *fakeHandler) OnCreate(resourceGroup string, obj client.Object) {
	h.events = append(h.events, fmt.Sprintf("%s, %s=%s, Created", resourceGroup, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName()))
}

func (h *fakeHandler) OnUpdate(resourceGroup string, _, newObj client.Object) {
	h.events = append(h.events, fmt.Sprintf("%s, %s=%s, Updated", resourceGroup, newObj.GetObjectKind().GroupVersionKind().Kind, newObj.GetName()))
}

func (h *fakeHandler) OnDelete(resourceGroup string, obj client.Object) {
	h.events = append(h.events, fmt.Sprintf("%s, %s=%s, Deleted", resourceGroup, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName()))
}

func (h *fakeHandler) OnGeneric(resourceGroup string, obj client.Object) {
	h.events = append(h.events, fmt.Sprintf("%s, %s=%s, Synced", resourceGroup, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName()))
}
