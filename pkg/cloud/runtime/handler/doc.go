/*
Package handler defines EventHandlers that enqueue reconcile.Requests in response to Create, Update, Deletion Events
observed from Watching Kubernetes APIs.

The implementation is derived from sigs.k8s.io/controller-runtime/pkg/handler and the main difference are:
- event handlers are resourceGroup aware.
- the package provide only one event handler implementation, EnqueueRequestForObject.
*/
package handler
