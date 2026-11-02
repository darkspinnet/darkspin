//go:build !scenario

package zone

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func (e *Zone) scenarioFixturePlans() []zonenpc.SpawnPlan { return nil }
