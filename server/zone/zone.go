package zone

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zonebarrier "github.com/darkspinnet/darkspin/server/zone/barrier"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonedeath "github.com/darkspinnet/darkspin/server/zone/death"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zoneencounter "github.com/darkspinnet/darkspin/server/zone/encounter"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zoneobjectid "github.com/darkspinnet/darkspin/server/zone/objectid"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
	zoneoutcome "github.com/darkspinnet/darkspin/server/zone/outcome"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	zonetimeline "github.com/darkspinnet/darkspin/server/zone/timeline"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

type State uint8

const (
	StateActive State = iota + 1
	StateComplete
)

type ZoneInfo struct {
	Level               string
	Difficulty          uint32
	ChainLevelIndex     uint32
	MemberLimit         uint16
	DirectorDefinition  game.CampaignDirector
	Navigation          *navigation.Mesh
	HordeBarrierPlans   map[string][]zonebarrier.Plan
	ScriptObjects       []game.CampaignScriptObject
	ScriptObjectPlans   []zoneobject.ScriptPlan
	InitialNPCPlans     []zonenpc.SpawnPlan
	FixturePlans        []zonenpc.SpawnPlan
	CatalystProgram     sim.Program
	OverdriveProgram    sim.Program
	CrystalDefinitions  []sim.CrystalDefinition
	CrystalLevelOffsets []sim.CrystalLevelOffset
	Security            *zonesecurity.Session
	Effect              *zoneeffect.Inventory
	NPCs                *zonenpc.Session
	Hero                *zonehero.Session
	Companion           *zonecompanion.Session
	Interactable        *zoneinteract.UseSession
	Pickups             *zoneinteract.PickupRegistry
	PickupPayload       *zoneinteract.PickupPayloadRegistry
	Orbs                *zoneinteract.OrbRegistry
	Loot                *zoneloot.Session
	DNA                 *zoneloot.DNASession
	Population          *zonepopulation.Session
	Director            *game.CampaignDirectorSession
	Route               *sim.DirectorSession
	Script              *game.CampaignScriptRegistry
	Encounter           *zoneencounter.StageSession
	Horde               *zonehorde.Session
	Boss                *zoneboss.Session
	Death               *zonedeath.Session
	Objective           *zoneobjective.Session
	ObjectiveProgress   *zoneobjective.Progress
	ObjectID            *zoneobjectid.Session
	ProjectileID        *zoneobjectid.Session
	Outcome             *zoneoutcome.Session
	Result              *zoneresult.Ledger
	ResultVote          *zoneresult.VoteSession
	Timeline            *zonetimeline.Session
	Timer               Timer
	NPCRandom           *sim.SimulatorRandom
	DropRandom          *sim.SimulatorRandom
	Checkpoint          zonecheckpoint.Repository
	Restore             *zonecheckpoint.Snapshot
}

type Member struct {
	UserID             uint64
	PeerGeneration     uint64
	Slot               uint16
	AbilityCount       uint32
	IsReplay           bool
	Roster             game.GameplayRoster
	CreatureFootprints [squad.Size]float32
	IsConnected        bool
}

// IsMemberReplay reports the per-player build-103 HasBeatenThisLevel result.
// Co-op members may differ because chain progression belongs to the account.
func (e *Zone) IsMemberReplay(userID uint64) (bool, bool) {
	if e == nil || userID == 0 {
		return false, false
	}
	e.mu.RLock()
	member, isFound := e.members[userID]
	e.mu.RUnlock()
	return member.IsReplay, isFound
}

type Snapshot struct {
	ID                   uint64
	Generation           uint64
	CompletionID         uint64
	State                State
	Level                string
	Difficulty           uint32
	ChainLevelIndex      uint32
	MemberLimit          uint16
	Members              []Member
	ClearedSpawnGroupIDs []uint32
	IsPopulationPrimed   bool
	IsRestored           bool
	IsObjectiveComplete  bool
}

type HeroDamage struct {
	Previous zonehero.Actor
	Current  zonehero.Actor
}

type objectiveActor struct {
	UserID             uint64
	PlayerIndex        uint8
	IsPlayerControlled bool
	IsFound            bool
}

type NPCTarget struct {
	UserID          uint64
	PeerGeneration  uint64
	ObjectID        uint32
	Position        game.Vec3
	LinearVelocity  game.Vec3
	FootprintRadius float32
	HitPoint        float32
	ManaPoint       float32
	IsHero          bool
}

type NPCTargetDamage struct {
	PreviousHitPoint float32
	HitPoint         float32
	IsHero           bool
	IsDefeated       bool
}

// Zone owns the state shared by every peer participating in one campaign
// run. Connection-local presentation and transport state remain outside it.
type Zone struct {
	mu                  sync.RWMutex
	ctx                 context.Context
	cancel              context.CancelFunc
	registry            *Registry
	id                  uint64
	generation          uint64
	completionID        uint64
	state               State
	startedAt           time.Time
	info                ZoneInfo
	members             map[uint64]Member
	crystals            map[uint64]sim.CrystalInventory
	unlocks             map[uint64]*zoneunlock.Session
	experienceAwards    map[uint32]ExperienceAward
	experienceTotals    map[uint64]uint32
	experienceCommits   map[uint64]ExperienceCommit
	projection          *zoneprojection.Session
	clearedSpawnGroups  map[uint32]struct{}
	checkpointHeroes    map[uint64]zonecheckpoint.Hero
	checkpointSquads    map[uint64]zonecheckpoint.Squad
	isPopulationPrimed  bool
	isRestored          bool
	isObjectiveComplete bool
}

const npcReturnIdleDelay = 10 * time.Second

type npcReturnStep struct {
	zone     *Zone
	objectID uint32
}

func (e npcReturnStep) execute() {
	e.zone.completeNPCReturn(e.objectID)
}

func New(id uint64, generation uint64, info ZoneInfo) (*Zone, error) {
	if id == 0 || generation == 0 {
		return nil, errors.New("campaign zone identity invalid")
	}
	if info.NPCs == nil || info.Hero == nil || info.Companion == nil ||
		info.Interactable == nil ||
		info.Pickups == nil || info.PickupPayload == nil ||
		info.Orbs == nil || info.Loot == nil ||
		info.DNA == nil ||
		info.Population == nil || info.Director == nil ||
		info.Script == nil || info.Encounter == nil ||
		info.Horde == nil || info.Boss == nil ||
		info.Death == nil || info.Objective == nil || info.ObjectID == nil ||
		info.ProjectileID == nil || info.Outcome == nil || info.Result == nil ||
		info.Timeline == nil || info.ResultVote == nil || info.Timer == nil ||
		info.NPCRandom == nil || info.DropRandom == nil ||
		info.Security == nil || info.Effect == nil {
		return nil, errors.New("zone info incomplete")
	}
	completionID := uint64(0)
	if info.Restore != nil {
		completionID = info.Restore.CompletionID
	} else {
		var err error
		completionID, err = newCompletionID()
		if err != nil {
			return nil, fmt.Errorf("zoneCompletionID: %w", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	zone := &Zone{
		ctx: ctx, cancel: cancel,
		id: id, generation: generation, completionID: completionID,
		state: StateActive, startedAt: time.Now(),
		info: info, members: make(map[uint64]Member),
		crystals:           make(map[uint64]sim.CrystalInventory),
		unlocks:            make(map[uint64]*zoneunlock.Session),
		experienceAwards:   make(map[uint32]ExperienceAward),
		experienceTotals:   make(map[uint64]uint32),
		experienceCommits:  make(map[uint64]ExperienceCommit),
		projection:         zoneprojection.NewSession(),
		clearedSpawnGroups: make(map[uint32]struct{}),
		checkpointHeroes:   make(map[uint64]zonecheckpoint.Hero),
		checkpointSquads:   make(map[uint64]zonecheckpoint.Squad),
	}
	if info.Restore != nil {
		err := zone.restoreCheckpoint(*info.Restore)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("zoneRestore: %w", err)
		}
	}
	return zone, nil
}

func newCompletionID() (uint64, error) {
	var completionBytes [8]byte
	_, err := cryptorand.Read(completionBytes[:])
	if err != nil {
		return 0, fmt.Errorf("random: %w", err)
	}
	completionID := binary.LittleEndian.Uint64(completionBytes[:]) & uint64(math.MaxInt64)
	if completionID == 0 {
		completionID = 1
	}
	return completionID, nil
}

// CompletionID is the durable idempotency identity for this zone's terminal
// account mutations. Checkpoints preserve it across process restarts.
func (e *Zone) CompletionID() uint64 {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	completionID := e.completionID
	e.mu.RUnlock()
	return completionID
}

// Elapsed returns the authoritative time spent in this campaign instance.
// Restored zones retain their checkpoint elapsed time by moving startedAt
// backwards during restore.
func (e *Zone) Elapsed(now time.Time) time.Duration {
	if e == nil || now.IsZero() {
		return 0
	}
	e.mu.RLock()
	startedAt := e.startedAt
	e.mu.RUnlock()
	if startedAt.IsZero() || now.Before(startedAt) {
		return 0
	}
	return now.Sub(startedAt)
}

// CompleteObjectives evaluates the selected packaged objective callbacks once
// for the shared zone and publishes the resulting medal state to every member.
func (e *Zone) CompleteObjectives(ctx context.Context, now time.Time) error {
	if e == nil || e.info.Objective == nil {
		return nil
	}
	if now.IsZero() {
		return errors.New("objective completion time unavailable")
	}
	e.mu.Lock()
	if e.isObjectiveComplete {
		e.mu.Unlock()
		return nil
	}
	completionTime := now.Sub(e.startedAt)
	if completionTime < 0 {
		completionTime = 0
	}
	killPercent := float64(0)
	if e.info.ObjectiveProgress != nil {
		killPercent = e.info.ObjectiveProgress.KillPercent()
	}
	var fullClearUpdates []zoneobjective.Update
	var fullClearErr error
	if killPercent == 1 {
		fullClearUpdates, fullClearErr = e.info.Objective.ApplyEvent(
			ctx, sim.LuaObjectiveEvent{
				Kind: sim.LuaObjectiveEventFullClear, KillPercent: 1,
			},
		)
	}
	updates, completionErr := e.info.Objective.Complete(
		ctx, completionTime, killPercent,
	)
	e.isObjectiveComplete = true
	e.mu.Unlock()
	e.PublishObjective(fullClearUpdates)
	e.PublishObjective(updates)
	var objectiveErrors []error
	if fullClearErr != nil {
		objectiveErrors = append(objectiveErrors, fmt.Errorf(
			"objectiveFullClear: %w", fullClearErr,
		))
	}
	if completionErr != nil {
		objectiveErrors = append(objectiveErrors, fmt.Errorf(
			"objectiveComplete: %w", completionErr,
		))
	}
	return errors.Join(objectiveErrors...)
}

func (e *Zone) Join(member Member) error {
	if e == nil {
		return errors.New("nil campaign zone")
	}
	if member.UserID == 0 || member.PeerGeneration == 0 {
		return errors.New("campaign member identity invalid")
	}
	if member.Slot >= game.MaxGamePlayers {
		return errors.New("campaign member objective slot out of range")
	}
	member.IsConnected = true
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return errors.New("campaign zone not active")
	}
	current, isFound := e.members[member.UserID]
	isRestoredPlaceholder := isFound && current.PeerGeneration == 0
	if isFound && current.PeerGeneration != 0 &&
		member.PeerGeneration < current.PeerGeneration {
		e.mu.Unlock()
		return fmt.Errorf(
			"campaign member generation stale: got %d, want >= %d",
			member.PeerGeneration, current.PeerGeneration,
		)
	}
	if !isFound && e.info.MemberLimit != 0 &&
		len(e.members) >= int(e.info.MemberLimit) {
		e.mu.Unlock()
		return errors.New("campaign zone member capacity reached")
	}
	if !isFound {
		for _, existing := range e.members {
			if existing.Slot == member.Slot {
				e.mu.Unlock()
				return fmt.Errorf("campaign member slot %d already reserved", member.Slot)
			}
		}
	}
	err := e.projection.Join(zoneprojection.Subscriber{
		UserID: member.UserID, PeerGeneration: member.PeerGeneration,
	})
	if err != nil {
		e.mu.Unlock()
		return fmt.Errorf("campaignProjectionJoin: %w", err)
	}
	err = e.info.Outcome.Join(zoneoutcome.Member{
		UserID: member.UserID, PeerGeneration: member.PeerGeneration,
	})
	if err != nil {
		e.projection.Leave(zoneprojection.Subscriber{
			UserID: member.UserID, PeerGeneration: member.PeerGeneration,
		})
		e.mu.Unlock()
		return fmt.Errorf("campaignOutcomeJoin: %w", err)
	}
	err = e.info.ResultVote.Join(zoneresult.Voter{
		UserID: member.UserID, PeerGeneration: member.PeerGeneration,
	})
	if err != nil {
		e.info.Outcome.Leave(zoneoutcome.Member{
			UserID: member.UserID, PeerGeneration: member.PeerGeneration,
		})
		if isRestoredPlaceholder {
			_ = e.info.Outcome.RestoreMember(member.UserID)
		}
		e.projection.Leave(zoneprojection.Subscriber{
			UserID: member.UserID, PeerGeneration: member.PeerGeneration,
		})
		e.mu.Unlock()
		return fmt.Errorf("campaignResultVoteJoin: %w", err)
	}
	if !isFound && e.info.Objective != nil {
		err = e.info.Objective.Join(uint8(member.Slot))
		if err != nil {
			e.info.ResultVote.Leave(zoneresult.Voter{
				UserID: member.UserID, PeerGeneration: member.PeerGeneration,
			})
			e.info.Outcome.Leave(zoneoutcome.Member{
				UserID: member.UserID, PeerGeneration: member.PeerGeneration,
			})
			e.projection.Leave(zoneprojection.Subscriber{
				UserID: member.UserID, PeerGeneration: member.PeerGeneration,
			})
			e.mu.Unlock()
			return fmt.Errorf("campaignObjectiveJoin: %w", err)
		}
	}
	releasedAction := make([]zonenpc.Snapshot, 0)
	if isFound && member.PeerGeneration > current.PeerGeneration {
		e.info.Hero.Remove(current.UserID, current.PeerGeneration)
		e.info.Companion.RemoveOwner(current.UserID, current.PeerGeneration)
		e.info.Result.Rebind(zoneresult.Voter{
			UserID: member.UserID, PeerGeneration: member.PeerGeneration,
		})
		releasedAction = e.info.NPCs.ReleaseActions(zonenpc.ActionOwner{
			UserID: current.UserID, PeerGeneration: current.PeerGeneration,
		})
	}
	e.members[member.UserID] = member
	if _, isFound := e.crystals[member.UserID]; !isFound {
		e.crystals[member.UserID] = sim.CrystalInventory{}
	}
	if _, isFound := e.unlocks[member.UserID]; !isFound {
		e.unlocks[member.UserID] = zoneunlock.NewSession(member.AbilityCount)
	}
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, enemy := range releasedAction {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(enemy.Plan.ObjectID))
	}
	return nil
}

