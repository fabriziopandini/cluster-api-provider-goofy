//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright The Kubernetes Authors.

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/api/v1beta1"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *APIEndpoint) DeepCopyInto(out *APIEndpoint) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new APIEndpoint.
func (in *APIEndpoint) DeepCopy() *APIEndpoint {
	if in == nil {
		return nil
	}
	out := new(APIEndpoint)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GoofyCluster) DeepCopyInto(out *GoofyCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GoofyCluster.
func (in *GoofyCluster) DeepCopy() *GoofyCluster {
	if in == nil {
		return nil
	}
	out := new(GoofyCluster)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GoofyCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GoofyClusterList) DeepCopyInto(out *GoofyClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GoofyCluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GoofyClusterList.
func (in *GoofyClusterList) DeepCopy() *GoofyClusterList {
	if in == nil {
		return nil
	}
	out := new(GoofyClusterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GoofyClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GoofyClusterSpec) DeepCopyInto(out *GoofyClusterSpec) {
	*out = *in
	out.ControlPlaneEndpoint = in.ControlPlaneEndpoint
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GoofyClusterSpec.
func (in *GoofyClusterSpec) DeepCopy() *GoofyClusterSpec {
	if in == nil {
		return nil
	}
	out := new(GoofyClusterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GoofyClusterStatus) DeepCopyInto(out *GoofyClusterStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make(v1beta1.Conditions, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GoofyClusterStatus.
func (in *GoofyClusterStatus) DeepCopy() *GoofyClusterStatus {
	if in == nil {
		return nil
	}
	out := new(GoofyClusterStatus)
	in.DeepCopyInto(out)
	return out
}