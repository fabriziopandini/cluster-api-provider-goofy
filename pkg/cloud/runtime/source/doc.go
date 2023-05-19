/*
Package source provides event streams to hook up to Controllers with Controller.Watch.  Events are
used with handler.EventHandlers to enqueue reconcile.Requests and trigger Reconciles for resources.

The implementation is derived from sigs.k8s.io/controller-runtime/pkg/source  and the main difference are:
- sources are resourceGroup aware.
- the package provide only one sources implementation, Kind.
*/
package source
