package manager

import (
	"context"
	"fmt"
	ccache "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/cache"
	ccontroller "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/controller"
	cresourcegroup "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/resourcegroup"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Manager interface {
	// TODO: refactor in resoucegroup.add/delete/get; make delete fail if rs does not exist
	AddResourceGroup(name string)
	DeleteResourceGroup(name string)
	GetResourceGroup(name string) cresourcegroup.ResourceGroup

	GetScheme() *runtime.Scheme

	// TODO: expose less (only get informers)
	GetCache() ccache.Cache

	AddController(ccontroller.Controller) error

	Start(ctx context.Context) error
}

var _ Manager = &manager{}

type manager struct {
	scheme *runtime.Scheme

	cache ccache.Cache

	controllers []ccontroller.Controller
	started     bool
}

func New(scheme *runtime.Scheme) Manager {
	m := &manager{
		scheme:      scheme,
		controllers: make([]ccontroller.Controller, 0),
	}
	m.cache = ccache.NewCache(scheme)
	return m
}

func (m *manager) AddResourceGroup(name string) {
	m.cache.AddResourceGroup(name)
}

func (m *manager) DeleteResourceGroup(name string) {
	m.cache.DeleteResourceGroup(name)
}

func (m *manager) GetResourceGroup(name string) cresourcegroup.ResourceGroup {
	return cresourcegroup.NewResourceGroup(name, m.cache)
}

func (m *manager) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *manager) GetCache() ccache.Cache {
	return m.cache
}

func (m *manager) AddController(controller ccontroller.Controller) error {
	if m.started {
		return fmt.Errorf("cannot add controller to a manager already started")
	}

	m.controllers = append(m.controllers, controller)
	return nil
}

func (m *manager) Start(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)

	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	if m.started {
		return fmt.Errorf("manager started more than once")
	}

	if err := m.cache.Start(ctx); err != nil {
		return fmt.Errorf("failed to start cache: %v", err)
	}

	log.Info("Staring manager")
	for _, c := range m.controllers {
		if err := c.Start(ctx); err != nil {
			return fmt.Errorf("failed to start controllers: %v", err)
		}
	}

	m.started = true
	log.Info("Manager successfully started!")
	return nil
}
