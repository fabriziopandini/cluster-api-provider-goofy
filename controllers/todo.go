package controllers

/*

Definitions:

- controllers might operates in 3 context
  - the management cluster									-> managementClusterClient (CR client, exist in etcd)
  - the space where cloud resources are stored				-> cloudClient (could be in both places, let's start having this in etcd)
  - the space where workload cluster resources are stored   -> workloadClusterClient (Goofy client, existing in a memory tracker)
  - NOTE: usually provider don't have the 3rd context, because it is stored in the actual workload cluster, but here
    the workload cluster will be totally fake

Implement following controllers:
- GoofyCluster controller
- GoofyMachine controller
- GoofyVM controller

----

GoofyCluster controller
- Responsible to implement the CAPI contract for infra-cluster

- Watches for
	- Cluster
    - GoofyCluster

- Reconcile
  - We do not need a LB, because there will be only one API server, so we can simply compute a control plane address based on a naming convention.
  - How to get control plane address?
	- we have to compute the ip of the server, which is the ip of the controller (of the svc; also possible to use and additional service).
	- each cluster will have a cp endpoint equal to <ip-of-the-server>/apiserver/<cluster-name>
	- then we have to create a service, pointing to server
	- (alternative use many ports)

----

GoofyMachine controller
- Responsible to implement the CAPI contract for infra-machines

- Watches for
	- Machine
	- GoofyMachine
    - GoofyVM << we need the possibility to watch cross boundaries (etcd, memory)

- Reconcile
  - use the cloudClient to create a GoofyVM

----

GoofyVM controller
- Responsible to be a minimal replacement of kubelet (create node) and cloud-init (run kubeadm init and join)

- Watches for
    - (Maybe) GoofyMachine
    - GoofyVM

- Reconcile
	- workloadClusterClient to create a Node
    - workloadClusterClient to create a StaticPods
    - workloadClusterClient to create a few ClusterRole and ClusterRoleObject
	- workloadClusterClient to create the kubeadm-config ConfigMap

*/
