/*
Copyright 2026.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// URLShortenerSpec defines the desired state of URLShortener
type URLShortenerSpec struct {
	// TargetURL is the full URL to redirect visitors to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://.+`
	TargetURL string `json:"targetUrl"`

	// CustomSlug is an optional custom short path identifier.
	// If omitted, the service will generate a random slug.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=32
	CustomSlug string `json:"customSlug,omitempty"`
}

// URLShortenerStatus defines the observed state of URLShortener.
type URLShortenerStatus struct {
	// ShortURL is the fully qualified short URL visitors can use.
	// +optional
	ShortURL string `json:"shortUrl,omitempty"`

	// Slug is the unique identifier assigned by the backend service.
	// +optional
	Slug string `json:"slug,omitempty"`

	// Hits is the number of times this short URL has been accessed.
	// +kubebuilder:default=0
	Hits int64 `json:"hits"`

	// Phase represents the current state: Pending, Ready, Error.
	// +kubebuilder:default="Pending"
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent observations of the resource state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastPolledTime records the last time the operator fetched hit stats.
	// +optional
	LastPolledTime *metav1.Time `json:"lastPolledTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type="string",JSONPath=".spec.targetUrl"
// +kubebuilder:printcolumn:name="Short URL",type="string",JSONPath=".status.shortUrl"
// +kubebuilder:printcolumn:name="Hits",type="integer",JSONPath=".status.hits"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// URLShortener is the Schema for the urlshorteners API
type URLShortener struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of URLShortener
	// +required
	Spec URLShortenerSpec `json:"spec"`

	// status defines the observed state of URLShortener
	// +optional
	Status URLShortenerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// URLShortenerList contains a list of URLShortener
type URLShortenerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []URLShortener `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &URLShortener{}, &URLShortenerList{})
		return nil
	})
}
