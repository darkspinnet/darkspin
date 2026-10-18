package zone

import (
	"errors"
	"fmt"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// AlertNPCAllies resolves current connected, visible player-aligned targets
// under the zone lock. A source cannot share a dead or stealthed hero target.
func (e *Zone) AlertNPCAllies(
	sourceObjectIDs []uint32, rules zonenpc.AllyAlertRules,
) ([]zonenpc.AllyAlert, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || e.info.NPCs == nil {
		return nil, errors.New("campaign zone not active")
	}
	alerts, err := e.info.NPCs.AlertAllies(zonenpc.AllyAlertRequest{
		SourceObjectIDs: sourceObjectIDs, Targets: e.livePlayerAlignedTargets(), Rules: rules,
	})
	if err != nil {
		return nil, fmt.Errorf("npcAllyAlert: %w", err)
	}
	for _, alert := range alerts {
		e.info.Timeline.Cancel(zonenpc.ReturnTimelineKey(alert.Recipient.Plan.ObjectID))
	}
	return alerts, nil
}
