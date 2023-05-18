/*
Package reconcile defines the Reconciler interface; Reconciler is provided
to Controllers at creation time as the API implementation.

The implementation is derived from sigs.k8s.io/controller-runtime/pkg/reconcile and the main difference is that
Reconciler and Request are resourceGroup aware.
*/
package reconcile
