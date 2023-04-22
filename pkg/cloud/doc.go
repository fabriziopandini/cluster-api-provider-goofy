/*
Package cloud implements an in memory cloud provider.

Cloud provider objects are grouped in resource groups, similarly to resource groups in Azure.

Cloud provider objects are defined like Kubernetes objects and they can be operated with
a client inspired from the controller-runtime client.

The Manager, is the object responsible for the lifecycle of objects; it also allow
defining controllers.
*/
package cloud