// UpdateMemberRoster refreshes mutable hero resources without changing zone
// membership or transport presence.
func (e *Zone) UpdateMemberRoster(
	userID uint64, peerGeneration uint64, roster game.GameplayRoster,
) error {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return errors.New("campaign roster member invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		return errors.New("campaign roster member unavailable")
	}
	member.Roster = roster
	e.members[userID] = member
	return nil
}

func (e *Zone) PublishHeroRoster(
	userID uint64, peerGeneration uint64, actor zonehero.Actor,
) error {
	if e == nil {
		return errors.New("nil zone")
	}
	e.mu.RLock()
	member, isFound := e.members[userID]
	e.mu.RUnlock()
	if !isFound || member.PeerGeneration != peerGeneration {
		return errors.New("campaign roster member unavailable")
	}
	e.projection.PublishHeroRoster(zoneprojection.HeroRoster{
		UserID: userID, PlayerSlot: member.Slot, Roster: member.Roster,
		Position: actor.Position, CreatureIndex: actor.CreatureIndex,
	}, zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	return nil
}

func (e *Zone) PublishHeroDeploy(
	deploy zoneprojection.HeroDeploy,
	sourceUserID uint64, sourceGeneration uint64,
) {
	if e == nil {
		return
	}
	e.projection.PublishHeroDeploy(deploy, zoneprojection.Subscriber{
		UserID: sourceUserID, PeerGeneration: sourceGeneration,
	})
}

// Disconnect detaches transport presence while retaining semantic membership,
// the hero, companions, loot, objectives, and result state for rejoin.
func (e *Zone) Disconnect(
	userID uint64, peerGeneration uint64,
) ([]zonenpc.Snapshot, bool) {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return nil, false
	}
	e.mu.Lock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration ||
		!member.IsConnected {
		e.mu.Unlock()
		return nil, false
	}
	member.IsConnected = false
	e.members[userID] = member
	released := e.info.NPCs.ReleaseActions(zonenpc.ActionOwner{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	if hero, isHeroFound := e.info.Hero.Snapshot(userID, peerGeneration); isHeroFound {
		targetReleased, err := e.info.NPCs.ReleaseTarget(hero.ObjectID)
		if err == nil {
			released = append(released, targetReleased...)
		}
	}
	for _, companion := range e.info.Companion.Snapshots() {
		if companion.UserID != userID ||
			companion.PeerGeneration != peerGeneration {
			continue
		}
		targetReleased, err := e.info.NPCs.ReleaseTarget(companion.ObjectID)
		if err == nil {
			released = append(released, targetReleased...)
		}
	}
	acquired, err := e.info.NPCs.AcquireTargets(
		e.livePlayerAlignedTargets(), true,
	)
	if err != nil {
		// Disconnect is already committed; reacquiring replacement targets is
		// best-effort because this boundary cannot roll back transport state.
		acquired = nil
	}
	returning := e.returnUnacquiredNPCs(released, acquired)
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range released {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
	}
	e.publishNPCReturns(returning)
	return acquired, true
}

// CommitReconnect restores transport presence and advances the member's
// projection cursor through the baseline that was successfully published.
// The peer generation deliberately remains stable; transport generations are
// owned by the gameplay adapter.
func (e *Zone) CommitReconnect(
	userID uint64, peerGeneration uint64, baselineRevision uint64,
) error {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return errors.New("campaign reconnect member invalid")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return errors.New("campaign reconnect member unavailable")
	}
	isReset := e.projection.ResetThrough(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	}, baselineRevision)
	if !isReset {
		e.mu.Unlock()
		return errors.New("campaign reconnect projection unavailable")
	}
	member.IsConnected = true
	e.members[userID] = member
	e.mu.Unlock()
	return nil
}

// ValidateReconnect verifies retained semantic membership without exposing the
// disconnected hero to targeting before its transport baseline commits.
func (e *Zone) ValidateReconnect(
	userID uint64, peerGeneration uint64,
) error {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return errors.New("campaign reconnect member invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		return errors.New("campaign reconnect member unavailable")
	}
	if member.IsConnected {
		return errors.New("campaign reconnect member already connected")
	}
	return nil
}

func (e *Zone) Leave(userID uint64, peerGeneration uint64) bool {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return false
	}
	e.mu.Lock()
	current, isFound := e.members[userID]
	if !isFound || current.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return false
	}
	delete(e.members, userID)
	delete(e.crystals, userID)
	delete(e.unlocks, userID)
	delete(e.checkpointHeroes, userID)
	delete(e.checkpointSquads, userID)
	delete(e.experienceTotals, userID)
	delete(e.experienceCommits, userID)
	if e.info.Objective != nil {
		_ = e.info.Objective.Leave(uint8(current.Slot))
	}
	e.projection.Leave(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	e.info.Outcome.Leave(zoneoutcome.Member{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	e.info.ResultVote.Leave(zoneresult.Voter{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	e.info.Result.Remove(zoneresult.Voter{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	hero, isHeroFound := e.info.Hero.Snapshot(userID, peerGeneration)
	e.info.Hero.Remove(userID, peerGeneration)
	companion := e.info.Companion.RemoveOwner(userID, peerGeneration)
	removedObjectIDs := make([]uint32, 0, squad.Size+len(companion))
	for creatureIndex := uint32(0); creatureIndex < squad.Size; creatureIndex++ {
		removedObjectIDs = append(
			removedObjectIDs, zonehero.ObjectID(current.Slot, creatureIndex),
		)
	}
	for _, actor := range companion {
		removedObjectIDs = append(removedObjectIDs, actor.ObjectID)
	}
	e.projection.PublishHeroLeave(zoneprojection.HeroLeave{
		ObjectIDs: removedObjectIDs,
	})
	releasedTarget := make([]zonenpc.Snapshot, 0)
	if isHeroFound {
		released, err := e.info.NPCs.ReleaseTarget(hero.ObjectID)
		if err == nil {
			releasedTarget = append(releasedTarget, released...)
		}
	}
	for _, actor := range companion {
		released, err := e.info.NPCs.ReleaseTarget(actor.ObjectID)
		if err == nil {
			releasedTarget = append(releasedTarget, released...)
		}
	}
	releasedAction := e.info.NPCs.ReleaseActions(zonenpc.ActionOwner{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	releasedTarget = append(releasedTarget, releasedAction...)
	acquired, err := e.info.NPCs.AcquireTargets(
		e.livePlayerAlignedTargets(), true,
	)
	if err != nil {
		// Leave is already committed; reacquiring replacement targets is
		// best-effort because this boundary cannot restore removed membership.
		acquired = nil
	}
	returning := e.returnUnacquiredNPCs(releasedTarget, acquired)
	isEmpty := len(e.members) == 0
	isAllCommitted := e.info.Outcome.AreAllCommitted()
	if isAllCommitted {
		e.state = StateComplete
	}
	death := e.info.Death
	timeline := e.info.Timeline
	registry := e.registry
	e.mu.Unlock()
	for _, enemy := range releasedAction {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(enemy.Plan.ObjectID))
	}
	for _, npc := range releasedTarget {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
	}
	e.publishNPCReturns(returning)
	if isEmpty {
		e.cancel()
		death.Stop()
		timeline.Stop()
	}
	if isEmpty && registry != nil {
		registry.retire(e)
	}
	return true
}

// Context is canceled when the last member leaves this zone. Delayed feature
// work must use it instead of a process-lifetime background context.
func (e *Zone) Context() context.Context {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ctx
}

func (e *Zone) CrystalInventory(
	userID uint64,
	peerGeneration uint64,
) (sim.CrystalInventory, bool) {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return sim.CrystalInventory{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		return sim.CrystalInventory{}, false
	}
	inventory, isFound := e.crystals[userID]
	return inventory, isFound
}

func (e *Zone) SetCrystalInventory(
	userID uint64,
	peerGeneration uint64,
	inventory sim.CrystalInventory,
) error {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return errors.New("crystal inventory member invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		return errors.New("crystal inventory member unavailable")
	}
	e.crystals[userID] = inventory
	return nil
}

func (e *Zone) Complete() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return false
	}
	e.state = StateComplete
	checkpoint := e.info.Checkpoint
	zoneID := e.id
	e.mu.Unlock()
	if checkpoint != nil {
		checkpoint.Discard(zoneID)
	}
	return true
}

// Discard retires a live zone and every delayed gameplay timeline without
// requiring its retained members to reconnect and leave first.
func (e *Zone) Discard() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return false
	}
	e.state = StateComplete
	checkpoint := e.info.Checkpoint
	zoneID := e.id
	cancel := e.cancel
	death := e.info.Death
	timeline := e.info.Timeline
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if death != nil {
		death.Stop()
	}
	if timeline != nil {
		timeline.Stop()
	}
	if checkpoint != nil {
		checkpoint.Discard(zoneID)
	}
	return true
}

func (e *Zone) AdmitHordeWave(
	targetObjectID uint32, markerSetName string,
	plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return errors.New("campaign zone not active")
	}
	err := zoneencounter.AdmitHordeWave(
		e.info.NPCs, e.info.Horde,
		targetObjectID, markerSetName, plans,
	)
	if err != nil {
		return fmt.Errorf("instanceHordeAdmit: %w", err)
	}
	return nil
}

func (e *Zone) AdmitBossSecondWave(
	targetObjectID uint32, plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return errors.New("campaign zone not active")
	}
	err := zoneencounter.AdmitBossSecondWave(
		e.info.NPCs, e.info.Boss, targetObjectID, plans,
		e.info.Difficulty, max(uint32(1), uint32(len(e.members))),
	)
	if err != nil {
		return fmt.Errorf("instanceBossAdmit: %w", err)
	}
	return nil
}

func (e *Zone) AdmitBoss(
	targetObjectID uint32, plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return errors.New("campaign zone not active")
	}
	err := zoneencounter.AdmitBoss(
		e.info.NPCs, e.info.Boss, targetObjectID, plans,
	)
	if err != nil {
		return fmt.Errorf("instanceBossAdmit: %w", err)
	}
	return nil
}

