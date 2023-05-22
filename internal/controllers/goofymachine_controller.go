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
	"crypto/rsa"
	"fmt"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud"
	cloudv1 "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/api/v1alpha1"
	"github.com/fabriziopandini/cluster-api-provider-goofy/pkg/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/conditions"
	clog "sigs.k8s.io/cluster-api/util/log"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

// GoofyMachineReconciler reconciles a GoofyMachine object.
type GoofyMachineReconciler struct {
	client.Client
	CloudMgr     cloud.Manager
	ApiServerMux *server.WorkloadClustersMux

	// WatchFilterValue is the label value used to filter events prior to reconciliation.
	WatchFilterValue string
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=goofymachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=goofymachines/status;goofymachines/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

// Reconcile reads that state of the cluster for a GoofyMachine object and makes changes based on the state read
// and what is in the GoofyMachine.Spec.
func (r *GoofyMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the GoofyMachine instance
	goofyMachine := &infrav1.GoofyMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, goofyMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// AddOwners adds the owners of DockerMachine as k/v pairs to the logger.
	// Specifically, it will add KubeadmControlPlane, MachineSet and MachineDeployment.
	ctx, log, err := clog.AddOwners(ctx, r.Client, goofyMachine)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Fetch the Machine.
	machine, err := util.GetOwnerMachine(ctx, r.Client, goofyMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Waiting for Machine Controller to set OwnerRef on GoofyMachine")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("Machine", klog.KObj(machine))
	ctx = ctrl.LoggerInto(ctx, log)

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("GoofyMachine owner Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this machine with a cluster using the label %s: <name of cluster>", clusterv1.ClusterNameLabel))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("Cluster", klog.KObj(cluster))
	ctx = ctrl.LoggerInto(ctx, log)

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, goofyMachine) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// Fetch the Goofy Cluster.
	goofyCluster := &infrav1.GoofyCluster{}
	goofyClusterName := client.ObjectKey{
		Namespace: goofyMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, goofyClusterName, goofyCluster); err != nil {
		log.Info("GoofyCluster is not available yet")
		return ctrl.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(goofyMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the GoofyMachine object and status after each reconciliation.
	defer func() {
		if err := patchHelper.Patch(ctx, goofyMachine); err != nil {
			log.Error(err, "failed to patch GoofyMachine")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(goofyMachine, infrav1.MachineFinalizer) {
		controllerutil.AddFinalizer(goofyMachine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	}

	// Handle deleted clusters
	if !goofyMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, goofyMachine)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, cluster, machine, goofyMachine)
}

func (r *GoofyMachineReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, machine *clusterv1.Machine, goofyMachine *infrav1.GoofyMachine) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Compute the resource group unique name.
	// NOTE: We are using reconcilerGroup also as a name for the listener for sake of simplicity.
	resourceGroup := klog.KObj(cluster).String()
	cloudClient := r.CloudMgr.GetResourceGroup(resourceGroup).GetClient()

	// Check if the infrastructure is ready, otherwise return and wait for the cluster object to be updated
	if !cluster.Status.InfrastructureReady {
		log.Info("Waiting for GoofyCluster Controller to create cluster infrastructure")
		return ctrl.Result{}, nil
	}

	// Make sure bootstrap data is available and populated.
	// NOTE: we are not using bootstrap data, but we wait for it in order to simulate a real machine
	// provisioning workflow.
	if machine.Spec.Bootstrap.DataSecretName == nil {
		if !util.IsControlPlaneMachine(machine) && !conditions.IsTrue(cluster, clusterv1.ControlPlaneInitializedCondition) {
			log.Info("Waiting for the control plane to be initialized")
			return ctrl.Result{}, nil
		}

		log.Info("Waiting for the Bootstrap provider controller to set bootstrap data")
		return ctrl.Result{}, nil
	}

	// Create VM
	// NOTE: for sake of simplicity we keep cloud resources as global resources (namespace empty).
	// TODO: we should convert this into a full reconcile (if it exist, update)
	cloudMachine := &cloudv1.CloudMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: goofyMachine.Name,
		},
	}
	if err := cloudClient.Create(ctx, cloudMachine); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, errors.Wrapf(err, "failed to create CloudMachine")
	}

	// Create Node
	// TODO: we should convert this into a full reconcile (if it exist, update)
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: goofyMachine.Name,
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("goofy://%s", goofyMachine.Name),
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	if err := cloudClient.Create(ctx, node); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, errors.Wrapf(err, "failed to create Node")
	}

	// If the machine is a control plane:
	if util.IsControlPlaneMachine(machine) {
		// If there  is not yer an API server for this machine.
		apiServer := fmt.Sprintf("kube-apiserver-%s", goofyMachine.Name)
		if !r.ApiServerMux.HasAPIServer(resourceGroup, apiServer) {
			// Getting the Kubernetes CA
			s, err := secret.Get(ctx, r.Client, client.ObjectKeyFromObject(cluster), secret.ClusterCA)
			if err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "failed to get cluster CA")
			}
			certData, exists := s.Data[secret.TLSCrtDataName]
			if !exists {
				return ctrl.Result{}, errors.Errorf("invalid cluster CA: missing data for %s", secret.TLSCrtDataName)
			}

			cert, err := certs.DecodeCertPEM(certData)
			if err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "invalid cluster CA: invalid %s", secret.TLSCrtDataName)
			}

			keyData, exists := s.Data[secret.TLSKeyDataName]
			if !exists {
				return ctrl.Result{}, errors.Errorf("invalid cluster CA: missing data for %s", secret.TLSCrtDataName)
			}

			key, err := certs.DecodePrivateKeyPEM(keyData)
			if err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "invalid cluster CA: invalid %s", secret.TLSCrtDataName)
			}

			// Adding the APIServer.
			// NOTE: When you add the first APIServer, the workload cluster listener is started.
			if err := r.ApiServerMux.AddAPIServer(resourceGroup, apiServer, cert, key.(*rsa.PrivateKey)); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to start API server")
			}
		}
	}

	goofyMachine.Spec.ProviderID = &node.Spec.ProviderID
	goofyMachine.Status.Ready = true

	return ctrl.Result{}, nil
}

func (r *GoofyMachineReconciler) reconcileDelete(ctx context.Context, goofyMachine *infrav1.GoofyMachine) (ctrl.Result, error) {
	// TODO: implement
	controllerutil.RemoveFinalizer(goofyMachine, infrav1.MachineFinalizer)

	return ctrl.Result{}, nil
}

// SetupWithManager will add watches for this controller.
func (r *GoofyMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.GoofyMachine{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), r.WatchFilterValue)).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(util.ClusterToInfrastructureMapFunc(ctx, infrav1.GroupVersion.WithKind("GoofyMachine"), mgr.GetClient(), &infrav1.GoofyMachine{})),
			builder.WithPredicates(
				predicates.ClusterUnpaused(ctrl.LoggerFrom(ctx)),
			),
		).Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}
	return nil
}