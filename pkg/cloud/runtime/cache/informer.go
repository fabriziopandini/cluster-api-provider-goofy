package cache

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type informer struct {
	handlers []InformEventHandler
}

func (i *informer) AddEventHandler(handler InformEventHandler) error {
	i.handlers = append(i.handlers, handler)
	return nil
}

func (c *cache) GetInformer(ctx context.Context, obj client.Object) (Informer, error) {
	gvk, err := apiutil.GVKForObject(obj, c.scheme)
	if err != nil {
		return nil, err
	}
	return c.GetInformerForKind(ctx, gvk)
}

func (c *cache) GetInformerForKind(_ context.Context, gvk schema.GroupVersionKind) (Informer, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, ok := c.informers[gvk]; !ok {
		c.informers[gvk] = &informer{}
	}
	return c.informers[gvk], nil
}

func (c *cache) informCreate(resourceGroup string, obj client.Object) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if i, ok := c.informers[obj.GetObjectKind().GroupVersionKind()]; ok {
		for _, h := range i.(*informer).handlers {
			h.OnCreate(resourceGroup, obj)
		}
	}
}

func (c *cache) informUpdate(resourceGroup string, oldRes, newRes client.Object) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if i, ok := c.informers[newRes.GetObjectKind().GroupVersionKind()]; ok {
		for _, h := range i.(*informer).handlers {
			h.OnUpdate(resourceGroup, oldRes, newRes)
		}
	}
}

func (c *cache) informDelete(resourceGroup string, obj client.Object) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if i, ok := c.informers[obj.GetObjectKind().GroupVersionKind()]; ok {
		for _, h := range i.(*informer).handlers {
			h.OnDelete(resourceGroup, obj)
		}
	}
}

func (c *cache) informSync(resourceGroup string, obj client.Object) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if i, ok := c.informers[obj.GetObjectKind().GroupVersionKind()]; ok {
		for _, h := range i.(*informer).handlers {
			h.OnGeneric(resourceGroup, obj)
		}
	}
}
