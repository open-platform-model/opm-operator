package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/open-platform-model/opm-operator/pkg/core"
)

func TestIsOPMManagedBy(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "cli actor", value: "opm-cli", want: true},
		{name: "controller actor", value: "opm-controller", want: true},
		{name: "legacy value", value: "open-platform-model", want: true},
		{name: "empty string", value: "", want: false},
		{name: "helm", value: "Helm", want: false},
		{name: "arbitrary", value: "some-other-tool", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, core.IsOPMManagedBy(tt.value))
		})
	}
}

func TestLabelConstants(t *testing.T) {
	assert.Equal(t, "opm-cli", core.LabelManagedByValue)
	assert.Equal(t, "opm-controller", core.LabelManagedByControllerValue)
	assert.Equal(t, "open-platform-model", core.LabelManagedByLegacyValue)
}
