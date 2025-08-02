package rtspeek

import "testing"

// Compile-time interface implementation assertion.
func TestStreamInfoInterfaceImplementation(t *testing.T) {
	var _ StreamInfo = &streamInfo{}
}
