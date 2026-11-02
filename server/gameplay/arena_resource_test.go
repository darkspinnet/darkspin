package gameplay

import (
	"testing"

	"github.com/darkspinnet/darkspin/server/game"
)

func TestArenaTemporaryCapacityIgnoresCampaignCache(t *testing.T) {
	member := gameplayPeerSession{}
	member.binding.Mode = game.ModeArena
	member.binding.Creatures[0] = game.GameplayCreature{Noun: 11, HitPoint: 100, PowerPoint: 60}
	member.maximumHitPoints[0] = 999
	member.maximumManaPoints[0] = 888
	member.arenaHitPointIncreases[0] = 25
	maximumHealth, maximumMana := member.characterResourceMaximum(0)
	if maximumHealth != 125 || maximumMana != 60 {
		t.Fatalf("temporary arena resources=%g/%g, want 125/60", maximumHealth, maximumMana)
	}
	run := &fieldMedicHeroBuffRun{creatureIndex: 0, maximumHitPoint: 25}
	err := run.remove(&member)
	if err != nil {
		t.Fatalf("remove capacity: %v", err)
	}
	maximumHealth, maximumMana = member.characterResourceMaximum(0)
	if maximumHealth != 100 || maximumMana != 60 {
		t.Fatalf("expired arena resources=%g/%g, want 100/60", maximumHealth, maximumMana)
	}
	if member.maximumHitPoints[0] != 999 || member.maximumManaPoints[0] != 888 {
		t.Fatal("arena modifier changed campaign resource caches")
	}
}
