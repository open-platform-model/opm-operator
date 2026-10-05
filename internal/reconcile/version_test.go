package reconcile

import "testing"

func strPtr(s string) *string { return &s }

func TestRecordNoOpVersion(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		rendered *string
		want     string
	}{
		{name: "no render leaves the field", field: "0.1.0", rendered: nil, want: "0.1.0"},
		{name: "render corrects a stale version", field: "9.9.9", rendered: strPtr("0.1.0"), want: "0.1.0"},
		{name: "render with no version clears the field", field: "0.1.0", rendered: strPtr(""), want: ""},
		{name: "render fills an empty field", field: "", rendered: strPtr("0.1.0"), want: "0.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := tt.field
			recordNoOpVersion(&field, tt.rendered)
			if field != tt.want {
				t.Errorf("recordNoOpVersion(%q, %v) = %q, want %q", tt.field, tt.rendered, field, tt.want)
			}
		})
	}
}

func TestAppliedVersion(t *testing.T) {
	tests := []struct {
		name     string
		rendered *string
		want     string
	}{
		{name: "no render gives empty", rendered: nil, want: ""},
		{name: "render with no version gives empty", rendered: strPtr(""), want: ""},
		{name: "render gives its version", rendered: strPtr("0.1.0"), want: "0.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appliedVersion(tt.rendered); got != tt.want {
				t.Errorf("appliedVersion(%v) = %q, want %q", tt.rendered, got, tt.want)
			}
		})
	}
}
