/*
Copyright 2023 The Kubernetes Authors.

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

/*
Package cache defines resource group aware Cache.

The Cache implements sync loop and garbage collector inspired from to ones existing
in Kubernetes, but in this case only finalizers are respected while currently there is not
a concept of ownerReferences.

Note: The cloud runtime is using a Cache for all the resource groups, so it will be possible
to implement controllers processing request from all the resources groups.
*/
package cache
