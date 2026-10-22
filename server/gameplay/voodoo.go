package gameplay

import (
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
)

const voodooCharmPollInterval = 100 * time.Millisecond

type voodooActorKey struct {
	zone          *zone.Zone
	member        gameplayMemberKey
	generation    uint64
	objectID      uint32
	creatureIndex uint32
}

type voodooActor struct {
	key      voodooActorKey
	position game.Vec3
	member   gameplayPeerSession
}

type voodooCharmProvider struct {
	radius        float32
	charm         voodooCharmReturn
	contacts      map[voodooActorKey]bool
	deathRevision uint64
}

// Each provider remembers rejected requests too. A rejected UniqueIrreplaceable
// request returns GUID zero, which AuraUtils retains until that actor exits.
type voodooCharmWorld struct {
	providers    map[voodooActorKey]*voodooCharmProvider
	owners       map[voodooActorKey]voodooActorKey
	deaths       map[voodooActorKey]uint64
	handovers    map[gameplayMemberKey]*voodooCharmHandover
	cancel       func()
	pollRevision uint64
}

type voodooCharmHandover struct {
	member gameplayPeerSession
}

func (e *gameplaySessionRegistry) releaseVoodooHandover(
	key gameplayMemberKey, expected *voodooCharmHandover,
) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.voodooCharms.releaseHandover(key, expected)
}

func (e *voodooCharmWorld) releaseHandover(
	key gameplayMemberKey, expected *voodooCharmHandover,
) {
	if e.handovers[key] == expected {
		delete(e.handovers, key)
	}
}

func voodooActorIdentity(member gameplayPeerSession) voodooActorKey {
	return voodooActorKey{
		zone: member.zone, member: gameplaySessionMemberKey(member),
		generation: member.generation, objectID: member.deployedObjectID,
		creatureIndex: member.deployedCreatureIndex,
	}
}

func (e *gameplaySessionRegistry) voodooActorsLocked(now time.Time) []voodooActor {
	members := make(map[gameplayMemberKey]gameplayPeerSession)
	for key, handover := range e.voodooCharms.handovers {
		member := handover.member
		actor, isFound := member.zone.Hero().Snapshot(member.binding.UserID, member.generation)
		if !isFound || actor.ObjectID != member.deployedObjectID {
			delete(e.voodooCharms.handovers, key)
			continue
		}
		members[key] = member
	}
	for key, retained := range e.retainedSessions {
		if now.Before(retained.expiresAt) {
			members[key] = retained.peerSession
		}
	}
	for _, member := range e.sessions {
		members[gameplaySessionMemberKey(member)] = member
		delete(e.voodooCharms.handovers, gameplaySessionMemberKey(member))
	}
	actors := make([]voodooActor, 0, len(members))
	for _, member := range members {
		if member.zone == nil || member.deployedObjectID == 0 ||
			(!member.stage.IsDungeon() && !member.isRejoinPending) || !member.dungeonSetup.IsCommitted() ||
			member.isZoneTerminal() || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
			continue
		}
		position := game.Vec3(member.playerPosition)
		if member.playerMotion != nil {
			sample, err := member.playerMotion.SamplePosition(now)
			if err != nil {
				if e.logger != nil {
					e.logger.Printf("RakNet Voodoo aura sample failed user=%d: %v", member.binding.UserID, err)
				}
				continue
			}
			position = game.Vec3{X: sample.X, Y: sample.Y, Z: sample.Z}
		}
		actors = append(actors, voodooActor{key: voodooActorIdentity(member), position: position, member: member})
	}
	// Simultaneous first observations have no recovered native ordering. Keep
	// the server's tie deterministic without preferring an Overdrive payload.
	slices.SortFunc(actors, compareVoodooActors)
	return actors
}

func compareVoodooActors(first, second voodooActor) int {
	if first.key.member.gameID < second.key.member.gameID {
		return -1
	}
	if first.key.member.gameID > second.key.member.gameID {
		return 1
	}
	if first.key.member.userID < second.key.member.userID {
		return -1
	}
	if first.key.member.userID > second.key.member.userID {
		return 1
	}
	return 0
}

