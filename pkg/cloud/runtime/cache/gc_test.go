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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
)

func Test_cache_gc(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	c := NewCache(scheme).(*cache)
	c.garbageCollectorRequeueAfter = 500 * time.Millisecond // force a shorter gc requeueAfter
	err := c.Start(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return c.started
	}, 5*time.Second, 200*time.Millisecond, "manager should start")

	c.AddResourceGroup("foo")

	obj := &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "baz",
			Finalizers: []string{"foo"},
		},
	}
	err = c.Create("foo", obj)
	require.NoError(t, err)

	err = c.Delete("foo", obj)
	require.NoError(t, err)

	require.Never(t, func() bool {
		if err := c.Get("foo", types.NamespacedName{Name: "baz"}, obj); apierrors.IsNotFound(err) {
			return true
		}
		return false
	}, 5*time.Second, 200*time.Millisecond, "object with finalizer should never be deleted")

	obj.Finalizers = nil
	err = c.Update("foo", obj)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		if err := c.Get("foo", types.NamespacedName{Name: "baz"}, obj); apierrors.IsNotFound(err) {
			return true
		}
		return false
	}, 5*time.Second, 200*time.Millisecond, "object should be garbage collected")

	c.lock.RLock()
	defer c.lock.RUnlock()

	require.Contains(t, c.resourceGroups["foo"].objects, cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind), "gvk must exists in object tracker for foo")
	require.NotContains(t, c.resourceGroups["foo"].objects[cloudv1.GroupVersion.WithKind(cloudv1.CloudMachineKind)], "baz", "object baz must not exist in object tracker for foo")

	cancel()
}
