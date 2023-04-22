package resourcegroup

import (
	"context"
	ccache "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/cache"
	cclient "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ ResourceGroup = &cachedResourceGroup{}

type cachedResourceGroup struct {
	name  string
	cache ccache.Cache
}

func NewResourceGroup(name string, cache ccache.Cache) ResourceGroup {
	return &cachedResourceGroup{
		name:  name,
		cache: cache,
	}
}

func (cc *cachedResourceGroup) GetClient() cclient.Client {
	return &cachedClient{
		resourceGroup: cc.name,
		cache:         cc.cache,
	}
}

var _ cclient.Client = &cachedClient{}

type cachedClient struct {
	resourceGroup string
	cache         ccache.Cache
}

func (c *cachedClient) Get(_ context.Context, key client.ObjectKey, obj client.Object) error {
	return c.cache.Get(c.resourceGroup, key, obj)
}

func (c *cachedClient) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return c.cache.List(c.resourceGroup, list, opts...)
}

func (c *cachedClient) Create(_ context.Context, obj client.Object) error {
	return c.cache.Create(c.resourceGroup, obj)
}

func (c *cachedClient) Delete(_ context.Context, obj client.Object) error {
	return c.cache.Delete(c.resourceGroup, obj)
}

func (c *cachedClient) Update(_ context.Context, obj client.Object) error {
	return c.cache.Update(c.resourceGroup, obj)
}
