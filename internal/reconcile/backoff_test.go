package reconcile

import (
	"testing"
	"time"
)

func TestComputeBackoff(t *testing.T) {
	tests := []struct {
		name         string
		failureCount int64
		want         time.Duration
	}{
		{name: "zero failures returns base", failureCount: 0, want: 5 * time.Second},
		{name: "first failure returns base", failureCount: 1, want: 5 * time.Second},
		{name: "second failure doubles", failureCount: 2, want: 10 * time.Second},
		{name: "third failure", failureCount: 3, want: 20 * time.Second},
		{name: "fourth failure", failureCount: 4, want: 40 * time.Second},
		{name: "fifth failure", failureCount: 5, want: 80 * time.Second},
		{name: "sixth failure", failureCount: 6, want: 160 * time.Second},
		{name: "seventh failure caps at max", failureCount: 7, want: 5 * time.Minute},
		{name: "tenth failure stays at cap", failureCount: 10, want: 5 * time.Minute},
		{name: "very large failure count stays at cap", failureCount: 100, want: 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeBackoff(tt.failureCount)
			if got != tt.want {
				t.Errorf("ComputeBackoff(%d) = %v, want %v", tt.failureCount, got, tt.want)
			}
		})
	}
}
