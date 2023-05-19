/*
Package predicate defines Predicates used by Controllers to filter Events before they are provided to EventHandlers.

The implementation is derived from sigs.k8s.io/controller-runtime/pkg/predicate and the main difference are:
- predicates are resourceGroup aware.
- the package doesn't provide predicates implementation out of the box.
*/
package predicate
