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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ManagedDatabaseSpec defines the desired state of ManagedDatabase
type ManagedDatabaseSpec struct {
	DesiredSchemaVersion     string                    `json:"desiredSchemaVersion,omitempty"`
	Connection               DatabaseConnectionInfo    `json:"connection,omitempty"`
	MigrationEngine          string                    `json:"migrationEngine,omitempty"`
	MigrationContainerConfig *MigrationContainerConfig `json:"migrationContainerConfig,omitempty"`
	ExportDataMetrics        *DataMetrics              `json:"exportDataMetrics,omitempty"`
	HintsEngine              *HintsEngineConfig        `json:"hintsEngine,omitempty"`
}

// HintsEngineConfig defines the values that can be passed to the hints engine
// to help it understand context under which this manageddatabase runs that
// can't be queried from the database directly.
type HintsEngineConfig struct {
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:validation:Minimum=1
	LargeTableRowsThreshold uint64 `json:"largetableRowsThreshold,omitempty"`
}

// DataMetrics declares what information the DBA operator should expose from the
// database under management
type DataMetrics struct {
	TableEstimatedSize *[]TableReference `json:"tableEstimatedSize,omitempty"`
	TableNextID        *[]TableReference `json:"tableNextID,omitempty"`
	SQLQuery           *[]SQLQueryMetric `json:"sqlQuery,omitempty"`
}

// TableReference refers to a DB table by name
type TableReference struct {
	TableName string `json:"tableName,omitempty"`
}

// SQLQueryMetric describes a SQL query to run against the database and how to
// expose it as a metric. It must select exactly one value in one row, and the
// value must represent either a counter (uint) or gauge (float).
type SQLQueryMetric struct {
	// +kubebuilder:validation:Pattern="SELECT [^;]+;"
	Query string `json:"query,omitempty"`

	PrometheusMetric PrometheusMetricExporter `json:"prometheusMetric,omitempty"`
}

// PrometheusMetricExporter describes how a given value should be exported.
type PrometheusMetricExporter struct {
	// +kubebuilder:validation:Enum=counter;gauge
	ValueType string `json:"valueType,omitempty"`

	// +kubebuilder:validation:database_[a-z_]+[a-z]
	Name string `json:"name,omitempty"`

	HelpString  string            `json:"helpString,omitempty"`
	ExtraLabels map[string]string `json:"extraLabels,omitempty"`
}

// DatabaseConnectionInfo defines engine specific connection parameters to establish
// a connection to the database.
type DatabaseConnectionInfo struct {
	// +kubebuilder:validation:Enum=mysql
	Engine string `json:"engine,omitempty"`

	// +kubebuilder:validation:MinLength=1
	DSNSecret string `json:"dsnSecret,omitempty"`
}

// MigrationContainerConfig defines extra configuration that a migration
// container may require before it is able to run. Specify a secret name
// and how to bind that into the container.
type MigrationContainerConfig struct {
	Secret      string             `json:"secret,omitempty"`
	VolumeMount corev1.VolumeMount `json:"volumeMount,omitempty"`
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