func (e *Zone) ObserveNPCDamage(
	result zonenpc.DamageResult,
) (zoneencounter.DamageTransition, error) {
	if e == nil {
		return zoneencounter.DamageTransition{},
			errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return zoneencounter.DamageTransition{},
			errors.New("campaign zone not active")
	}
	transition, err := zoneencounter.ObserveDamage(
		e.info.Horde, e.info.Boss, result,
	)
	if err != nil {
		return zoneencounter.DamageTransition{},
			fmt.Errorf("instanceDamageObserve: %w", err)
	}
	return transition, nil
}

func (e *Zone) SetHeroResources(
	userID uint64,
	peerGeneration uint64,
	objectID uint32,
	hitPoint float32,
	manaPoint float32,
) (zonehero.Actor, error) {
	if e == nil {
		return zonehero.Actor{}, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return zonehero.Actor{}, errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration ||
		member.Slot > 255 {
		return zonehero.Actor{}, errors.New("campaign member unavailable")
	}
	previous, err := e.info.Hero.SetResources(
		userID, peerGeneration, objectID, hitPoint, manaPoint,
	)
	if err != nil {
		return zonehero.Actor{}, fmt.Errorf("instanceHeroResource: %w", err)
	}
	actor, isFound := e.info.Hero.Snapshot(userID, peerGeneration)
	if !isFound {
		return zonehero.Actor{}, errors.New("campaign hero unavailable")
	}
	e.publishHeroResource(member, actor)
	return previous, nil
}

func (e *Zone) ReleaseHeroTarget(
	userID uint64, peerGeneration uint64, objectID uint32,
) ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return nil, errors.New("campaign member unavailable")
	}
	actor, isFound := e.info.Hero.Snapshot(userID, peerGeneration)
	if !isFound || actor.ObjectID != objectID || actor.HitPoint > 0 {
		e.mu.Unlock()
		return nil, errors.New("campaign defeated hero unavailable")
	}
	released, err := e.info.NPCs.ReleaseTarget(objectID)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceHeroTargetRelease: %w", err)
	}
	acquired, err := e.info.NPCs.AcquireTargets(e.livePlayerAlignedTargets(), false)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceHeroTargetAcquire: %w", err)
	}
	returning := e.returnUnacquiredNPCs(released, acquired)
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range released {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
	}
	e.publishNPCReturns(returning)
	return acquired, nil
}

func (e *Zone) SwitchHeroTarget(
	userID uint64, peerGeneration uint64,
	sourceObjectID uint32, targetObjectID uint32, timestamp uint64,
) ([]zonenpc.Snapshot, []zonenpc.Snapshot, error) {
	if e == nil {
		return nil, nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, nil, errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return nil, nil, errors.New("campaign member unavailable")
	}
	actor, isFound := e.info.Hero.Snapshot(userID, peerGeneration)
	if !isFound || actor.ObjectID != targetObjectID || actor.HitPoint <= 0 {
		e.mu.Unlock()
		return nil, nil, errors.New("campaign replacement hero unavailable")
	}
	released, err := e.info.NPCs.ReleaseTarget(sourceObjectID)
	if err != nil {
		e.mu.Unlock()
		return nil, nil, fmt.Errorf("instanceHeroTargetRelease: %w", err)
	}
	acquired, err := e.info.NPCs.AcquireTargets(e.livePlayerAlignedTargets(), true)
	if err != nil {
		e.mu.Unlock()
		return nil, nil, fmt.Errorf("instanceHeroTargetAcquire: %w", err)
	}
	returning := e.returnUnacquiredNPCs(released, acquired)
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range released {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		if !npc.IsActionStarted {
			continue
		}
		e.PublishNPCAction(zonenpc.ActionEvent{
			Kind: zonenpc.ActionEventCancel,
			Plan: zonenpc.FirstActionPlan{
				ObjectID:         npc.Plan.ObjectID,
				ActionGeneration: npc.ActionGeneration,
				SourcePosition:   npc.Plan.Position,
			},
			Timestamp: timestamp,
		}, userID, peerGeneration)
	}
	e.publishNPCReturns(returning)
	return acquired, released, nil
}

func (e *Zone) RefreshNPCTargets() ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return nil, errors.New("campaign zone not active")
	}
	acquired, err := e.info.NPCs.AcquireTargets(
		e.livePlayerAlignedTargets(), true,
	)
	if err != nil {
		return nil, fmt.Errorf("instanceNPCTargetAcquire: %w", err)
	}
	for _, npc := range acquired {
		e.info.Timeline.Cancel(
			zonenpc.ReturnTimelineKey(npc.Plan.ObjectID),
		)
	}
	return acquired, nil
}

func (e *Zone) livePlayerAlignedTargets() []zonenpc.Target {
	actor := e.info.Hero.Snapshots()
	companion := e.info.Companion.Snapshots()
	target := make([]zonenpc.Target, 0, len(actor)+len(companion))
	for _, current := range actor {
		member, isMemberFound := e.members[current.UserID]
		if !isMemberFound || !member.IsConnected ||
			member.PeerGeneration != current.PeerGeneration {
			continue
		}
		if current.IsStealthed {
			continue
		}
		target = append(target, zonenpc.Target{
			ObjectID: current.ObjectID, Position: current.Position,
			FootprintRadius: current.FootprintRadius,
			Faction:         zonenpc.FactionPlayerAligned,
			Owner: zonenpc.ActionOwner{
				UserID: current.UserID, PeerGeneration: current.PeerGeneration,
			},
			IsAlive: current.HitPoint > 0,
		})
	}
	for _, current := range companion {
		member, isMemberFound := e.members[current.UserID]
		if !isMemberFound || !member.IsConnected ||
			member.PeerGeneration != current.PeerGeneration {
			continue
		}
		if !current.IsTargetable {
			continue
		}
		target = append(target, zonenpc.Target{
			ObjectID: current.ObjectID, Position: current.Position,
			FootprintRadius: current.FootprintRadius,
			Faction:         zonenpc.FactionPlayerAligned,
			Owner: zonenpc.ActionOwner{
				UserID: current.UserID, PeerGeneration: current.PeerGeneration,
			},
			IsAlive: current.HitPoint > 0,
		})
	}
	return target
}

func (e *Zone) RestartHeroTarget(
	userID uint64, peerGeneration uint64, objectID uint32,
) ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return nil, errors.New("campaign member unavailable")
	}
	actor, isFound := e.info.Hero.Snapshot(userID, peerGeneration)
	if !isFound || actor.ObjectID != objectID || actor.HitPoint <= 0 {
		e.mu.Unlock()
		return nil, errors.New("campaign active hero unavailable")
	}
	owner := zonenpc.ActionOwner{
		UserID: userID, PeerGeneration: peerGeneration,
	}
	restarted := e.info.NPCs.ReleaseActions(owner)
	targetRestarted, err := e.info.NPCs.RestartTarget(objectID)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceHeroTargetRestart: %w", err)
	}
	restartedObject := make(map[uint32]struct{}, len(restarted))
	for _, npc := range restarted {
		restartedObject[npc.Plan.ObjectID] = struct{}{}
	}
	for _, npc := range targetRestarted {
		if _, isFound := restartedObject[npc.Plan.ObjectID]; isFound {
			continue
		}
		restarted = append(restarted, npc)
	}
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range restarted {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
	}
	return restarted, nil
}

func (e *Zone) ReleaseHeroActions(
	userID uint64, peerGeneration uint64,
) []zonenpc.Snapshot {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return nil
	}
	e.mu.Lock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return nil
	}
	released := e.info.NPCs.ReleaseActions(zonenpc.ActionOwner{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range released {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
	}
	return released
}

func (e *Zone) NPCTarget(objectID uint32) (NPCTarget, bool) {
	if e == nil || objectID == 0 {
		return NPCTarget{}, false
	}
	hero, isHeroFound := e.info.Hero.SnapshotByObjectID(objectID)
	if isHeroFound && hero.HitPoint > 0 && !hero.IsStealthed {
		return NPCTarget{
			UserID: hero.UserID, PeerGeneration: hero.PeerGeneration,
			ObjectID: hero.ObjectID, Position: hero.Position,
			LinearVelocity:  hero.LinearVelocity,
			FootprintRadius: hero.FootprintRadius,
			HitPoint:        hero.HitPoint, ManaPoint: hero.ManaPoint,
			IsHero: true,
		}, true
	}
	companion, isCompanionFound := e.info.Companion.Snapshot(objectID)
	if !isCompanionFound || !companion.IsTargetable || companion.HitPoint <= 0 {
		return NPCTarget{}, false
	}
	return NPCTarget{
		UserID: companion.UserID, PeerGeneration: companion.PeerGeneration,
		ObjectID: companion.ObjectID, Position: companion.Position,
		FootprintRadius: companion.FootprintRadius,
		HitPoint:        companion.HitPoint,
	}, true
}

func (e *Zone) SetHeroStealthed(
	userID uint64, peerGeneration uint64, objectID uint32, isStealthed bool,
) ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		e.mu.Unlock()
		return nil, errors.New("campaign member unavailable")
	}
	err := e.info.Hero.SetStealthed(
		userID, peerGeneration, objectID, isStealthed,
	)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceHeroStealth: %w", err)
	}
	released := make([]zonenpc.Snapshot, 0)
	if isStealthed {
		released, err = e.info.NPCs.ReleaseTarget(objectID)
		if err != nil {
			e.mu.Unlock()
			return nil, fmt.Errorf("instanceStealthTargetRelease: %w", err)
		}
	}
	acquired, err := e.info.NPCs.AcquireTargets(
		e.livePlayerAlignedTargets(), true,
	)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceStealthTargetAcquire: %w", err)
	}
	returning := e.returnUnacquiredNPCs(released, acquired)
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range released {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
	}
	for _, npc := range acquired {
		timeline.Cancel(zonenpc.ReturnTimelineKey(npc.Plan.ObjectID))
	}
	e.publishNPCReturns(returning)
	return acquired, nil
}

func (e *Zone) LiveNPCTargets() []NPCTarget {
	if e == nil {
		return nil
	}
	target := e.livePlayerAlignedTargets()
	result := make([]NPCTarget, 0, len(target))
	for _, current := range target {
		resolved, isFound := e.NPCTarget(current.ObjectID)
		if isFound {
			result = append(result, resolved)
		}
	}
	return result
}

