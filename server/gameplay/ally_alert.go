package gameplay

import (
	"fmt"
	"slices"

	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// AllyAlertRuleSource supplies a consistent policy for one propagation pass.
type AllyAlertRuleSource interface {
	AllyAlertRules() zonenpc.AllyAlertRules
}

type fixedAllyAlertRules struct{ rules zonenpc.AllyAlertRules }

func (e fixedAllyAlertRules) AllyAlertRules() zonenpc.AllyAlertRules { return e.rules }

func (e campaignNPCActionRuntime) alertAllies(
	instance *zone.Zone, plans []zonenpc.SpawnPlan, timestamp uint64,
) ([]zonenpc.SpawnPlan, error) {
	if e.allyAlertRuleSource == nil {
		return plans, nil
	}
	rules := e.allyAlertRuleSource.AllyAlertRules()
	if !rules.IsEnabled {
		return plans, nil
	}
	sourceObjectIDs := make([]uint32, 0, len(plans))
	planObjectIDs := make(map[uint32]struct{}, len(plans))
	for _, plan := range plans {
		sourceObjectIDs = append(sourceObjectIDs, plan.ObjectID)
		planObjectIDs[plan.ObjectID] = struct{}{}
	}
	alerts, err := instance.AlertNPCAllies(sourceObjectIDs, rules)
	if err != nil {
		return nil, fmt.Errorf("alertAllies: %w", err)
	}
	if len(alerts) == 0 {
		return plans, nil
	}
	plans = slices.Clone(plans)
	for _, alert := range alerts {
		if _, isPlanned := planObjectIDs[alert.Recipient.Plan.ObjectID]; !isPlanned {
			plans = append(plans, alert.Recipient.Plan)
			planObjectIDs[alert.Recipient.Plan.ObjectID] = struct{}{}
		}
		if rules.IsDiagnosticLoggingEnabled && e.logger != nil {
			e.logger.Printf(
				"NPC ally alert time=%d source=%d recipient=%d noun=%s target=%d hop=%d distance=%.3f range=%.3f range_owner=%s",
				timestamp, alert.SourceObjectID, alert.Recipient.Plan.ObjectID,
				alert.Recipient.Plan.NounName, alert.Recipient.TargetObjectID,
				alert.Hop, alert.Distance, alert.Range, rules.RangeOwner,
			)
		}
	}
	return plans, nil
}
