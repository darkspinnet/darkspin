package raknet103

import (
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// AttributeMirrorChanges publishes semantic mirror updates through the same
// reflected attribute/current-resource contract as initial NPC admission.
func AttributeMirrorChanges(snapshots []zonenpc.Snapshot) ([][]byte, error) {
	messages := make([]raknet.ApplicationMessage, 0, len(snapshots)*2)
	for _, snapshot := range snapshots {
		profile := snapshot.Plan.NPCProfile
		attributes := map[uint8]float32{
			0: profile.Strength, 1: profile.Dexterity, 2: profile.Mind,
			4: profile.HitPoint, 5: profile.PowerPoint,
			7: profile.DodgeRating, 9: profile.ResistRating, 10: profile.CriticalRating,
			48: zonenpc.EffectiveMovementSpeedBuff(snapshot.Plan),
		}
		for id, amount := range affixAttributes(snapshot) {
			attributes[id] = amount
		}
		messages = append(messages,
			raknet.AttributeDataUpdateMessage{ObjectID: snapshot.Plan.ObjectID, Value: attributes},
			raknet.CombatantDataUpdateMessage{ObjectID: snapshot.Plan.ObjectID, HitPoints: snapshot.HitPoint, ManaPoints: snapshot.ManaPoint},
		)
	}
	return marshalMessages(messages, "mirrorAttributes")
}
