/*
Package event contains the definitions for the Event types produced by source.Sources and transformed into
reconcile.Requests by handler.EventHandler.

The implementation is derived from sigs.k8s.io/controller-runtime/pkg/event and the main difference is that
events are resourceGroup aware.
*/
package event