func (e *gameplaySessionRegistry) refreshVoodooCharmsLocked(now time.Time) {
	if e == nil || e.isClosed {
		return
	}
	actors := e.voodooActorsLocked(now)
	world := &e.voodooCharms
	if world.providers == nil {
		world.providers = make(map[voodooActorKey]*voodooCharmProvider)
		world.owners = make(map[voodooActorKey]voodooActorKey)
		world.deaths = make(map[voodooActorKey]uint64)
	}
	providers := make(map[voodooActorKey]voodooActor)
	targets := make(map[voodooActorKey]voodooActor, len(actors))
	for _, actor := range actors {
		targets[actor.key] = actor
		if world.deaths[actor.key] != actor.member.voodooDeathRevision {
			delete(world.owners, actor.key)
		}
		world.deaths[actor.key] = actor.member.voodooDeathRevision
		if actor.member.deployedHitPoint() > 0 &&
			actor.member.binding.Creatures[actor.key.creatureIndex].PassiveAbility == util.HashID("VoodooTempestPassive") {
			providers[actor.key] = actor
		}
	}
	// Process exits before new enters; cleanup removes only this provider's
	// accepted child, leaving other providers' rejected contacts unchanged.
	for key, provider := range world.providers {
		source, isSourceFound := providers[key]
		if !isSourceFound || provider.deathRevision != source.member.voodooDeathRevision {
			world.removeProvider(key)
			continue
		}
		for targetKey := range provider.contacts {
			target, isTargetFound := targets[targetKey]
			if !isTargetFound || source.position.Sub(target.position).Length() > provider.radius {
				world.removeContact(key, targetKey)
			}
		}
	}
	for targetKey := range world.owners {
		target, isTargetFound := targets[targetKey]
		if !isTargetFound || target.member.deployedHitPoint() <= 0 {
			delete(world.owners, targetKey)
		}
	}
	for targetKey := range world.deaths {
		if _, isTargetFound := targets[targetKey]; !isTargetFound {
			delete(world.deaths, targetKey)
		}
	}
	for _, source := range actors {
		if _, isProvider := providers[source.key]; !isProvider {
			continue
		}
		provider := world.providers[source.key]
		if provider == nil {
			provider = &voodooCharmProvider{radius: 8, charm: voodooCharmReturn{manaRatio: 0.02}, contacts: make(map[voodooActorKey]bool)}
			provider.deathRevision = source.member.voodooDeathRevision
			if source.member.isOverdriveActiveAt(now) {
				provider.radius = 12
				provider.charm = voodooCharmReturn{manaRatio: 0.04, healthRatio: 0.04}
			}
			world.providers[source.key] = provider
		}
		for _, target := range actors {
			if target.key.zone != source.key.zone || source.position.Sub(target.position).Length() > provider.radius {
				continue
			}
			if _, isInside := provider.contacts[target.key]; isInside {
				continue
			}
			provider.contacts[target.key] = false
			if target.member.deployedHitPoint() <= 0 {
				continue
			}
			if _, isOwned := world.owners[target.key]; isOwned {
				continue
			}
			provider.contacts[target.key] = true
			world.owners[target.key] = source.key
		}
	}
	if len(world.providers) == 0 {
		if world.cancel != nil {
			world.cancel()
			world.cancel = nil
			world.pollRevision++
		}
		return
	}
	if world.cancel == nil && e.timer != nil {
		world.pollRevision++
		step := voodooCharmPoll{registry: e, revision: world.pollRevision}
		cancel, err := e.timer.Schedule(voodooCharmPollInterval, step.execute)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet Voodoo aura scheduling failed: %v", err)
			}
			return
		}
		world.cancel = cancel
	}
}

func (e *voodooCharmWorld) removeContact(providerKey, targetKey voodooActorKey) {
	if e.owners[targetKey] == providerKey {
		delete(e.owners, targetKey)
	}
	delete(e.providers[providerKey].contacts, targetKey)
}

func (e *voodooCharmWorld) removeProvider(providerKey voodooActorKey) {
	provider := e.providers[providerKey]
	if provider == nil {
		return
	}
	for targetKey := range provider.contacts {
		e.removeContact(providerKey, targetKey)
	}
	delete(e.providers, providerKey)
}

type voodooCharmPoll struct {
	registry *gameplaySessionRegistry
	revision uint64
}

func (e voodooCharmPoll) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if e.revision != e.registry.voodooCharms.pollRevision {
		return
	}
	e.registry.voodooCharms.cancel = nil
	e.registry.refreshVoodooCharmsLocked(e.registry.now())
}

func (e *gameplaySessionRegistry) voodooCharmReturn(member gameplayPeerSession) voodooCharmReturn {
	e.refreshVoodooCharmsLocked(e.now())
	providerKey, isFound := e.voodooCharms.owners[voodooActorIdentity(member)]
	if !isFound {
		return voodooCharmReturn{}
	}
	provider := e.voodooCharms.providers[providerKey]
	if provider == nil {
		return voodooCharmReturn{}
	}
	return provider.charm
}
