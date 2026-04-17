package core

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MarshalJSON returns the JSON byte representation of the resource's CUE value.
func (r *Resource) MarshalJSON() ([]byte, error) {
	if err := r.Value.Err(); err != nil {
		return nil, fmt.Errorf("resource %s: cue value error: %w", r.String(), err)
	}
	b, err := r.Value.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("resource %s: marshal json: %w", r.String(), err)
	}
	return b, nil
}

// ToUnstructured converts the resource to a *unstructured.Unstructured.
// Uses JSON as the intermediate format.
func (r *Resource) ToUnstructured() (*unstructured.Unstructured, error) {
	j, err := r.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(j, &obj); err != nil {
		return nil, fmt.Errorf("resource %s: unmarshal to map: %w", r.String(), err)
	}
	return &unstructured.Unstructured{Object: obj}, nil
}