func (e *Zone) TauntCompanion(
	userID uint64, peerGeneration uint64, objectID uint32, radius float32,
) ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	companion, isFound := e.info.Companion.Snapshot(objectID)
	if !isFound || companion.UserID != userID ||
		companion.PeerGeneration != peerGeneration || companion.HitPoint <= 0 {
		e.mu.Unlock()
		return nil, errors.New("campaign companion unavailable")
	}
	acquired, err := e.info.NPCs.ForceTargetInRadius(zonenpc.Target{
		ObjectID: companion.ObjectID, Position: companion.Position,
		FootprintRadius: companion.FootprintRadius,
		Faction:         zonenpc.FactionPlayerAligned,
		Owner: zonenpc.ActionOwner{
			UserID: userID, PeerGeneration: peerGeneration,
		},
		IsAlive: true,
	}, radius)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceCompanionTaunt: %w", err)
	}
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range acquired {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		timeline.Cancel(zonenpc.ReturnTimelineKey(npc.Plan.ObjectID))
	}
	return acquired, nil
}

func (e *Zone) TauntHero(
	userID uint64, peerGeneration uint64, objectID uint32, radius float32,
) ([]zonenpc.Snapshot, error) {
	if e == nil {
		return nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	hero, isFound := e.info.Hero.Snapshot(userID, peerGeneration)
	if !isFound || hero.ObjectID != objectID || hero.HitPoint <= 0 {
		e.mu.Unlock()
		return nil, errors.New("campaign hero unavailable")
	}
	acquired, err := e.info.NPCs.ForceTargetInRadius(zonenpc.Target{
		ObjectID: hero.ObjectID, Position: hero.Position,
		FootprintRadius: hero.FootprintRadius,
		Faction:         zonenpc.FactionPlayerAligned,
		Owner: zonenpc.ActionOwner{
			UserID: userID, PeerGeneration: peerGeneration,
		},
		IsAlive: true,
	}, radius)
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("instanceHeroTaunt: %w", err)
	}
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range acquired {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		timeline.Cancel(zonenpc.ReturnTimelineKey(npc.Plan.ObjectID))
	}
	return acquired, nil
}

