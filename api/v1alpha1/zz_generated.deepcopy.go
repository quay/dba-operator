// +build !ignore_autogenerated

/*

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

// autogenerated by controller-gen object, do not modify manually

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseConnectionInfo) DeepCopyInto(out *DatabaseConnectionInfo) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseConnectionInfo.
func (in *DatabaseConnectionInfo) DeepCopy() *DatabaseConnectionInfo {
	if in == nil {
		return nil
	}
	out := new(DatabaseConnectionInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseMigration) DeepCopyInto(out *DatabaseMigration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseMigration.
func (in *DatabaseMigration) DeepCopy() *DatabaseMigration {
	if in == nil {
		return nil
	}
	out := new(DatabaseMigration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DatabaseMigration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseMigrationList) DeepCopyInto(out *DatabaseMigrationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DatabaseMigration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseMigrationList.
func (in *DatabaseMigrationList) DeepCopy() *DatabaseMigrationList {
	if in == nil {
		return nil
	}
	out := new(DatabaseMigrationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DatabaseMigrationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseMigrationPhase) DeepCopyInto(out *DatabaseMigrationPhase) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseMigrationPhase.
func (in *DatabaseMigrationPhase) DeepCopy() *DatabaseMigrationPhase {
	if in == nil {
		return nil
	}
	out := new(DatabaseMigrationPhase)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseMigrationSpec) DeepCopyInto(out *DatabaseMigrationSpec) {
	*out = *in
	if in.Phases != nil {
		in, out := &in.Phases, &out.Phases
		*out = make([]DatabaseMigrationPhase, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseMigrationSpec.
func (in *DatabaseMigrationSpec) DeepCopy() *DatabaseMigrationSpec {
	if in == nil {
		return nil
	}
	out := new(DatabaseMigrationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseMigrationStatus) DeepCopyInto(out *DatabaseMigrationStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseMigrationStatus.
func (in *DatabaseMigrationStatus) DeepCopy() *DatabaseMigrationStatus {
	if in == nil {
		return nil
	}
	out := new(DatabaseMigrationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedDatabase) DeepCopyInto(out *ManagedDatabase) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedDatabase.
func (in *ManagedDatabase) DeepCopy() *ManagedDatabase {
	if in == nil {
		return nil
	}
	out := new(ManagedDatabase)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ManagedDatabase) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedDatabaseList) DeepCopyInto(out *ManagedDatabaseList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ManagedDatabase, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedDatabaseList.
func (in *ManagedDatabaseList) DeepCopy() *ManagedDatabaseList {
	if in == nil {
		return nil
	}
	out := new(ManagedDatabaseList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ManagedDatabaseList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedDatabaseSpec) DeepCopyInto(out *ManagedDatabaseSpec) {
	*out = *in
	out.Connection = in.Connection
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedDatabaseSpec.
func (in *ManagedDatabaseSpec) DeepCopy() *ManagedDatabaseSpec {
	if in == nil {
		return nil
	}
	out := new(ManagedDatabaseSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedDatabaseStatus) DeepCopyInto(out *ManagedDatabaseStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedDatabaseStatus.
func (in *ManagedDatabaseStatus) DeepCopy() *ManagedDatabaseStatus {
	if in == nil {
		return nil
	}
	out := new(ManagedDatabaseStatus)
	in.DeepCopyInto(out)
	return out
}