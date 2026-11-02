package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const explodingTreeDelay = 200 * time.Millisecond

func isExplodingTree(nounName string) bool {
	return strings.EqualFold(nounName, "exploding_tree.Noun")
}

func explodingTreeDeathProfile() zonenpc.ActionProfile {
	return zonenpc.ActionProfile{
		Family: zonenpc.ActionRetainedArea, AbilityName: "ExplodingTreeDeath", Radius: 15,
		DamageType: 4, DamageSource: 0, DescriptorMask: 1<<3 | 1<<6,
		IsDamageProfileKnown: true, ModifierName: "Fear", ModifierDuration: 3 * time.Second,
	}
}

func withExplodingTreeDeath(snapshot zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	if !snapshot.Plan.IsFixture || !isExplodingTree(snapshot.Plan.NounName) {
		return definition
	}
	definition.isRemnantRetained = true
	definition.isCollisionRetained = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.deleteDelay = 0
	// The callback sets dead graphics after its delayed query, not at death.
	definition.graphicsState = 0
	definition.explosionEffectName = "effect_environment_nocturna_tree_death.ServerEventDef"
	return definition
}

func (e campaignFixtureBlastStep) treeFear(target zonenpc.Snapshot) ([][]byte, error) {
	if target.Plan.IsFixture {
		return nil, nil
	}
	// Build103 A0B670 calls the constant-true E224A0 stub and returns
	// (true, 0, false) for a valid target, including authored zero damage.
	// Do not turn this acceptance condition into positive damage or a hit event.
	npcRuntime := e.runtime.npc
	npcRuntime.registry = e.runtime.registry
	npcRuntime.pursuit.registry = e.runtime.registry
	npcRuntime.pursuit.now = npcRuntime.now
	npcRuntime.pursuit.logger = e.runtime.logger
	runtime := campaignAbilityCommandRuntime{
		registry: e.runtime.registry, modifierPool: npcRuntime.modifierPool,
		npc: npcRuntime, now: npcRuntime.now, logger: e.runtime.logger,
	}
	profile := explodingTreeDeathProfile()
	packets, err := runtime.applyNPCFear(e.packet, e.sessionKey, e.generation,
		target.Plan.ObjectID, e.source.Plan.ObjectID, profile.ModifierDuration,
		e.timestamp, profile.ModifierName, true)
	if err != nil {
		return packets, fmt.Errorf("treeFearApply: %w", err)
	}
	return packets, nil
}

func (e campaignFixtureBlastStep) finishExplodingTree() ([][]byte, error) {
	if !isExplodingTree(e.source.Plan.NounName) {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.SetObjectGFXStateMessage{
		ObjectID: e.source.Plan.ObjectID, State: util.HashID("dead"), Timestamp: e.timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("treeGraphics: %w", err)
	}
	packets := [][]byte{packet}
	e.publication.recordPreparedPackets(packets)
	return packets, nil
}