func (e *Zone) TauntHeroTargets(
	userID uint64, peerGeneration uint64, objectID uint32,
	targetObjectIDs []uint32, expiresAt time.Time,
) ([]zonenpc.Snapshot, error) {
	if e == nil || len(targetObjectIDs) == 0 || expiresAt.IsZero() {
		return nil, errors.New("campaign targeted taunt invalid")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	hero, isFound := e.info.Hero.Snapshot(userID, peerGeneration)
	if !isFound || hero.ObjectID != objectID || hero.HitPoint <= 0 {
		e.mu.Unlock()
		return nil, errors.New("campaign hero unavailable")
	}
	target := zonenpc.Target{
		ObjectID: hero.ObjectID, Position: hero.Position,
		FootprintRadius: hero.FootprintRadius,
		Faction:         zonenpc.FactionPlayerAligned,
		Owner: zonenpc.ActionOwner{
			UserID: userID, PeerGeneration: peerGeneration,
		},
		IsAlive: true,
	}
	acquired := make([]zonenpc.Snapshot, 0, len(targetObjectIDs))
	for index, targetObjectID := range targetObjectIDs {
		npc, err := e.info.NPCs.ApplyTaunt(targetObjectID, target, expiresAt)
		if err != nil {
			e.mu.Unlock()
			return nil, fmt.Errorf("instanceHeroTargetTaunt[%d]: %w", index, err)
		}
		acquired = append(acquired, npc)
	}
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range acquired {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		timeline.Cancel(zonenpc.ReturnTimelineKey(npc.Plan.ObjectID))
	}
	return acquired, nil
}

func (e *Zone) TauntCompanionTargets(
	userID uint64, peerGeneration uint64, objectID uint32,
	targetObjectIDs []uint32, expiresAt time.Time,
) ([]zonenpc.Snapshot, error) {
	if e == nil || len(targetObjectIDs) == 0 || expiresAt.IsZero() {
		return nil, errors.New("campaign companion taunt invalid")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, errors.New("campaign zone not active")
	}
	companion, isFound := e.info.Companion.Snapshot(objectID)
	if !isFound || companion.UserID != userID ||
		companion.PeerGeneration != peerGeneration || !companion.IsTargetable ||
		companion.HitPoint <= 0 {
		e.mu.Unlock()
		return nil, errors.New("campaign companion unavailable")
	}
	target := zonenpc.Target{
		ObjectID: companion.ObjectID, Position: companion.Position,
		FootprintRadius: companion.FootprintRadius,
		Faction:         zonenpc.FactionPlayerAligned,
		Owner: zonenpc.ActionOwner{
			UserID: userID, PeerGeneration: peerGeneration,
		},
		IsAlive: true,
	}
	acquired := make([]zonenpc.Snapshot, 0, len(targetObjectIDs))
	for index, targetObjectID := range targetObjectIDs {
		npc, err := e.info.NPCs.ApplyTaunt(targetObjectID, target, expiresAt)
		if err != nil {
			e.mu.Unlock()
			return nil, fmt.Errorf("instanceCompanionTargetTaunt[%d]: %w", index, err)
		}
		acquired = append(acquired, npc)
	}
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range acquired {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		timeline.Cancel(zonenpc.ReturnTimelineKey(npc.Plan.ObjectID))
	}
	return acquired, nil
}

func (e *Zone) ExpireHeroTaunt(
	objectID uint32, targetObjectID uint32, expiresAt time.Time,
) ([]zonenpc.Snapshot, bool, error) {
	if e == nil || objectID == 0 || targetObjectID == 0 || expiresAt.IsZero() {
		return nil, false, errors.New("campaign taunt expiry invalid")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, false, nil
	}
	_, isExpired := e.info.NPCs.ExpireTaunt(objectID, targetObjectID, expiresAt)
	if !isExpired {
		e.mu.Unlock()
		return nil, false, nil
	}
	acquired, err := e.info.NPCs.AcquireTargets(e.livePlayerAlignedTargets(), false)
	if err != nil {
		e.mu.Unlock()
		return nil, false, fmt.Errorf("instanceTauntReacquire: %w", err)
	}
	e.mu.Unlock()
	return acquired, true, nil
}

func (e *Zone) ApplyNPCTargetDamage(
	owner zonenpc.ActionOwner,
	sourceObjectID uint32,
	targetObjectID uint32,
	damage float32,
	at time.Time,
) (NPCTargetDamage, bool, error) {
	if e == nil {
		return NPCTargetDamage{}, false, errors.New("nil campaign zone")
	}
	if owner.UserID == 0 || owner.PeerGeneration == 0 ||
		sourceObjectID == 0 || targetObjectID == 0 ||
		damage <= 0 || math.IsNaN(float64(damage)) ||
		math.IsInf(float64(damage), 0) || at.IsZero() {
		return NPCTargetDamage{}, false, errors.New("campaign target damage invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return NPCTargetDamage{}, false, errors.New("campaign zone not active")
	}
	actionMember, isFound := e.members[owner.UserID]
	if !isFound || actionMember.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	npc, isFound := e.info.NPCs.NPC(sourceObjectID)
	if !isFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionOwner != owner || npc.TargetObjectID != targetObjectID ||
		e.info.NPCs.StunRemaining(sourceObjectID, at) > 0 {
		return NPCTargetDamage{}, false, nil
	}
	return e.applyNPCTargetDamage(owner, sourceObjectID, targetObjectID, damage)
}

func (e *Zone) ApplyNPCReflectedTargetDamage(
	owner zonenpc.ActionOwner,
	sourceObjectID uint32,
	targetObjectID uint32,
	damage float32,
) (NPCTargetDamage, bool, error) {
	if e == nil {
		return NPCTargetDamage{}, false, errors.New("nil campaign zone")
	}
	if owner.UserID == 0 || owner.PeerGeneration == 0 ||
		sourceObjectID == 0 || targetObjectID == 0 || sourceObjectID == targetObjectID ||
		damage <= 0 || math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) {
		return NPCTargetDamage{}, false, errors.New("campaign reflected damage invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return NPCTargetDamage{}, false, errors.New("campaign zone not active")
	}
	actionMember, isFound := e.members[owner.UserID]
	if !isFound || actionMember.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	source, isFound := e.info.NPCs.NPC(sourceObjectID)
	if !isFound || source.Plan.IsFixture {
		return NPCTargetDamage{}, false, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isProfileFound || profile.PassiveMeleeDamageReflection <= 0 {
		return NPCTargetDamage{}, false, nil
	}
	return e.applyNPCTargetDamage(owner, sourceObjectID, targetObjectID, damage)
}

func (e *Zone) ApplyNPCAreaTargetDamage(
	owner zonenpc.ActionOwner,
	sourceObjectID uint32,
	targetObjectID uint32,
	damage float32,
	at time.Time,
) (NPCTargetDamage, bool, error) {
	if e == nil {
		return NPCTargetDamage{}, false, errors.New("nil campaign zone")
	}
	if owner.UserID == 0 || owner.PeerGeneration == 0 ||
		sourceObjectID == 0 || targetObjectID == 0 || damage <= 0 ||
		math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) || at.IsZero() {
		return NPCTargetDamage{}, false, errors.New("campaign area target damage invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return NPCTargetDamage{}, false, errors.New("campaign zone not active")
	}
	actionMember, isFound := e.members[owner.UserID]
	if !isFound || actionMember.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	npc, isFound := e.info.NPCs.NPC(sourceObjectID)
	if !isFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionOwner != owner || e.info.NPCs.StunRemaining(sourceObjectID, at) > 0 {
		return NPCTargetDamage{}, false, nil
	}
	return e.applyNPCTargetDamage(owner, sourceObjectID, targetObjectID, damage)
}

// ApplyNPCTargetStatusDamage commits damage from an already-admitted hostile
// modifier. Unlike a direct action, the retained status is allowed to outlive
// its caster's current target and life state.
func (e *Zone) ApplyNPCTargetStatusDamage(
	owner zonenpc.ActionOwner,
	sourceObjectID uint32,
	targetObjectID uint32,
	damage float32,
) (NPCTargetDamage, bool, error) {
	if e == nil {
		return NPCTargetDamage{}, false, errors.New("nil campaign zone")
	}
	if owner.UserID == 0 || owner.PeerGeneration == 0 ||
		sourceObjectID == 0 || targetObjectID == 0 ||
		damage <= 0 || math.IsNaN(float64(damage)) ||
		math.IsInf(float64(damage), 0) {
		return NPCTargetDamage{}, false, errors.New("campaign status damage invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return NPCTargetDamage{}, false, errors.New("campaign zone not active")
	}
	actionMember, isFound := e.members[owner.UserID]
	if !isFound || actionMember.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	_, isFound = e.info.NPCs.NPC(sourceObjectID)
	if !isFound {
		return NPCTargetDamage{}, false, nil
	}
	return e.applyNPCTargetDamage(owner, sourceObjectID, targetObjectID, damage)
}

func (e *Zone) applyNPCTargetDamage(
	owner zonenpc.ActionOwner,
	sourceObjectID uint32,
	targetObjectID uint32,
	damage float32,
) (NPCTargetDamage, bool, error) {
	actor, isFound := e.info.Hero.SnapshotByObjectID(targetObjectID)
	if !isFound {
		return e.applyNPCCompanionDamage(
			owner, sourceObjectID, targetObjectID, damage,
		)
	}
	if actor.HitPoint <= 0 {
		return NPCTargetDamage{}, false, nil
	}
	targetMember, isFound := e.members[actor.UserID]
	if !isFound || targetMember.PeerGeneration != actor.PeerGeneration ||
		targetMember.Slot > 255 {
		return NPCTargetDamage{}, false, nil
	}
	hitPoint := max(float32(0), actor.HitPoint-damage)
	previous, err := e.info.Hero.SetResources(
		actor.UserID, actor.PeerGeneration, targetObjectID,
		hitPoint, actor.ManaPoint,
	)
	if err != nil {
		return NPCTargetDamage{}, false, fmt.Errorf("instanceHeroDamage: %w", err)
	}
	current, isFound := e.info.Hero.Snapshot(
		actor.UserID, actor.PeerGeneration,
	)
	if !isFound {
		return NPCTargetDamage{}, false, errors.New("campaign hero unavailable")
	}
	e.publishHeroResourceExcept(
		targetMember, current,
		zoneprojection.Subscriber{
			UserID: owner.UserID, PeerGeneration: owner.PeerGeneration,
		},
		zoneprojection.Subscriber{
			UserID: actor.UserID, PeerGeneration: actor.PeerGeneration,
		},
	)
	return NPCTargetDamage{
		PreviousHitPoint: previous.HitPoint, HitPoint: current.HitPoint,
		IsHero: true, IsDefeated: current.HitPoint == 0,
	}, true, nil
}

func (e *Zone) applyNPCCompanionDamage(
	owner zonenpc.ActionOwner,
	sourceObjectID uint32,
	targetObjectID uint32,
	damage float32,
) (NPCTargetDamage, bool, error) {
	companion, isFound := e.info.Companion.Snapshot(targetObjectID)
	if !isFound || !companion.IsTargetable || companion.HitPoint <= 0 {
		return NPCTargetDamage{}, false, nil
	}
	hitPoint := max(float32(0), companion.HitPoint-damage)
	previous, current, err := e.info.Companion.SetHitPoint(
		targetObjectID, hitPoint,
	)
	if err != nil {
		return NPCTargetDamage{}, false, fmt.Errorf("instanceCompanionDamage: %w", err)
	}
	appliedDamage := previous.HitPoint - current.HitPoint
	e.projection.PublishCompanionDamage(
		zoneprojection.CompanionDamage{
			SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
			Damage: appliedDamage, HitPoint: current.HitPoint,
			IsDefeated: current.HitPoint == 0,
		},
		zoneprojection.Subscriber{
			UserID: owner.UserID, PeerGeneration: owner.PeerGeneration,
		},
	)
	if current.HitPoint == 0 {
		released, releaseErr := e.info.NPCs.ReplaceTarget(
			targetObjectID, e.livePlayerAlignedTargets(),
		)
		if releaseErr != nil {
			return NPCTargetDamage{}, false,
				fmt.Errorf("instanceCompanionRetarget: %w", releaseErr)
		}
		for _, npc := range released {
			e.info.Timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		}
		returning := e.returnUnacquiredNPCs(released, nil)
		e.publishNPCReturns(returning)
	}
	return NPCTargetDamage{
		PreviousHitPoint: previous.HitPoint, HitPoint: current.HitPoint,
		IsDefeated: current.HitPoint == 0,
	}, true, nil
}

func (e *Zone) returnUnacquiredNPCs(
	released []zonenpc.Snapshot, acquired []zonenpc.Snapshot,
) []zonenpc.Snapshot {
	acquiredObjectID := make(map[uint32]bool, len(acquired))
	for _, npc := range acquired {
		acquiredObjectID[npc.Plan.ObjectID] = true
		e.info.Timeline.Cancel(
			zonenpc.ReturnTimelineKey(npc.Plan.ObjectID),
		)
	}
	for _, npc := range released {
		if acquiredObjectID[npc.Plan.ObjectID] {
			continue
		}
		step := npcReturnStep{
			zone: e, objectID: npc.Plan.ObjectID,
		}
		err := e.info.Timeline.Schedule(
			zonenpc.ReturnTimelineKey(npc.Plan.ObjectID),
			npcReturnIdleDelay, step.execute, e.info.Timer.Schedule,
		)
		if err != nil {
			continue
		}
	}
	return nil
}

func (e *Zone) completeNPCReturn(objectID uint32) {
	if e == nil || objectID == 0 {
		return
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return
	}
	npc, isFound := e.info.NPCs.NPC(objectID)
	if !isFound || npc.IsDefeated || npc.TargetObjectID != 0 {
		e.mu.Unlock()
		return
	}
	returning := e.info.NPCs.ReturnHome([]uint32{objectID})
	e.mu.Unlock()
	e.publishNPCReturns(returning)
}

func (e *Zone) publishNPCReturns(returning []zonenpc.Snapshot) {
	for _, npc := range returning {
		e.projection.PublishNPCAction(zonenpc.ActionEvent{
			Kind: zonenpc.ActionEventReturn,
			Plan: zonenpc.FirstActionPlan{
				ObjectID:       npc.Plan.ObjectID,
				TargetPosition: npc.Origin,
			},
		}, zoneprojection.Subscriber{})
	}
}

func (e *Zone) publishHeroResource(member Member, actor zonehero.Actor) {
	e.publishHeroResourceExcept(
		member, actor, zoneprojection.Subscriber{
			UserID: member.UserID, PeerGeneration: member.PeerGeneration,
		},
	)
}

func (e *Zone) PublishHeroResourceTo(
	userID uint64, peerGeneration uint64,
) {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return
	}
	e.mu.RLock()
	member, isMemberFound := e.members[userID]
	actor, isActorFound := e.info.Hero.Snapshot(userID, peerGeneration)
	e.mu.RUnlock()
	if !isMemberFound || member.PeerGeneration != peerGeneration ||
		!isActorFound || member.Slot > 255 {
		return
	}
	e.projection.PublishHeroResourceTo(
		zoneprojection.HeroResource{
			PlayerSlot: uint8(member.Slot), CreatureIndex: actor.CreatureIndex,
			ObjectID: actor.ObjectID, HitPoint: actor.HitPoint,
			ManaPoint: actor.ManaPoint, MaximumHitPoint: actor.MaximumHitPoint,
			MaximumManaPoint: actor.MaximumManaPoint,
		},
		zoneprojection.Subscriber{
			UserID: userID, PeerGeneration: peerGeneration,
		},
	)
}

func (e *Zone) PublishCompanionResourceTo(
	userID uint64, peerGeneration uint64, objectID uint32,
) {
	if e == nil || userID == 0 || peerGeneration == 0 || objectID == 0 {
		return
	}
	companion, isFound := e.info.Companion.Snapshot(objectID)
	if !isFound || companion.UserID != userID ||
		companion.PeerGeneration != peerGeneration {
		return
	}
	e.projection.PublishCompanionResourceTo(
		zoneprojection.CompanionResource{
			ObjectID: companion.ObjectID, HitPoint: companion.HitPoint,
			MaximumHitPoint: companion.MaximumHitPoint,
		},
		zoneprojection.Subscriber{
			UserID: userID, PeerGeneration: peerGeneration,
		},
	)
}

func (e *Zone) publishHeroResourceExcept(
	member Member, actor zonehero.Actor,
	excludedSubscribers ...zoneprojection.Subscriber,
) {
	e.projection.PublishHeroResource(
		zoneprojection.HeroResource{
			PlayerSlot:       uint8(member.Slot),
			CreatureIndex:    actor.CreatureIndex,
			ObjectID:         actor.ObjectID,
			HitPoint:         actor.HitPoint,
			ManaPoint:        actor.ManaPoint,
			MaximumHitPoint:  actor.MaximumHitPoint,
			MaximumManaPoint: actor.MaximumManaPoint,
		},
		excludedSubscribers...,
	)
}

func (e *Zone) Snapshot() Snapshot {
	if e == nil {
		return Snapshot{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	member := make([]Member, 0, len(e.members))
	for _, current := range e.members {
		member = append(member, current)
	}
	sort.Slice(member, func(left int, right int) bool {
		return member[left].UserID < member[right].UserID
	})
	clearedSpawnGroupIDs := make([]uint32, 0, len(e.clearedSpawnGroups))
	for spawnGroupID := range e.clearedSpawnGroups {
		clearedSpawnGroupIDs = append(clearedSpawnGroupIDs, spawnGroupID)
	}
	sort.Slice(clearedSpawnGroupIDs, func(left int, right int) bool {
		return clearedSpawnGroupIDs[left] < clearedSpawnGroupIDs[right]
	})
	return Snapshot{
		ID: e.id, Generation: e.generation, CompletionID: e.completionID,
		State: e.state, Level: e.info.Level, Difficulty: e.info.Difficulty,
		ChainLevelIndex: e.info.ChainLevelIndex, MemberLimit: e.info.MemberLimit,
		Members: member, ClearedSpawnGroupIDs: clearedSpawnGroupIDs,
		IsPopulationPrimed: e.isPopulationPrimed, IsRestored: e.isRestored,
		IsObjectiveComplete: e.isObjectiveComplete,
	}
}

func (e *Zone) restoreCheckpoint(snapshot zonecheckpoint.Snapshot) error {
	if e == nil {
		return errors.New("nil zone")
	}
	if e.info.MemberLimit != 0 &&
		len(snapshot.Members) > int(e.info.MemberLimit) {
		return errors.New("checkpoint member capacity exceeded")
	}
	for index, member := range snapshot.Members {
		restoredMember := Member{
			UserID: member.UserID, Slot: member.Slot,
			AbilityCount: member.AbilityCount, IsConnected: false,
		}
		e.members[member.UserID] = restoredMember
		err := e.info.Outcome.RestoreMember(member.UserID)
		if err != nil {
			return fmt.Errorf("checkpointOutcomeMember[%d]: %w", index, err)
		}
		err = e.info.ResultVote.RestoreVoter(member.UserID)
		if err != nil {
			return fmt.Errorf("checkpointVoteMember[%d]: %w", index, err)
		}
	}
	npcStates := make([]zonenpc.Snapshot, 0, len(snapshot.NPCs))
	objectiveNPCObjectIDs := make([]uint32, 0, len(snapshot.NPCs))
	defeatedObjectiveNPCObjectIDs := make([]uint32, 0, len(snapshot.NPCs))
	var maximumObjectID uint32
	for _, npc := range snapshot.NPCs {
		npcStates = append(npcStates, npc.State)
		maximumObjectID = max(maximumObjectID, npc.State.Plan.ObjectID)
		if npc.State.Plan.IsFixture || npc.State.Plan.IsRewardSuppressed {
			continue
		}
		objectiveNPCObjectIDs = append(
			objectiveNPCObjectIDs, npc.State.Plan.ObjectID,
		)
		if npc.State.IsDefeated {
			defeatedObjectiveNPCObjectIDs = append(
				defeatedObjectiveNPCObjectIDs, npc.State.Plan.ObjectID,
			)
		}
	}
	err := e.info.NPCs.Restore(npcStates)
	if err != nil {
		return fmt.Errorf("checkpointNPC: %w", err)
	}
	restoredPlans := make([]zonenpc.SpawnPlan, 0, len(npcStates))
	for _, npcState := range npcStates {
		restoredPlans = append(restoredPlans, npcState.Plan)
	}
	err = e.info.NPCs.ConfigureShieldedAffix(
		restoredPlans, snapshot.Difficulty,
		max(uint32(1), uint32(len(snapshot.Members))),
	)
	if err != nil {
		return fmt.Errorf("checkpointShieldedAffix: %w", err)
	}
	err = e.info.NPCs.ConfigureCarapaceAffix(
		restoredPlans, snapshot.Difficulty,
	)
	if err != nil {
		return fmt.Errorf("checkpointCarapaceAffix: %w", err)
	}
	if maximumObjectID != 0 {
		err = e.info.ObjectID.AdvancePast(maximumObjectID)
		if err != nil {
			return fmt.Errorf("checkpointObjectID: %w", err)
		}
	}
	if e.info.ObjectiveProgress != nil {
		err = e.info.ObjectiveProgress.Restore(
			objectiveNPCObjectIDs, defeatedObjectiveNPCObjectIDs,
		)
		if err != nil {
			return fmt.Errorf("checkpointObjectiveProgress: %w", err)
		}
	}
	for _, crystal := range snapshot.Crystals {
		if crystal.UserID != 0 {
			e.crystals[crystal.UserID] = crystal.Inventory
		}
	}
	for index, restoredAward := range snapshot.ExperienceAwards {
		memberAwards := make(map[uint64]uint32, len(restoredAward.MemberAwards))
		for userID, experience := range restoredAward.MemberAwards {
			if experience > ^uint32(0)-e.experienceTotals[userID] {
				return fmt.Errorf("checkpointExperience[%d]: overflow", index)
			}
			memberAwards[userID] = experience
			e.experienceTotals[userID] += experience
		}
		e.experienceAwards[restoredAward.ObjectID] = ExperienceAward{
			ObjectID:       restoredAward.ObjectID,
			BaseExperience: restoredAward.BaseExperience,
			MemberAwards:   memberAwards,
		}
	}
	for _, hero := range snapshot.Heroes {
		if hero.UserID != 0 {
			e.checkpointHeroes[hero.UserID] = hero
		}
	}
	for _, restoredSquad := range snapshot.Squads {
		if restoredSquad.UserID != 0 {
			e.checkpointSquads[restoredSquad.UserID] = restoredSquad
		}
	}
	for _, spawnGroupID := range snapshot.ClearedSpawnGroupIDs {
		if spawnGroupID != 0 {
			e.clearedSpawnGroups[spawnGroupID] = struct{}{}
		}
	}
	err = e.info.Objective.Restore(snapshot.Objectives)
	if err != nil {
		return fmt.Errorf("checkpointObjective: %w", err)
	}
	err = e.info.Script.RestoreUses(snapshot.ScriptUses)
	if err != nil {
		return fmt.Errorf("checkpointScript: %w", err)
	}
	err = e.info.Security.Restore(snapshot.Security)
	if err != nil {
		return fmt.Errorf("checkpointSecurity: %w", err)
	}
	err = e.info.Horde.RestoreCompleted(snapshot.Hordes)
	if err != nil {
		return fmt.Errorf("checkpointHorde: %w", err)
	}
	if snapshot.Elapsed > 0 {
		e.startedAt = time.Now().Add(-snapshot.Elapsed)
	}
	e.isPopulationPrimed = len(npcStates) != 0 ||
		len(snapshot.ClearedSpawnGroupIDs) != 0
	e.isRestored = true
	return nil
}

// SetCheckpointSquad updates the zone-owned durable view of one member's
// complete squad. It contains no transport or presentation state.
func (e *Zone) SetCheckpointSquad(
	userID uint64, peerGeneration uint64, state squad.State,
	position game.Vec3,
) error {
	if e == nil || userID == 0 || peerGeneration == 0 {
		return errors.New("checkpoint squad identity invalid")
	}
	if math.IsNaN(float64(position.X)) || math.IsInf(float64(position.X), 0) ||
		math.IsNaN(float64(position.Y)) || math.IsInf(float64(position.Y), 0) ||
		math.IsNaN(float64(position.Z)) || math.IsInf(float64(position.Z), 0) {
		return errors.New("checkpoint squad position invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration {
		return errors.New("checkpoint squad member unavailable")
	}
	e.checkpointSquads[userID] = zonecheckpoint.Squad{
		UserID: userID, Position: position, State: state,
	}
	return nil
}

func (e *Zone) RestoredSquad(userID uint64) (zonecheckpoint.Squad, bool) {
	if e == nil || userID == 0 {
		return zonecheckpoint.Squad{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	restoredSquad, isFound := e.checkpointSquads[userID]
	return restoredSquad, isFound && e.isRestored
}

// RestoredHero returns the process-independent active-hero state. Setup keeps
// it available until the complete client baseline has been assembled so a
// failed transport attempt can retry without losing durable state.
func (e *Zone) RestoredHero(userID uint64) (zonecheckpoint.Hero, bool) {
	if e == nil || userID == 0 {
		return zonecheckpoint.Hero{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	hero, isFound := e.checkpointHeroes[userID]
	return hero, isFound && e.isRestored
}

// ConsumeRestoredHero prevents a successfully applied checkpoint record from
// being replayed during a later squad swap.
func (e *Zone) ConsumeRestoredHero(userID uint64) {
	if e == nil || userID == 0 {
		return
	}
	e.mu.Lock()
	delete(e.checkpointHeroes, userID)
	e.mu.Unlock()
}

func (e *Zone) IsRestored() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isRestored
}

// SaveCheckpoint captures one immutable semantic boundary and queues it for
// persistence without putting storage I/O on the gameplay path.
func (e *Zone) SaveCheckpoint(reason zonecheckpoint.Reason) {
	if e == nil || reason == "" || e.info.Checkpoint == nil {
		return
	}
	if !e.isCheckpointSafe() {
		return
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return
	}
	members := make([]zonecheckpoint.Member, 0, len(e.members))
	for _, member := range e.members {
		members = append(members, zonecheckpoint.Member{
			UserID: member.UserID, Slot: member.Slot,
			AbilityCount: member.AbilityCount,
		})
	}
	crystals := make([]zonecheckpoint.Crystal, 0, len(e.crystals))
	for userID, inventory := range e.crystals {
		crystals = append(crystals, zonecheckpoint.Crystal{
			UserID: userID, Inventory: inventory,
		})
	}
	squads := make([]zonecheckpoint.Squad, 0, len(e.checkpointSquads))
	for _, current := range e.checkpointSquads {
		squads = append(squads, current)
	}
	restoredHeroes := make([]zonecheckpoint.Hero, 0, len(e.checkpointHeroes))
	for _, current := range e.checkpointHeroes {
		restoredHeroes = append(restoredHeroes, current)
	}
	clearedSpawnGroupIDs := make([]uint32, 0, len(e.clearedSpawnGroups))
	for spawnGroupID := range e.clearedSpawnGroups {
		clearedSpawnGroupIDs = append(clearedSpawnGroupIDs, spawnGroupID)
	}
	experienceAwards := make(
		[]zonecheckpoint.ExperienceAward, 0, len(e.experienceAwards),
	)
	for _, current := range e.experienceAwards {
		memberAwards := make(map[uint64]uint32, len(current.MemberAwards))
		for userID, experience := range current.MemberAwards {
			memberAwards[userID] = experience
		}
		experienceAwards = append(experienceAwards, zonecheckpoint.ExperienceAward{
			ObjectID: current.ObjectID, BaseExperience: current.BaseExperience,
			MemberAwards: memberAwards,
		})
	}
	zoneID := e.id
	generation := e.generation
	level := e.info.Level
	difficulty := e.info.Difficulty
	now := time.Now().UTC()
	elapsed := now.Sub(e.startedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	recorder := e.info.Checkpoint
	e.mu.Unlock()
	sort.Slice(members, func(left int, right int) bool {
		return members[left].UserID < members[right].UserID
	})
	sort.Slice(crystals, func(left int, right int) bool {
		return crystals[left].UserID < crystals[right].UserID
	})
	sort.Slice(squads, func(left int, right int) bool {
		return squads[left].UserID < squads[right].UserID
	})
	sort.Slice(clearedSpawnGroupIDs, func(left int, right int) bool {
		return clearedSpawnGroupIDs[left] < clearedSpawnGroupIDs[right]
	})
	sort.Slice(experienceAwards, func(left int, right int) bool {
		return experienceAwards[left].ObjectID < experienceAwards[right].ObjectID
	})
	heroSnapshots := e.info.Hero.Snapshots()
	heroes := make([]zonecheckpoint.Hero, 0, len(heroSnapshots)+len(restoredHeroes))
	for _, hero := range heroSnapshots {
		heroes = append(heroes, zonecheckpoint.Hero{
			UserID: hero.UserID, ObjectID: hero.ObjectID,
			CreatureIndex: hero.CreatureIndex, Position: hero.Position,
			FootprintRadius: hero.FootprintRadius, HitPoint: hero.HitPoint,
			ManaPoint: hero.ManaPoint, MaximumHitPoint: hero.MaximumHitPoint,
			MaximumManaPoint: hero.MaximumManaPoint, IsStealthed: hero.IsStealthed,
		})
		for index := range squads {
			if squads[index].UserID == hero.UserID {
				squads[index].Position = hero.Position
				break
			}
		}
	}
	for _, hero := range restoredHeroes {
		isLive := false
		for _, current := range heroSnapshots {
			if current.UserID == hero.UserID {
				isLive = true
				break
			}
		}
		if !isLive {
			heroes = append(heroes, hero)
		}
	}
	sort.Slice(heroes, func(left int, right int) bool {
		return heroes[left].UserID < heroes[right].UserID
	})
	npcSnapshots := e.info.NPCs.Snapshots()
	npcs := make([]zonecheckpoint.NPC, 0, len(npcSnapshots))
	for _, npc := range npcSnapshots {
		npcs = append(npcs, zonecheckpoint.NPC{
			State: npc,
		})
	}
	objectives := []sim.ObjectiveSnapshot(nil)
	if e.info.Objective != nil && e.info.Objective.State() != nil {
		objectives = e.info.Objective.State().Snapshot()
	}
	scriptUses := e.info.Script.SnapshotUses()
	security := e.info.Security.Snapshot()
	hordes := e.info.Horde.Snapshots()
	snapshot := zonecheckpoint.Snapshot{
		Version: zonecheckpoint.Version, ZoneID: zoneID,
		ZoneGeneration: generation, CompletionID: e.CompletionID(),
		Level: level, Difficulty: difficulty, Reason: reason,
		SavedAt: now, Elapsed: elapsed, Members: members, Heroes: heroes,
		Squads: squads, NPCs: npcs, Hordes: hordes, Crystals: crystals,
		ExperienceAwards: experienceAwards,
		Security:         security, Objectives: objectives, ScriptUses: scriptUses,
		ClearedSpawnGroupIDs: clearedSpawnGroupIDs,
	}
	// Serialize the queue operation with terminal state. Without this final
	// fence, completion could discard a checkpoint while an older capture was
	// still being assembled and that stale capture could then recreate it.
	e.mu.Lock()
	if e.state == StateActive && e.generation == generation &&
		e.isCheckpointEncounterSafe() {
		recorder.Record(snapshot)
	}
	e.mu.Unlock()
}

func (e *Zone) isCheckpointSafe() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	isSafe := e.state == StateActive && e.isCheckpointEncounterSafe()
	e.mu.RUnlock()
	return isSafe
}

// isCheckpointEncounterSafe requires e.mu to be held. Encounter sessions own
// independent locks, and their transitions never acquire the zone lock while
// holding those locks.
func (e *Zone) isCheckpointEncounterSafe() bool {
	return e.info.Boss.IsDormant() && e.info.Horde.IsCheckpointSafe()
}

func (e *Zone) SaveSpawnGroupCheckpoint(spawnGroupID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return
	}
	if spawnGroupID != 0 {
		e.clearedSpawnGroups[spawnGroupID] = struct{}{}
	}
	e.mu.Unlock()
	e.SaveCheckpoint(zonecheckpoint.ReasonSpawnGroupCleared)
}

// SaveCheckpointIfSafe records a pickup boundary only while no living NPC is
// actively targeting a player-aligned actor.
func (e *Zone) SaveCheckpointIfSafe(reason zonecheckpoint.Reason) bool {
	if e == nil {
		return false
	}
	for _, npc := range e.info.NPCs.Snapshots() {
		if npc.IsDefeated || npc.HitPoint <= 0 || npc.TargetObjectID == 0 {
			continue
		}
		return false
	}
	e.SaveCheckpoint(reason)
	return true
}

func (e *Zone) NPCs() *zonenpc.Session {
	if e == nil {
		return nil
	}
	return e.info.NPCs
}

func (e *Zone) Effect() *zoneeffect.Inventory {
	if e == nil {
		return nil
	}
	return e.info.Effect
}

func (e *Zone) Hero() *zonehero.Session {
	if e == nil {
		return nil
	}
	return e.info.Hero
}

func (e *Zone) Companion() *zonecompanion.Session {
	if e == nil {
		return nil
	}
	return e.info.Companion
}

func (e *Zone) Interactable() *zoneinteract.UseSession {
	if e == nil {
		return nil
	}
	return e.info.Interactable
}

func (e *Zone) Pickups() *zoneinteract.PickupRegistry {
	if e == nil {
		return nil
	}
	return e.info.Pickups
}

func (e *Zone) PickupPayload() *zoneinteract.PickupPayloadRegistry {
	if e == nil {
		return nil
	}
	return e.info.PickupPayload
}

func (e *Zone) Orbs() *zoneinteract.OrbRegistry {
	if e == nil {
		return nil
	}
	return e.info.Orbs
}

func (e *Zone) Loot() *zoneloot.Session {
	if e == nil {
		return nil
	}
	return e.info.Loot
}

func (e *Zone) DNA() *zoneloot.DNASession {
	if e == nil {
		return nil
	}
	return e.info.DNA
}

func (e *Zone) Population() *zonepopulation.Session {
	if e == nil {
		return nil
	}
	return e.info.Population
}

func (e *Zone) DirectorDefinition() game.CampaignDirector {
	if e == nil {
		return game.CampaignDirector{}
	}
	return e.info.DirectorDefinition
}

func (e *Zone) CampaignDirectorSnapshot() game.CampaignDirectorSnapshot {
	if e == nil || e.info.Director == nil {
		return game.CampaignDirectorSnapshot{}
	}
	return e.info.Director.Snapshot()
}

func (e *Zone) Navigation() *navigation.Mesh {
	if e == nil {
		return nil
	}
	return e.info.Navigation
}

func (e *Zone) CreatureFootprintRadius(
	userID uint64, peerGeneration uint64, creatureIndex uint32,
) float32 {
	if e == nil || userID == 0 || peerGeneration == 0 || creatureIndex >= squad.Size {
		return 0
	}
	e.mu.RLock()
	member, isFound := e.members[userID]
	e.mu.RUnlock()
	if !isFound || member.PeerGeneration != peerGeneration {
		return 0
	}
	return member.CreatureFootprints[creatureIndex]
}

func (e *Zone) Route() *sim.DirectorSession {
	if e == nil {
		return nil
	}
	return e.info.Route
}

func (e *Zone) Script() *game.CampaignScriptRegistry {
	if e == nil {
		return nil
	}
	return e.info.Script
}

func (e *Zone) Encounter() *zoneencounter.StageSession {
	if e == nil {
		return nil
	}
	return e.info.Encounter
}

func (e *Zone) Horde() *zonehorde.Session {
	if e == nil {
		return nil
	}
	return e.info.Horde
}

func (e *Zone) Boss() *zoneboss.Session {
	if e == nil {
		return nil
	}
	return e.info.Boss
}

func (e *Zone) Death() *zonedeath.Session {
	if e == nil {
		return nil
	}
	return e.info.Death
}

func (e *Zone) Objective() *zoneobjective.Session {
	if e == nil {
		return nil
	}
	return e.info.Objective
}

func (e *Zone) ObjectiveProgress() *zoneobjective.Progress {
	if e == nil {
		return nil
	}
	return e.info.ObjectiveProgress
}

func (e *Zone) Timeline() *zonetimeline.Session {
	if e == nil {
		return nil
	}
	return e.info.Timeline
}

func (e *Zone) NPCRandom() *sim.SimulatorRandom {
	if e == nil {
		return nil
	}
	return e.info.NPCRandom
}

func (e *Zone) DropRandom() *sim.SimulatorRandom {
	if e == nil {
		return nil
	}
	return e.info.DropRandom
}

func (e *Zone) HordeBarrierPlans(markerSetName string) []zonebarrier.Plan {
	if e == nil {
		return nil
	}
	plans := e.info.HordeBarrierPlans[strings.ToLower(markerSetName)]
	return append([]zonebarrier.Plan(nil), plans...)
}

func (e *Zone) ActiveHordeBarrierPlans() [][]zonebarrier.Plan {
	if e == nil || e.info.Horde == nil {
		return nil
	}
	active := make([][]zonebarrier.Plan, 0)
	for _, snapshot := range e.info.Horde.Snapshots() {
		if !snapshot.IsGateActive {
			continue
		}
		plans := e.HordeBarrierPlans(snapshot.MarkerSetName)
		if len(plans) != 0 {
			active = append(active, plans)
		}
	}
	return active
}

func (e *Zone) ScriptObjects() []game.CampaignScriptObject {
	if e == nil {
		return nil
	}
	return append([]game.CampaignScriptObject(nil), e.info.ScriptObjects...)
}

func (e *Zone) ScriptObjectPlans() []zoneobject.ScriptPlan {
	if e == nil {
		return nil
	}
	return append([]zoneobject.ScriptPlan(nil), e.info.ScriptObjectPlans...)
}

func (e *Zone) FixturePlans() []zonenpc.SpawnPlan {
	if e == nil {
		return nil
	}
	return append([]zonenpc.SpawnPlan(nil), e.info.FixturePlans...)
}

func (e *Zone) CatalystProgram() sim.Program {
	if e == nil {
		return sim.Program{}
	}
	return e.info.CatalystProgram
}

func (e *Zone) OverdriveProgram() sim.Program {
	if e == nil {
		return sim.Program{}
	}
	return e.info.OverdriveProgram
}

func (e *Zone) CrystalDefinitions() []sim.CrystalDefinition {
	if e == nil {
		return nil
	}
	return append([]sim.CrystalDefinition(nil), e.info.CrystalDefinitions...)
}

func (e *Zone) CrystalLevelOffsets() []sim.CrystalLevelOffset {
	if e == nil {
		return nil
	}
	return append([]sim.CrystalLevelOffset(nil), e.info.CrystalLevelOffsets...)
}

func (e *Zone) Security() *zonesecurity.Session {
	if e == nil {
		return nil
	}
	return e.info.Security
}

func (e *Zone) SecurityThreats() []zonesecurity.Threat {
	if e == nil || e.info.NPCs == nil {
		return nil
	}
	npcs := e.info.NPCs.LiveSnapshots()
	threats := make([]zonesecurity.Threat, 0, len(npcs))
	for _, npc := range npcs {
		threats = append(threats, zonesecurity.Threat{
			Position:    npc.Plan.Position,
			Footprint:   npc.Plan.NPCProfile.FootprintRadius,
			IsDefeated:  npc.IsDefeated,
			IsFixture:   npc.Plan.IsFixture,
			IsInvisible: npc.IsInvisibleToSecurityTeleporter,
		})
	}
	return threats
}

func (e *Zone) PublishObjective(updates []zoneobjective.Update) {
	if e == nil {
		return
	}
	e.projection.PublishObjective(updates)
}

// UpdateElapsedObjective keeps the live FinishLevelQuickly presentation on
// the same authoritative clock used by the campaign HUD. ObjectiveSession
// deduplicates concurrent co-op clock ticks before projection fanout.
func (e *Zone) UpdateElapsedObjective(elapsed time.Duration) error {
	if e == nil || e.info.Objective == nil {
		return nil
	}
	updates, err := e.info.Objective.UpdateElapsed(e.ctx, elapsed)
	if err != nil {
		return fmt.Errorf("elapsedObjective: %w", err)
	}
	e.PublishObjective(updates)
	return nil
}

// RecordNPCDamage routes resolved player-aligned damage through selected
// objectives before publishing the transport-neutral combat presentation.
// Immune outcomes are presentation-only and do not advance damage objectives.
// Objective failures never suppress the authoritative damage projection.
func (e *Zone) RecordNPCDamage(
	ctx context.Context, damage zonenpc.DamageEvent,
) error {
	if e == nil {
		return errors.New("nil zone")
	}
	if ctx == nil {
		return errors.New("npc damage context unavailable")
	}
	var objectiveErr error
	if !damage.IsDamageImmune {
		objectiveErr = e.ApplyNPCDamageObjectives(ctx, damage)
	}
	e.projection.PublishNPCDamage(damage)
	return objectiveErr
}

// RecordNPCHeal routes committed non-player healing through selected packaged
// objectives. Healing remains authoritative even when an optional objective
// callback cannot consume its semantic event.
func (e *Zone) RecordNPCHeal(
	ctx context.Context, sourceObjectID uint32, targetObjectID uint32,
	healing float32,
) error {
	if e == nil || e.info.Objective == nil {
		return nil
	}
	if ctx == nil || sourceObjectID == 0 || targetObjectID == 0 ||
		healing <= 0 || math.IsNaN(float64(healing)) || math.IsInf(float64(healing), 0) {
		return errors.New("npc heal objective context unavailable")
	}
	updates, err := e.info.Objective.ApplyEvent(ctx, sim.LuaObjectiveEvent{
		Kind: sim.LuaObjectiveEventHeal, ObjectID: targetObjectID,
		TargetObjectID: targetObjectID, SourceObjectID: sourceObjectID,
		Healing: float64(healing), IsTargetNPC: true,
	})
	if err != nil {
		return fmt.Errorf("npcHealObjective: %w", err)
	}
	e.PublishObjective(updates)
	return nil
}

// RecordModifierCreated routes a committed status modifier through packaged
// objectives after gameplay authority has accepted and retained the modifier.
func (e *Zone) RecordModifierCreated(
	ctx context.Context, eventObjectID uint32, sourceObjectID uint32,
	targetObjectID uint32, descriptor uint32,
) error {
	if e == nil || e.info.Objective == nil {
		return nil
	}
	if ctx == nil || eventObjectID == 0 || sourceObjectID == 0 ||
		targetObjectID == 0 || descriptor == 0 || e.info.Hero == nil {
		return errors.New("modifier objective context unavailable")
	}
	target, isTargetFound := e.info.Hero.SnapshotByObjectID(targetObjectID)
	if !isTargetFound {
		return nil
	}
	e.mu.RLock()
	member, isMemberFound := e.members[target.UserID]
	e.mu.RUnlock()
	if !isMemberFound || member.Slot >= game.MaxGamePlayers {
		return nil
	}
	updates, err := e.info.Objective.ApplyEvent(ctx, sim.LuaObjectiveEvent{
		Kind: sim.LuaObjectiveEventModifierCreated, ObjectID: eventObjectID,
		TargetObjectID: targetObjectID, SourceObjectID: sourceObjectID,
		TargetTeam: 1, SourceTeam: 2, PlayerIndex: uint8(member.Slot),
		IsTargetPlayerControlled: true, ModifierDescriptor: descriptor,
	})
	if err != nil {
		return fmt.Errorf("modifierObjective: %w", err)
	}
	e.PublishObjective(updates)
	return nil
}

// RecordQuestCrystalFound invokes the authored LootCrystals callback after a
// quest-crystal pickup has committed at the world boundary.
func (e *Zone) RecordQuestCrystalFound(ctx context.Context) error {
	if e == nil || e.info.Objective == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("quest crystal objective context unavailable")
	}
	updates, err := e.info.Objective.Invoke(
		ctx, zoneobjective.LootCrystalsID, "CrystalFound",
	)
	if err != nil {
		return fmt.Errorf("questCrystalObjective: %w", err)
	}
	e.PublishObjective(updates)
	return nil
}

func (e *Zone) ApplyNPCDamageObjectives(
	ctx context.Context, damage zonenpc.DamageEvent,
) error {
	if e == nil {
		return errors.New("nil zone")
	}
	if ctx == nil {
		return errors.New("npc damage context unavailable")
	}
	source := e.objectiveSource(damage.SourceObjectID)
	if !source.IsFound || source.UserID == 0 || e.info.Objective == nil {
		return nil
	}
	updates, err := e.info.Objective.ApplyEvent(
		ctx, sim.LuaObjectiveEvent{
			Kind:           sim.LuaObjectiveEventDamage,
			ObjectID:       damage.TargetObjectID,
			TargetObjectID: damage.TargetObjectID,
			SourceObjectID: damage.SourceObjectID,
			Damage:         float64(damage.Damage),
			DamageInteger:  int32(math.Round(float64(damage.Damage))),
			TargetTeam:     2, SourceTeam: 1,
			PlayerIndex: source.PlayerIndex, IsTargetNPC: true,
			IsSourcePlayerControlled: source.IsPlayerControlled,
		},
	)
	if err != nil {
		return fmt.Errorf("npcDamageObjective: %w", err)
	}
	e.PublishObjective(updates)
	return nil
}

func (e *Zone) objectiveSource(
	objectID uint32,
) objectiveActor {
	if e == nil || objectID == 0 {
		return objectiveActor{}
	}
	userID := uint64(0)
	isPlayerControlled := false
	heroActor := zonehero.Actor{}
	isHeroFound := false
	if e.info.Hero != nil {
		heroActor, isHeroFound = e.info.Hero.SnapshotByObjectID(objectID)
	}
	if isHeroFound {
		userID = heroActor.UserID
		isPlayerControlled = true
	} else {
		if e.info.Companion == nil {
			return objectiveActor{}
		}
		companion, isCompanionFound := e.info.Companion.Snapshot(objectID)
		if !isCompanionFound {
			return objectiveActor{}
		}
		userID = companion.UserID
	}
	e.mu.RLock()
	member, isMemberFound := e.members[userID]
	e.mu.RUnlock()
	if !isMemberFound || member.Slot >= game.MaxGamePlayers {
		return objectiveActor{
			UserID: userID, IsPlayerControlled: isPlayerControlled,
		}
	}
	return objectiveActor{
		UserID: userID, PlayerIndex: uint8(member.Slot),
		IsPlayerControlled: isPlayerControlled, IsFound: true,
	}
}

// ApplyHeroDeathObjectives routes an authoritative player death through the
// shared objective set without changing NPC completion accounting.
func (e *Zone) ApplyHeroDeathObjectives(
	ctx context.Context, userID uint64, peerGeneration uint64, objectID uint32,
) error {
	if e == nil || ctx == nil || userID == 0 || peerGeneration == 0 || objectID == 0 {
		return errors.New("hero death objective context unavailable")
	}
	e.mu.RLock()
	member, isFound := e.members[userID]
	objective := e.info.Objective
	progress := e.info.ObjectiveProgress
	e.mu.RUnlock()
	if !isFound || member.PeerGeneration != peerGeneration || objective == nil {
		return nil
	}
	killPercent := float64(0)
	if progress != nil {
		killPercent = progress.KillPercent()
	}
	updates, err := objective.ApplyEvent(ctx, sim.LuaObjectiveEvent{
		Kind: sim.LuaObjectiveEventDeath, ObjectID: objectID,
		TargetObjectID: objectID, KillPercent: killPercent,
		PlayerIndex: uint8(member.Slot), TargetTeam: 1,
		IsTargetPlayerControlled: true,
	})
	if err != nil {
		return fmt.Errorf("heroDeathObjective: %w", err)
	}
	e.PublishObjective(updates)
	return nil
}

func (e *Zone) PublishHeroMovement(
	movement zoneprojection.HeroMovement,
	sourceUserID uint64, sourceGeneration uint64,
) {
	if e == nil {
		return
	}
	e.projection.PublishHeroMovement(movement, zoneprojection.Subscriber{
		UserID: sourceUserID, PeerGeneration: sourceGeneration,
	})
}

func (e *Zone) PublishNPCDeath(
	deaths []zonenpc.DeathEvent, sourceUserID uint64, sourceGeneration uint64,
) {
	if e == nil {
		return
	}
	e.projection.PublishNPCDeath(deaths, zoneprojection.Subscriber{
		UserID: sourceUserID, PeerGeneration: sourceGeneration,
	})
}

// RecordNPCDeaths advances generic objective progress from authoritative NPC
// deaths before publishing their presentation events to connected members.
func (e *Zone) RecordNPCDeaths(
	ctx context.Context, deaths []zonenpc.DeathEvent,
	sourceUserID uint64, sourceGeneration uint64,
) error {
	if e == nil || len(deaths) == 0 {
		return nil
	}
	if ctx == nil {
		return errors.New("npc death context unavailable")
	}
	var objectiveErr error
	for _, death := range deaths {
		if death.Kind != zonenpc.DeathHitPoint || death.HitPoint > 0 {
			continue
		}
		progress := e.info.ObjectiveProgress
		objective := e.info.Objective
		if progress == nil || objective == nil {
			break
		}
		npc, isNPCFound := e.info.NPCs.NPC(death.TargetObjectID)
		if !isNPCFound {
			continue
		}
		events, isRecorded := progress.RecordDeath(death.TargetObjectID)
		if !isRecorded && !npc.Plan.IsFixture {
			continue
		}
		if !isRecorded {
			events = []sim.LuaObjectiveEvent{{
				Kind:        sim.LuaObjectiveEventDeath,
				ObjectID:    death.TargetObjectID,
				KillPercent: progress.KillPercent(),
			}}
		}
		for eventIndex, event := range events {
			source := e.objectiveSource(death.SourceObjectID)
			event.TargetObjectID = death.TargetObjectID
			event.SourceObjectID = death.SourceObjectID
			event.TargetAssetID = util.HashID(npc.Plan.NounName)
			event.TargetTeam = 2
			event.SourceTeam = 1
			if source.IsFound {
				event.PlayerIndex = source.PlayerIndex
				event.DamageInteger = int32(source.PlayerIndex)
				event.IsSourcePlayerControlled = source.IsPlayerControlled
			}
			event.IsTargetNPC = !npc.Plan.IsFixture
			event.IsTargetDestructible = npc.Plan.IsFixture
			if npc.Plan.IsFixture {
				event.TargetMarkerID = npc.Plan.LocusID
			}
			updates, err := objective.ApplyEvent(ctx, event)
			if err != nil {
				objectiveErr = fmt.Errorf("npcObjective[%d]: %w", eventIndex, err)
				break
			}
			e.PublishObjective(updates)
		}
		if objectiveErr != nil {
			break
		}
	}
	e.PublishNPCDeath(deaths, sourceUserID, sourceGeneration)
	return objectiveErr
}

func (e *Zone) ResurrectNPC(
	ctx context.Context, objectID uint32, hitPointFraction float32,
) (zonenpc.Snapshot, bool, error) {
	if e == nil || ctx == nil || objectID == 0 || e.NPCs() == nil || e.Death() == nil {
		return zonenpc.Snapshot{}, false, errors.New("npc resurrection unavailable")
	}
	defeated, isFound := e.NPCs().NPC(objectID)
	if !isFound || !defeated.IsDefeated || defeated.Plan.IsFixture {
		return zonenpc.Snapshot{}, false, nil
	}
	event, isRevived, err := e.Death().Revive(ctx, objectID)
	if err != nil {
		return zonenpc.Snapshot{}, false, fmt.Errorf("resurrectDeath: %w", err)
	}
	if !isRevived {
		return zonenpc.Snapshot{}, false, nil
	}
	revived, err := e.NPCs().Resurrect(objectID, hitPointFraction)
	if err != nil {
		return zonenpc.Snapshot{}, false, fmt.Errorf("resurrectNPC: %w", err)
	}
	if e.Horde() != nil {
		err = e.Horde().Revive(revived)
		if err != nil {
			return zonenpc.Snapshot{}, false, fmt.Errorf("resurrectHorde: %w", err)
		}
	}
	if e.Loot() != nil {
		err = e.Loot().SuppressNPCDrops(objectID)
		if err != nil {
			return zonenpc.Snapshot{}, false, fmt.Errorf("resurrectLoot: %w", err)
		}
	}
	e.PublishNPCDeath(event, 0, 0)
	return revived, true, nil
}

func (e *Zone) PublishNPCSpawn(
	spawn zoneprojection.NPCSpawn,
	sourceUserID uint64, sourceGeneration uint64,
) error {
	if e == nil {
		return errors.New("npc spawn zone unavailable")
	}
	if e.info.ObjectiveProgress != nil {
		objectIDs := make([]uint32, 0, len(spawn.Plans))
		for _, plan := range spawn.Plans {
			if plan.IsFixture || plan.IsRewardSuppressed {
				continue
			}
			objectIDs = append(objectIDs, plan.ObjectID)
		}
		err := e.info.ObjectiveProgress.Register(objectIDs)
		if err != nil {
			return fmt.Errorf("npcSpawnObjective: %w", err)
		}
	}
	e.projection.PublishNPCSpawn(spawn, zoneprojection.Subscriber{
		UserID: sourceUserID, PeerGeneration: sourceGeneration,
	})
	return nil
}

func (e *Zone) PublishNPCAction(
	action zonenpc.ActionEvent, sourceUserID uint64, sourceGeneration uint64,
) {
	if e == nil {
		return
	}
	e.projection.PublishNPCAction(action, zoneprojection.Subscriber{
		UserID: sourceUserID, PeerGeneration: sourceGeneration,
	})
}

func (e *Zone) PublishNPCActionAll(action zonenpc.ActionEvent) {
	if e == nil {
		return
	}
	e.projection.PublishNPCActionAll(action)
}

func (e *Zone) PublishNPCForcedMovement(
	movement zonenpc.ForcedMovementEvent,
	sourceUserID uint64,
	sourceGeneration uint64,
) {
	if e == nil {
		return
	}
	e.projection.PublishNPCForcedMovement(
		movement,
		zoneprojection.Subscriber{
			UserID: sourceUserID, PeerGeneration: sourceGeneration,
		},
	)
}

func (e *Zone) DrainProjection(
	userID uint64, peerGeneration uint64,
) []zoneprojection.Event {
	event := e.PeekProjection(userID, peerGeneration)
	if len(event) == 0 {
		return nil
	}
	e.CommitProjection(
		userID, peerGeneration, event[len(event)-1].Sequence,
	)
	return event
}

func (e *Zone) PeekProjection(
	userID uint64, peerGeneration uint64,
) []zoneprojection.Event {
	if e == nil {
		return nil
	}
	return e.projection.Peek(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	})
}

func (e *Zone) IsProjectionBaselineRequired(
	userID uint64, peerGeneration uint64,
) bool {
	if e == nil || e.projection == nil {
		return false
	}
	return e.projection.IsBaselineRequired(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	})
}

func (e *Zone) RequireProjectionBaseline(
	userID uint64, peerGeneration uint64,
) error {
	if e == nil || e.projection == nil {
		return errors.New("campaign projection unavailable")
	}
	err := e.projection.RequireBaseline(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	})
	if err != nil {
		return fmt.Errorf("campaignProjectionBaseline: %w", err)
	}
	return nil
}

func (e *Zone) ProjectionRevision() uint64 {
	if e == nil || e.projection == nil {
		return 0
	}
	return e.projection.Revision()
}

func (e *Zone) ResetProjectionThrough(
	userID uint64, peerGeneration uint64, sequence uint64,
) bool {
	if e == nil || e.projection == nil {
		return false
	}
	return e.projection.ResetThrough(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	}, sequence)
}

func (e *Zone) CommitProjection(
	userID uint64, peerGeneration uint64, sequence uint64,
) bool {
	if e == nil {
		return false
	}
	return e.projection.Commit(zoneprojection.Subscriber{
		UserID: userID, PeerGeneration: peerGeneration,
	}, sequence)
}
