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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ManagedDatabaseSpec defines the desired state of ManagedDatabase
type ManagedDatabaseSpec struct {
	DesiredSchemaVersion string                 `json:"desiredSchemaVersion,omitempty"`
	Connection           DatabaseConnectionInfo `json:"connection,omitempty"`
	MigrationEngine      string                 `json:"migrationEngine,omitempty"`
}

// DatabaseConnectionInfo defines engine specific connection parameters to establish
// a connection to the database.
type DatabaseConnectionInfo struct {
	Engine    string `json:"engine,omitempty"`
	DSNSecret string `json:"dsnSecret,omitempty"`
}

// ManagedDatabaseError contains information about an error that occurred when
// reconciling this ManagedDatabase, and whether the error is considered
// temporary/transient.
type ManagedDatabaseError struct {
	Message   string `json:"message,omitempty"`
	Temporary bool   `json:"temporary,omitempty"`
}

// ManagedDatabaseStatus defines the observed state of ManagedDatabase
type ManagedDatabaseStatus struct {
	CurrentVersion string                 `json:"currentVersion,omitempty"`
	Errors         []ManagedDatabaseError `json:"errors,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedDatabase is the Schema for the manageddatabases API
// +kubebuilder:subresource:status
type ManagedDatabase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedDatabaseSpec   `json:"spec,omitempty"`
	Status ManagedDatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedDatabaseList contains a list of ManagedDatabase
type ManagedDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedDatabase{}, &ManagedDatabaseList{})
}
