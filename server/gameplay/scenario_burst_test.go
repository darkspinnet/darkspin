//go:build scenario

package gameplay

import (
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sim"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
)

func TestScenarioBurstObservesAnyAuthoredSlot(t *testing.T) {
	fixtures := []struct {
		asset string
		slot  zonecontent.HeroAbilitySlot
	}{
		{asset: "MissileTempestActive", slot: zonecontent.HeroAbilitySpecialTwo},
		{asset: "LightningTempest_Active", slot: zonecontent.HeroAbilitySpecialTwo},
		{asset: "SoulRavagerSupport", slot: zonecontent.HeroAbilityRandom},
		{asset: "SoulRavagerActive", slot: zonecontent.HeroAbilitySpecialTwo},
		{asset: "genericBasic", slot: zonecontent.HeroAbilityBasic},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.asset, func(t *testing.T) {
			registry := newGameplaySessionRegistry(nil, time.Now, nil)
			member := arenaBurstTestMember(t, 1, 1, 1, 100)
			setupGeneration, isReserved := member.dungeonSetup.Reserve()
			if !isReserved || !member.dungeonSetup.Commit(setupGeneration) {
				t.Fatal("commit setup")
			}
			ability := zonecontent.HeroAbility{ID: 17, AssetName: fixture.asset,
				Definition: sim.AbilityDefinition{Kind: sim.AbilityKindProjectileBurst}}
			program := Programs{HeroKits: map[uint32]zonecontent.HeroKit{
				1: {Abilities: map[zonecontent.HeroAbilitySlot]zonecontent.HeroAbility{fixture.slot: ability}},
			}}
			registry.configureScenarioAbility(program)
			runtime := campaignAbilityCommandRuntime{registry: registry, program: program, now: time.Now}
			cast := member.prepareScenarioBurst(campaignCharacterAbilityRequest{runtime: runtime}, 17, time.Now(), raknet.Vector3{}, 2, 3)
			if cast == nil || cast.state.AbilityAsset != fixture.asset || cast.state.ExpectedLaunchCount != 3 {
				t.Fatal("authored burst slot was not prepared")
			}
			registry.sessions["source"] = member
			runtime.acceptScenarioRide("source", cast, time.Now())
			member = registry.sessions["source"]
			member.launchScenarioBurst(cast, 2, time.Now())
			member.releaseScenarioRide(cast, time.Now())
			before := registry.scenarioAbilitySnapshot(member, time.Now())
			world := registry.scenarioArenaWorld(member, scenario.WorldState{})
			if world.Ability == nil || world.Ability.Cast == nil || world.Ability.Cast.Revision != cast.state.Revision {
				t.Fatal("arena world omitted its real burst producer")
			}
			if before.Cast == nil || before.Cast.IsBurstCompleted || before.Cast.LaunchCount != 3 || !before.Cast.IsReleased {
				t.Fatalf("release manufactured completion or hid cast: %+v", before.Cast)
			}
			member.completeScenarioBurst(cast, time.Now())
			member.cancelScenarioRide("teardown", time.Now())
			after := registry.scenarioAbilitySnapshot(member, time.Now())
			if after.Cast == nil || !after.Cast.IsAccepted || !after.Cast.IsBurstCompleted || after.Cast.IsFailed || after.Phase != "completed" {
				t.Fatalf("terminal producer evidence missing: %+v", after.Cast)
			}
			member.transportGeneration++
			stale := registry.scenarioAbilitySnapshot(member, time.Now())
			if stale.Cast != nil {
				t.Fatal("cast crossed transport lifetime")
			}
		})
	}
}

func TestScenarioBurstRequiresEveryLaunchAndRelease(t *testing.T) {
	for _, isReleased := range []bool{false, true} {
		member := arenaBurstTestMember(t, 1, 1, 1, 100)
		ability := zonecontent.HeroAbility{ID: 17, AssetName: "genericBurst",
			Definition: sim.AbilityDefinition{Kind: sim.AbilityKindProjectileBurst}}
		cast := member.prepareScenarioRide(campaignCharacterAbilityRequest{}, ability, time.Now(), raknet.Vector3{}, 2)
		if cast == nil {
			t.Fatal("prepare burst")
		}
		cast.state.ExpectedLaunchCount = 3
		launchIndex := 2
		if isReleased {
			launchIndex = 1
			member.releaseScenarioRide(cast, time.Now())
		}
		member.launchScenarioBurst(cast, launchIndex, time.Now())
		member.completeScenarioBurst(cast, time.Now())
		if !cast.state.IsFailed || cast.state.IsBurstCompleted {
			t.Fatal("incomplete burst reported completion")
		}
	}
}
