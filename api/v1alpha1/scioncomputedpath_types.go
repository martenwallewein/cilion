package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ScionComputedPathSpec defines the desired state of ScionComputedPath
type ScionComputedPathSpec struct {
	// PolicyRef points back to the ScionPathPolicy that generated this path
	PolicyRef string `json:"policyRef"`

	// DestinationCIDR is the subnet of the remote cluster (e.g., 10.244.2.0/24)
	DestinationCIDR string `json:"destinationCidr"`

	// NextHopIP is the SCION Border Router / Transit Provider IP
	NextHopIP string `json:"nextHopIP"`

	// HopFields contains the raw byte array of the SCION cryptographic path for eBPF
	// +optional
	HopFields []byte `json:"hopFields,omitempty"`
}

// ScionComputedPathStatus defines the observed state of ScionComputedPath
type ScionComputedPathStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScionComputedPath is the Schema for the scioncomputedpaths API
type ScionComputedPath struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec ScionComputedPathSpec `json:"spec"`

	// +optional
	Status ScionComputedPathStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScionComputedPathList contains a list of ScionComputedPath
type ScionComputedPathList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ScionComputedPath `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScionComputedPath{}, &ScionComputedPathList{})
}
