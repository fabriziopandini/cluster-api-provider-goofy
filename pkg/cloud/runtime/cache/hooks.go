package cache

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"time"
)

func (c *cache) beforeCreate(_ string, obj client.Object) error {
	now := time.Now().UTC()
	obj.SetCreationTimestamp(metav1.Time{Time: now})
	// TODO: UID
	obj.SetAnnotations(appendAnnotations(obj, lastSyncTimeAnnotation, now.Format(time.RFC3339)))
	obj.SetResourceVersion(fmt.Sprintf("v%d", 1))
	return nil
}

func (c *cache) afterCreate(resourceGroup string, obj client.Object) {
	c.informCreate(resourceGroup, obj)
}

func (c *cache) beforeUpdate(_ string, old, new client.Object) error {
	new.SetCreationTimestamp(old.GetCreationTimestamp())
	new.SetResourceVersion(old.GetResourceVersion())
	// TODO: UID
	new.SetAnnotations(appendAnnotations(new, lastSyncTimeAnnotation, old.GetAnnotations()[lastSyncTimeAnnotation]))
	if !old.GetDeletionTimestamp().IsZero() {
		new.SetDeletionTimestamp(old.GetDeletionTimestamp())
	}
	if !reflect.DeepEqual(new, old) {
		now := time.Now().UTC()
		new.SetAnnotations(appendAnnotations(new, lastSyncTimeAnnotation, now.Format(time.RFC3339)))

		oldResourceVersion, _ := strconv.Atoi(strings.TrimPrefix(old.GetResourceVersion(), "v"))
		new.SetResourceVersion(fmt.Sprintf("v%d", oldResourceVersion+1))
	}
	return nil
}

func (c *cache) afterUpdate(resourceGroup string, old, new client.Object) {
	if old.GetDeletionTimestamp().IsZero() && !new.GetDeletionTimestamp().IsZero() {
		c.informDelete(resourceGroup, new)
		return
	}
	if !reflect.DeepEqual(new, old) {
		c.informUpdate(resourceGroup, old, new)
	}
}

func (c *cache) beforeDelete(_ string, _ client.Object) error {
	return nil
}

func (c *cache) afterDelete(_ string, _ client.Object) {
}

func appendAnnotations(obj client.Object, kayValuePair ...string) map[string]string {
	newAnnotations := map[string]string{}
	for k, v := range obj.GetAnnotations() {
		newAnnotations[k] = v
	}
	for i := 0; i < len(kayValuePair)-1; i = i + 2 {
		k := kayValuePair[i]
		v := kayValuePair[i+1]
		newAnnotations[k] = v
	}
	return newAnnotations
}
