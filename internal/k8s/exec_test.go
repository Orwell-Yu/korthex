package k8s

import (
	"testing"
)

// Compile-time interface compliance check.
var _ PodExecutor = (*k8sClient)(nil)

func TestClientImplementsPodExecutor(t *testing.T) {
	// This test exists solely to verify the compile-time assertion above.
	// If k8sClient does not implement PodExecutor, this file will not compile.
}
