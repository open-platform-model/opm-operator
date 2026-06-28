package core

import (
	librarycore "github.com/open-platform-model/library/opm/core"
)

// ResourceFromCompiled adapts a library *core.Compiled — the kernel's terminal
// per-transformer output — into the operator's *Resource. The two types carry
// identical fields (CUE value plus instance/component/transformer provenance),
// so the adapter is a field copy; it exists so the kernel-backed render path
// can hand its compiled output to the operator's existing inventory and apply
// pipeline unchanged.
//
// A nil input yields a nil result.
func ResourceFromCompiled(c *librarycore.Compiled) *Resource {
	if c == nil {
		return nil
	}
	return &Resource{
		Value:       c.Value,
		Instance:    c.Instance,
		Component:   c.Component,
		Transformer: c.Transformer,
	}
}
