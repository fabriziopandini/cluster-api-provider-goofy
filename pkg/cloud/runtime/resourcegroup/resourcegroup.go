package resourcegroup

import (
	cclient "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/client"
)

type ResourceGroup interface {
	GetClient() cclient.Client
}
