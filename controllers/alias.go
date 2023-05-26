/*
Copyright 2021 The Kubernetes Authors.

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

// Package controllers provides access to reconcilers implemented in internal/controllers.
package controllers

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	goofycontrollers "github.com/fabriziopandini/cluster-api-provider-goofy/internal/controllers"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server"
)

// Following types provides access to reconcilers implemented in internal/controllers, thus
// allowing users to provide a single binary "batteries included" with Cluster API and providers of choice.

// GoofyClusterReconciler reconciles a GoofyCluster object.
type GoofyClusterReconciler struct {
	Client       client.Client
	CloudMgr     cloud.Manager
	APIServerMux *server.WorkloadClustersMux // TODO: find a way to use an interface here

	// WatchFilterValue is the label value used to filter events prior to reconciliation.
	WatchFilterValue string
}

// SetupWithManager sets up the reconciler with the Manager.
func (r *GoofyClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	return (&goofycontrollers.GoofyClusterReconciler{
		Client:           r.Client,
		CloudMgr:         r.CloudMgr,
		APIServerMux:     r.APIServerMux,
		WatchFilterValue: r.WatchFilterValue,
	}).SetupWithManager(ctx, mgr, options)
}

// GoofyMachineReconciler reconciles a GoofyMachine object.
type GoofyMachineReconciler struct {
	Client       client.Client
	CloudMgr     cloud.Manager
	APIServerMux *server.WorkloadClustersMux // TODO: find a way to use an interface here

	// WatchFilterValue is the label value used to filter events prior to reconciliation.
	WatchFilterValue string
}

// SetupWithManager sets up the reconciler with the Manager.
func (r *GoofyMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	return (&goofycontrollers.GoofyMachineReconciler{
		Client:           r.Client,
		CloudMgr:         r.CloudMgr,
		APIServerMux:     r.APIServerMux,
		WatchFilterValue: r.WatchFilterValue,
	}).SetupWithManager(ctx, mgr, options)
}
