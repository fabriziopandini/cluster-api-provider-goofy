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

package manager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	ccache "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/cache"
	ccontroller "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/controller"
	cresourcegroup "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/resourcegroup"
)

// Manager initializes shared dependencies such as Caches and Clients, and provides them to Runnables.
// A Manager is required to create Controllers.
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

// GetResourceGroup returns a resource group which reads from the cache.
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

	log.Info("Starting manager")
	for _, c := range m.controllers {
		if err := c.Start(ctx); err != nil {
			return fmt.Errorf("failed to start controllers: %v", err)
		}
	}

	m.started = true
	log.Info("Manager successfully started!")
	return nil
}
