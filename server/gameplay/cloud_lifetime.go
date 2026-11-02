package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	"time"
)

// The Lua cloud callback looks up SproutPoison on the target before requesting
// it. All clouds sustain that first modifier, including another caster's cloud.
// registry.mutex protects this world and its target-owned poison state.
type campaignCloudPoisonWorld struct {
	regions        map[*campaignCloudLobRun]campaignCloudPoisonRegion
	targets        map[uint32]*campaignCloudPoisonTarget
	nextOrder      uint64
	timer          *time.Timer
	revision       uint64
	isPublishing   bool
	startedAt      time.Time
	startTimestamp uint64
	effect         *zoneeffect.Inventory
}

type campaignCloudPoisonRegion struct {
	schedule      campaignCloudLobSchedule
	expiresAt     time.Time
	deleteAt      time.Time
	isDeactivated bool
	isPublished   bool
	order         uint64
	occupants     map[uint32]struct{}
}

type campaignCloudPoisonTarget struct {
	region           campaignCloudPoisonRegion
	creature         game.GameplayCreature
	nextTickAt       time.Time
	outsideWaitCount uint32
	instanceID       uint32
	isCreated        bool
	startTimestamp   uint64
	isPending        bool
}

type campaignCloudPoisonPublication struct {
	sessionKey  string
	generation  uint64
	sourceID    uint32
	binding     game.GameplayBinding
	results     []zoneability.AreaResult
	transitions []campaignDamageTransition
	entries     [][]byte
}

type campaignCloudPoisonPoll struct {
	schedule campaignCloudLobSchedule
	npc      *zonenpc.Session
	world    *campaignCloudPoisonWorld
	revision uint64
}

func (e campaignCloudPoisonPoll) execute() {
	registry := e.schedule.runtime.registry
	registry.mutex.Lock()
	if registry.cloudPoisons[e.npc] != e.world || e.world.revision != e.revision {
		registry.mutex.Unlock()
		return
	}
	if registry.isClosed {
		e.schedule.retireCloudWorldLocked(e.npc, e.world)
		delete(registry.cloudPoisons, e.npc)
		registry.mutex.Unlock()
		return
	}
	sessionKey, driver := registry.cloudPublicationDriverLocked(e.npc, e.schedule.binding.UserID)
	if sessionKey == "" || !driver.zone.IsActive() {
		e.schedule.retireCloudWorldLocked(e.npc, e.world)
		delete(registry.cloudPoisons, e.npc)
		registry.mutex.Unlock()
		return
	}
	schedule := e.schedule
	schedule.sessionKey = sessionKey
	schedule.generation = driver.generation
	driverPacket, packetErr := cloudSchedulePacket(sessionKey, driver)
	if packetErr != nil {
		e.schedule.retireCloudWorldLocked(e.npc, e.world)
		delete(registry.cloudPoisons, e.npc)
		registry.mutex.Unlock()
		e.schedule.runtime.logger.Printf("Sprout publication driver: %v", packetErr)
		return
	}
	schedule.packet = driverPacket
	at := schedule.runtime.now()
	regions := e.world.liveRegions(at, nil)
	publications, resolveErr := schedule.resolveCloudPoisonsLocked(e.world, regions, at)
	deletes, deleteErr := schedule.deleteCloudObjectsLocked(e.world, at)
	// Serialize authoritative ticks with their durable publication. A landing
	// may join this world while feedback is prepared outside the registry lock.
	e.world.isPublishing = true
	e.world.timer = nil
	e.world.revision++
	registry.mutex.Unlock()
	if resolveErr != nil {
		schedule.runtime.logger.Printf("Sprout poison world damage: %v", resolveErr)
	}
	if deleteErr != nil {
		schedule.runtime.logger.Printf("Sprout poison world cleanup: %v", deleteErr)
	}
	timestamp := e.world.startTimestamp + uint64(max(time.Duration(0), at.Sub(e.world.startedAt))/time.Millisecond)
	schedule.packet.SourceTime = timestamp
	packets, publishErr := schedule.publishCloudPoisons(publications, timestamp)
	if publishErr != nil {
		schedule.runtime.logger.Printf("Sprout poison world publication: %v", publishErr)
	}
	packets = append(packets, deletes...)
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	for key, peerSession := range registry.sessions {
		if peerSession.zone == nil || peerSession.zone.NPCs() != e.npc {
			continue
		}
		peerSession.queueCampaignPackets(packets)
		registry.sessions[key] = peerSession
	}
	e.world.isPublishing = false
	if registry.cloudPoisons[e.npc] == e.world && !registry.isClosed {
		e.world.scheduleNextLocked(schedule, e.npc, schedule.runtime.now())
	}
}

func (e *campaignCloudPoisonWorld) scheduleNextLocked(schedule campaignCloudLobSchedule, npc *zonenpc.Session, at time.Time) {
	if e.isPublishing {
		return
	}
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.revision++
	if len(e.regions) == 0 && len(e.targets) == 0 {
		delete(schedule.runtime.registry.cloudPoisons, npc)
		return
	}
	delay := 500 * time.Millisecond
	isPublished := false
	for _, target := range e.targets {
		if target.isPending {
			continue
		}
		isPublished = true
		delay = min(delay, max(time.Duration(0), target.nextTickAt.Sub(at)))
	}
	for _, region := range e.regions {
		if !region.isPublished {
			continue
		}
		isPublished = true
		if at.Before(region.expiresAt) {
			delay = min(delay, region.expiresAt.Sub(at))
		}
		delay = min(delay, max(time.Duration(0), region.deleteAt.Sub(at)))
	}
	if !isPublished {
		// A pending creation batch arms its world only after durable enqueue.
		return
	}
	// World callbacks resolve a fresh current driver when they run; retain no
	// original request payload, response collector or command cancellation.
	schedule.packet = gameplaySchedulePacket(schedule.packet)
	step := campaignCloudPoisonPoll{schedule: schedule, npc: npc, world: e, revision: e.revision}
	e.timer = time.AfterFunc(delay, step.execute)
}

func (e campaignCloudLobSchedule) publishCloudLanding(packets [][]byte) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	for npc, world := range e.runtime.registry.cloudPoisons {
		region, isFound := world.regions[e.run]
		if !isFound {
			continue
		}
		for key, peerSession := range e.runtime.registry.sessions {
			if peerSession.zone == nil || peerSession.zone.NPCs() != npc {
				continue
			}
			peerSession.queueCampaignPackets(packets)
			e.runtime.registry.sessions[key] = peerSession
		}
		region.isPublished = true
		world.regions[e.run] = region
		for _, target := range world.targets {
			if target.region.schedule.run == e.run {
				target.isPending = false
			}
		}
		world.scheduleNextLocked(e, npc, e.runtime.now())
		return
	}
}
