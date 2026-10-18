//go:build scenario

package scenario

// ProtocolVersion identifies compiled scenario support, not a live peer check.
const ProtocolVersion uint32 = 1

// Capability describes one component compiled with scenario support. A local
// capability must never be used as evidence that a remote peer supports it.
type Capability struct {
	ProtocolVersion uint32
	Component       string
	BuildID         string
	Features        []string
}

// NewCapability reports compiled support. BuildID must come from the component
// itself; environment flags cannot change this capability or prove provenance.
func NewCapability(component, buildID string) Capability {
	return Capability{
		ProtocolVersion: ProtocolVersion,
		Component:       component,
		BuildID:         buildID,
		Features:        []string{"scenario-v1"},
	}
}
