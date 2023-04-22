package cloud

import (
	cbuilder "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/builder"
	cclient "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/client"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Client cclient.Client
type Object client.Object

type Manager cmanager.Manager

var (
	NewManager             = cmanager.New
	NewControllerManagedBy = cbuilder.ControllerManagedBy
)
