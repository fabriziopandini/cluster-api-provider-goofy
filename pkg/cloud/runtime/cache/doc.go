/*
Package cache defines resource group aware Cache.

The Cache implements sync loop and garbage collector inspired from to ones existing
in Kubernetes, but in this case only finalizers are respected while currently there is not
a concept of ownerReferences.

Note: The cloud runtime is using a Cache for all the resource groups, so it will be possible
to implement controllers processing request from all the resources groups.
*/
package cache
