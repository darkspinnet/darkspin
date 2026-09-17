package raknet103

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
	simraknet103 "github.com/darkspinnet/darkspin/server/sim/raknet103"
)

type roleResolver map[sim.Role]simraknet103.Binding

func (r roleResolver) ResolveRole(
	_ context.Context, role sim.Role,
) (simraknet103.Binding, error) {
	binding, isFound := r[role]
	if !isFound {
		return simraknet103.Binding{}, fmt.Errorf("roleBinding: %s", role)
	}
	return binding, nil
}
