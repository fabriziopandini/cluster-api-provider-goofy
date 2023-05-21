/*
Copyright 2019 The Kubernetes Authors.

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

// Package controllers implements controller functionality.
package controllers

import (
	"context"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sync"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/predicates"

	infrav1 "github.com/fabriziopandini/cluster-api-provider-goofy/api/v1alpha1"
)

// GoofyClusterReconciler reconciles a GoofyCluster object.
type GoofyClusterReconciler struct {
	client.Client
	CloudMgr     cloud.Manager
	ApiServerMux *server.WorkloadClustersMux

	// WatchFilterValue is the label value used to filter events prior to reconciliation.
	WatchFilterValue string

	hostRestartDone bool
	hostRestartLock sync.RWMutex
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=goofyclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=goofyclusters/status;goofyclusters/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch

// Reconcile reads that state of the cluster for a GoofyCluster object and makes changes based on the state read
// and what is in the GoofyCluster.Spec.
func (r *GoofyClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the GoofyCluster instance
	goofyCluster := &infrav1.GoofyCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, goofyCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, goofyCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on GoofyCluster")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("Cluster", klog.KObj(cluster))
	ctx = ctrl.LoggerInto(ctx, log)

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(goofyCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If the controller is restarting and there is already an existing set of GoofyClusters,
	// tries to rebuild the internal state of the ApiServerMux accordingly.
	if ret, err := r.reconcileHotRestart(ctx); !ret.IsZero() || err != nil {
		return ret, err
	}

	// Always attempt to Patch the GoofyCluster object and status after each reconciliation.
	defer func() {
		if err := patchHelper.Patch(ctx, goofyCluster); err != nil {
			log.Error(err, "failed to patch GoofyCluster")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(goofyCluster, infrav1.ClusterFinalizer) {
		controllerutil.AddFinalizer(goofyCluster, infrav1.ClusterFinalizer)
		return ctrl.Result{}, nil
	}

	// Handle deleted clusters
	if !goofyCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, goofyCluster)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, cluster, goofyCluster)
}

// reconcileHotRestart tries to setup the ApiServerMux according to an existing sets of GoofyCluster.
// NOTE: This is done at best effort in order to make iterative development workflow easier.
func (r *GoofyClusterReconciler) reconcileHotRestart(ctx context.Context) (ctrl.Result, error) {
	if !r.isHotRestart() {
		return ctrl.Result{}, nil
	}

	r.hostRestartLock.Lock()
	defer r.hostRestartLock.Unlock()

	goofyClusterList := &infrav1.GoofyClusterList{}
	if err := r.Client.List(ctx, goofyClusterList); err != nil {
		return ctrl.Result{}, err
	}
	r.ApiServerMux.HotRestart(goofyClusterList)

	r.hostRestartDone = true
	return ctrl.Result{}, nil
}

func (r *GoofyClusterReconciler) isHotRestart() bool {
	r.hostRestartLock.RLock()
	defer r.hostRestartLock.RUnlock()
	return !r.hostRestartDone
}

func (r *GoofyClusterReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, goofyCluster *infrav1.GoofyCluster) (ctrl.Result, error) {
	// Compute the resource group unique name.
	resourceGroup := klog.KObj(cluster).String()

	// Stores the resource group used by this goofyCluster.
	goofyCluster.Annotations[infrav1.ResourceGroupAnnotationName] = resourceGroup

	// Create a resource group for all the cloud resources belonging the workload cluster;
	// if the resource group already exists, the operation is a no-op.
	// NOTE: We are storing in this resource group both the cloud resources (e.g. VM) as
	// well as Kubernetes resources that are expected to exist on the workload cluster (e.g Nodes).
	r.CloudMgr.AddResourceGroup(resourceGroup)

	// Initialize a listener for the workload cluster; if the listener has been already initialized
	// the operation is a no-op.
	// NOTE: We are using reconcilerGroup also as a name for the listener for sake of simplicity.
	// IMPORTANT: The fact that both the listener and the resourceGroup for a workload cluster have
	// the same name is used by the current implementation of the resourceGroup resolvers in the ApiServerMux.
	listener, err := r.ApiServerMux.InitWorkloadClusterListener(resourceGroup)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to init the listener for the workload cluster")
	}

	// Surface the control plane endpoint
	if goofyCluster.Spec.ControlPlaneEndpoint.Host == "" {
		goofyCluster.Spec.ControlPlaneEndpoint.Host = listener.Host()
		goofyCluster.Spec.ControlPlaneEndpoint.Port = listener.Port()
	}

	// Mark the dockerCluster ready
	goofyCluster.Status.Ready = true

	return ctrl.Result{}, nil
}

func (r *GoofyClusterReconciler) reconcileDelete(ctx context.Context, goofyCluster *infrav1.GoofyCluster) (ctrl.Result, error) {
	// TODO: implement
	controllerutil.RemoveFinalizer(goofyCluster, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}

// SetupWithManager will add watches for this controller.
func (r *GoofyClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.GoofyCluster{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), r.WatchFilterValue)).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(util.ClusterToInfrastructureMapFunc(ctx, infrav1.GroupVersion.WithKind("GoofyCluster"), mgr.GetClient(), &infrav1.GoofyCluster{})),
			builder.WithPredicates(
				predicates.ClusterUnpaused(ctrl.LoggerFrom(ctx)),
			),
		).Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}
	return nil
}
