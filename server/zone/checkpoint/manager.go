package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const closeTimeout = 5 * time.Second
const retryInterval = time.Second

// Manager coalesces immutable zone snapshots in memory and persists them off
// the gameplay path. One pending snapshot per zone bounds write pressure.
type Manager struct {
	store  Store
	logger *log.Logger
	wake   chan struct{}
	done   chan struct{}

	storeMu          sync.Mutex
	memberMu         sync.Mutex
	mu               sync.Mutex
	pendingSnapshots map[uint64]Snapshot
	pendingDeletes   map[uint64]struct{}
	discardedZones   map[uint64]struct{}
	declinedMembers  map[uint64]map[uint64]struct{}
	revision         uint64
	isClosing        bool
	closeErr         error
}

func (e *Manager) FindResume(
	ctx context.Context, userID int64,
) (game.ResumeCheckpoint, bool, error) {
	if e == nil || e.store == nil || userID <= 0 {
		return game.ResumeCheckpoint{}, false, nil
	}
	e.mu.Lock()
	var pending Snapshot
	var pendingMember Member
	for zoneID, snapshot := range e.pendingSnapshots {
		if _, isDiscarded := e.discardedZones[zoneID]; isDiscarded {
			continue
		}
		for _, member := range snapshot.Members {
			if member.UserID == uint64(userID) &&
				snapshot.Revision >= pending.Revision {
				pending = snapshot
				pendingMember = member
			}
		}
	}
	e.mu.Unlock()
	if pending.ZoneID != 0 {
		if game.IsTutorialLevel(pending.Level) {
			e.Discard(pending.ZoneID)
			return game.ResumeCheckpoint{}, false, nil
		}
		err := Validate(
			pending, pending.ZoneID, pending.Level, pending.Difficulty,
			uint64(userID),
		)
		if err != nil {
			e.Discard(pending.ZoneID)
			return game.ResumeCheckpoint{}, false, nil
		}
		resume, isFound, err := resumeCheckpoint(pending, pendingMember)
		if err == nil && isFound && e.logger != nil {
			e.logger.Printf(
				"Zone checkpoint resume found in memory zone=%d user=%d revision=%d",
				pending.ZoneID, userID, pending.Revision,
			)
		}
		return resume, isFound, err
	}
	snapshot, isFound, err := e.loadMember(ctx, uint64(userID))
	if err != nil {
		return game.ResumeCheckpoint{}, false, fmt.Errorf("resumeLoad: %w", err)
	}
	if !isFound {
		return game.ResumeCheckpoint{}, false, nil
	}
	if game.IsTutorialLevel(snapshot.Level) {
		e.Discard(snapshot.ZoneID)
		return game.ResumeCheckpoint{}, false, nil
	}
	e.mu.Lock()
	_, isDiscarded := e.discardedZones[snapshot.ZoneID]
	e.mu.Unlock()
	if isDiscarded {
		return game.ResumeCheckpoint{}, false, nil
	}
	err = Validate(
		snapshot, snapshot.ZoneID, snapshot.Level, snapshot.Difficulty,
		uint64(userID),
	)
	if err != nil {
		e.Discard(snapshot.ZoneID)
		return game.ResumeCheckpoint{}, false, nil
	}
	for _, member := range snapshot.Members {
		if member.UserID != uint64(userID) {
			continue
		}
		resume, isFound, resumeErr := resumeCheckpoint(snapshot, member)
		if resumeErr == nil && isFound && e.logger != nil {
			e.logger.Printf(
				"Zone checkpoint resume found on disk zone=%d user=%d revision=%d",
				snapshot.ZoneID, userID, snapshot.Revision,
			)
		}
		return resume, isFound, resumeErr
	}
	return game.ResumeCheckpoint{}, false, nil
}

func resumeCheckpoint(
	snapshot Snapshot, member Member,
) (game.ResumeCheckpoint, bool, error) {
	if snapshot.ZoneID > uint64(^uint32(0)) {
		return game.ResumeCheckpoint{}, false,
			errors.New("resume game id out of range")
	}
	runSeed := snapshot.RunSeed
	if runSeed == 0 {
		// Version-5 checkpoints created before run seeds use their durable,
		// cryptographically random completion identity for a stable migration.
		runSeed = snapshot.CompletionID
	}
	if runSeed == 0 {
		return game.ResumeCheckpoint{}, false, errors.New("resume run seed unavailable")
	}
	return game.ResumeCheckpoint{
		GameID: uint32(snapshot.ZoneID), RunSeed: runSeed, Level: snapshot.Level,
		Difficulty: snapshot.Difficulty, Slot: member.Slot,
		ExpectedPlayerCount: uint16(len(snapshot.Members)),
	}, true, nil
}

func NewManager(store Store, logger *log.Logger) *Manager {
	e := &Manager{
		store: store, logger: logger, wake: make(chan struct{}, 1),
		done: make(chan struct{}), pendingSnapshots: make(map[uint64]Snapshot),
		pendingDeletes:  make(map[uint64]struct{}),
		discardedZones:  make(map[uint64]struct{}),
		declinedMembers: make(map[uint64]map[uint64]struct{}),
		revision:        uint64(time.Now().UnixNano()),
	}
	go e.run()
	return e
}

func (e *Manager) Record(snapshot Snapshot) {
	if e == nil || e.store == nil || snapshot.ZoneID == 0 {
		return
	}
	if game.IsTutorialLevel(snapshot.Level) {
		e.Discard(snapshot.ZoneID)
		return
	}
	snapshot = cloneSnapshot(snapshot)
	e.mu.Lock()
	if e.isClosing {
		e.mu.Unlock()
		return
	}
	if _, isDiscarded := e.discardedZones[snapshot.ZoneID]; isDiscarded {
		e.mu.Unlock()
		return
	}
	snapshot = checkpointWithoutMembers(
		snapshot, e.declinedMembers[snapshot.ZoneID],
	)
	if len(snapshot.Members) == 0 {
		e.mu.Unlock()
		return
	}
	e.revision++
	snapshot.Revision = e.revision
	var validationErr error
	if len(snapshot.Members) == 0 {
		validationErr = errors.New("checkpoint member unavailable")
	} else {
		validationErr = Validate(
			snapshot, snapshot.ZoneID, snapshot.Level, snapshot.Difficulty,
			snapshot.Members[0].UserID,
		)
	}
	if validationErr != nil {
		e.mu.Unlock()
		if e.logger != nil {
			e.logger.Printf(
				"Zone checkpoint capture rejected zone=%d: %v",
				snapshot.ZoneID, validationErr,
			)
		}
		return
	}
	current, isFound := e.pendingSnapshots[snapshot.ZoneID]
	if !isFound || snapshot.Revision >= current.Revision {
		e.pendingSnapshots[snapshot.ZoneID] = snapshot
	}
	select {
	case e.wake <- struct{}{}:
	default:
	}
	e.mu.Unlock()
}

// RemoveMemberNow durably removes one member's resume entitlement while
// retaining the shared checkpoint for every member who still wants to resume.
// Removing the final member retires the checkpoint as a whole.
func (e *Manager) RemoveMemberNow(
	ctx context.Context, zoneID uint64, userID uint64,
) (int, error) {
	if e == nil || e.store == nil || zoneID == 0 || userID == 0 {
		return 0, nil
	}
	if ctx == nil {
		return 0, errors.New("checkpoint member context unavailable")
	}
	e.memberMu.Lock()
	defer e.memberMu.Unlock()
	snapshot, isFound, err := e.Load(ctx, zoneID)
	if err != nil {
		return 0, fmt.Errorf("memberLoad: %w", err)
	}
	if !isFound {
		return 0, nil
	}
	member, isMemberFound := checkpointMember(snapshot, userID)
	if !isMemberFound {
		return len(snapshot.Members), nil
	}
	e.mu.Lock()
	declinedMembers := e.declinedMembers[zoneID]
	if declinedMembers == nil {
		declinedMembers = make(map[uint64]struct{})
		e.declinedMembers[zoneID] = declinedMembers
	}
	declinedMembers[userID] = struct{}{}
	delete(e.pendingSnapshots, zoneID)
	e.mu.Unlock()
	snapshot = checkpointWithoutMembers(
		snapshot, map[uint64]struct{}{userID: {}},
	)
	if len(snapshot.Members) == 0 {
		err = e.DiscardNow(ctx, zoneID)
		if err != nil {
			e.restoreDeclinedMember(zoneID, userID)
			return 0, fmt.Errorf("memberDiscard: %w", err)
		}
		return 0, nil
	}
	e.mu.Lock()
	e.revision++
	snapshot.Revision = e.revision
	e.mu.Unlock()
	snapshot.SavedAt = time.Now().UTC()
	err = Validate(
		snapshot, snapshot.ZoneID, snapshot.Level, snapshot.Difficulty,
		snapshot.Members[0].UserID,
	)
	if err != nil {
		e.restoreDeclinedMember(zoneID, userID)
		return 0, fmt.Errorf("memberValidate: %w", err)
	}
	isSaved, err := e.save(ctx, snapshot)
	if err != nil {
		e.restoreDeclinedMember(zoneID, userID)
		return 0, fmt.Errorf("memberSave: %w", err)
	}
	if !isSaved {
		e.restoreDeclinedMember(zoneID, userID)
		return 0, errors.New("checkpoint member save rejected")
	}
	if e.logger != nil {
		e.logger.Printf(
			"Zone checkpoint member removed zone=%d user=%d slot=%d remaining=%d",
			zoneID, userID, member.Slot, len(snapshot.Members),
		)
	}
	return len(snapshot.Members), nil
}

func (e *Manager) restoreDeclinedMember(zoneID uint64, userID uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	declinedMembers := e.declinedMembers[zoneID]
	delete(declinedMembers, userID)
	if len(declinedMembers) == 0 {
		delete(e.declinedMembers, zoneID)
	}
}

func (e *Manager) Load(ctx context.Context, zoneID uint64) (Snapshot, bool, error) {
	if e == nil || e.store == nil {
		return Snapshot{}, false, nil
	}
	e.mu.Lock()
	if _, isDiscarded := e.discardedZones[zoneID]; isDiscarded {
		e.mu.Unlock()
		return Snapshot{}, false, nil
	}
	pending, isPending := e.pendingSnapshots[zoneID]
	e.mu.Unlock()
	if isPending {
		return cloneSnapshot(pending), true, nil
	}
	snapshot, isFound, err := e.load(ctx, zoneID)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("snapshotLoad: %w", err)
	}
	return snapshot, isFound, nil
}

// Discard asynchronously invalidates both queued and durable state for a
// terminal zone. The gameplay path never waits on SQLite.
func (e *Manager) Discard(zoneID uint64) {
	if e == nil || e.store == nil || zoneID == 0 {
		return
	}
	e.markDiscarded(zoneID)
}

// DiscardNow durably retires a launcher-selected interrupted mission before
// the new client is allowed to start. Gameplay terminal paths use Discard so
// they never wait on SQLite.
func (e *Manager) DiscardNow(ctx context.Context, zoneID uint64) error {
	if e == nil || e.store == nil || zoneID == 0 {
		return nil
	}
	if ctx == nil {
		return errors.New("checkpoint discard context unavailable")
	}
	e.markDiscarded(zoneID)
	err := e.delete(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("checkpointDelete: %w", err)
	}
	e.mu.Lock()
	delete(e.pendingDeletes, zoneID)
	e.mu.Unlock()
	if e.logger != nil {
		e.logger.Printf("Zone checkpoint deleted zone=%d", zoneID)
	}
	return nil
}

func (e *Manager) markDiscarded(zoneID uint64) {
	e.mu.Lock()
	delete(e.pendingSnapshots, zoneID)
	e.discardedZones[zoneID] = struct{}{}
	e.pendingDeletes[zoneID] = struct{}{}
	if !e.isClosing {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
	e.mu.Unlock()
}

func (e *Manager) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	if !e.isClosing {
		e.isClosing = true
		close(e.wake)
	}
	e.mu.Unlock()
	select {
	case <-e.done:
		e.mu.Lock()
		closeErr := e.closeErr
		e.mu.Unlock()
		return closeErr
	case <-time.After(closeTimeout):
		return fmt.Errorf("flushWait: %w", context.DeadlineExceeded)
	}
}

func (e *Manager) run() {
	defer close(e.done)
	retry := time.NewTicker(retryInterval)
	defer retry.Stop()
	for {
		select {
		case _, isOpen := <-e.wake:
			if !isOpen {
				if !e.flushUntil(time.Now().Add(closeTimeout)) {
					e.mu.Lock()
					e.closeErr = errors.New("checkpoint flush incomplete")
					e.mu.Unlock()
				}
				return
			}
			e.flush()
		case <-retry.C:
			e.flush()
		}
	}
}

func (e *Manager) flushUntil(deadline time.Time) bool {
	isFlushed := e.flush()
	for !isFlushed && time.Now().Before(deadline) {
		timer := time.NewTimer(min(retryInterval, time.Until(deadline)))
		<-timer.C
		isFlushed = e.flush()
	}
	return isFlushed
}

// flush drains queued operations until the queue is empty or storage rejects
// one operation. Rejected work is restored to the queue for the periodic or
// shutdown retry path.
func (e *Manager) flush() bool {
	for {
		e.mu.Lock()
		var deletedZoneID uint64
		for zoneID := range e.pendingDeletes {
			deletedZoneID = zoneID
			break
		}
		var snapshot Snapshot
		if deletedZoneID == 0 {
			for zoneID, pending := range e.pendingSnapshots {
				snapshot = pending
				delete(e.pendingSnapshots, zoneID)
				break
			}
		}
		e.mu.Unlock()
		if deletedZoneID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
			err := e.delete(ctx, deletedZoneID)
			cancel()
			if err != nil && e.logger != nil {
				e.logger.Printf(
					"Zone checkpoint delete failed zone=%d: %v",
					deletedZoneID, err,
				)
			}
			if err != nil {
				return false
			}
			e.mu.Lock()
			delete(e.pendingDeletes, deletedZoneID)
			e.mu.Unlock()
			if e.logger != nil {
				e.logger.Printf("Zone checkpoint deleted zone=%d", deletedZoneID)
			}
			continue
		}
		if snapshot.ZoneID == 0 {
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		isSaved, err := e.save(ctx, snapshot)
		cancel()
		if !isSaved && err == nil {
			continue
		}
		if err != nil && e.logger != nil {
			e.logger.Printf(
				"Zone checkpoint save failed zone=%d revision=%d: %v",
				snapshot.ZoneID, snapshot.Revision, err,
			)
		}
		if err != nil {
			e.mu.Lock()
			_, isDiscarded := e.pendingDeletes[snapshot.ZoneID]
			current, isReplaced := e.pendingSnapshots[snapshot.ZoneID]
			if !isDiscarded && (!isReplaced || current.Revision < snapshot.Revision) {
				e.pendingSnapshots[snapshot.ZoneID] = snapshot
			}
			e.mu.Unlock()
			return false
		}
		if e.logger != nil {
			e.logger.Printf(
				"Zone checkpoint saved zone=%d revision=%d reason=%s members=%d",
				snapshot.ZoneID, snapshot.Revision, snapshot.Reason,
				len(snapshot.Members),
			)
		}
	}
}

func (e *Manager) save(
	ctx context.Context, snapshot Snapshot,
) (bool, error) {
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	e.mu.Lock()
	_, isDiscarded := e.discardedZones[snapshot.ZoneID]
	e.mu.Unlock()
	if isDiscarded {
		return false, nil
	}
	err := e.store.Save(ctx, snapshot)
	if err != nil {
		return false, fmt.Errorf("storeSave: %w", err)
	}
	return true, nil
}

func (e *Manager) load(
	ctx context.Context, zoneID uint64,
) (Snapshot, bool, error) {
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	snapshot, isFound, err := e.store.Load(ctx, zoneID)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("storeLoad: %w", err)
	}
	return snapshot, isFound, nil
}

func (e *Manager) loadMember(
	ctx context.Context, userID uint64,
) (Snapshot, bool, error) {
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	snapshot, isFound, err := e.store.LoadMember(ctx, userID)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("storeMember: %w", err)
	}
	return snapshot, isFound, nil
}

func (e *Manager) delete(ctx context.Context, zoneID uint64) error {
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	err := e.store.Delete(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("storeDelete: %w", err)
	}
	return nil
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Members = append([]Member(nil), snapshot.Members...)
	snapshot.Heroes = append([]Hero(nil), snapshot.Heroes...)
	snapshot.Squads = append([]Squad(nil), snapshot.Squads...)
	snapshot.NPCs = append([]NPC(nil), snapshot.NPCs...)
	for index := range snapshot.NPCs {
		snapshot.NPCs[index].State.Plan = snapshot.NPCs[index].State.Plan.Clone()
	}
	snapshot.Crystals = append([]Crystal(nil), snapshot.Crystals...)
	snapshot.ExperienceAwards = append(
		[]ExperienceAward(nil), snapshot.ExperienceAwards...,
	)
	for index := range snapshot.ExperienceAwards {
		memberAwards := snapshot.ExperienceAwards[index].MemberAwards
		snapshot.ExperienceAwards[index].MemberAwards = make(
			map[uint64]uint32, len(memberAwards),
		)
		for userID, experience := range memberAwards {
			snapshot.ExperienceAwards[index].MemberAwards[userID] = experience
		}
	}
	snapshot.Objectives = append(
		[]sim.ObjectiveSnapshot(nil), snapshot.Objectives...,
	)
	snapshot.ScriptUses = append(
		[]game.CampaignScriptUse(nil), snapshot.ScriptUses...,
	)
	snapshot.ClearedSpawnGroupIDs = append(
		[]uint32(nil), snapshot.ClearedSpawnGroupIDs...,
	)
	return snapshot
}

func checkpointMember(snapshot Snapshot, userID uint64) (Member, bool) {
	for _, member := range snapshot.Members {
		if member.UserID == userID {
			return member, true
		}
	}
	return Member{}, false
}

func checkpointWithoutMembers(
	snapshot Snapshot, removedMembers map[uint64]struct{},
) Snapshot {
	if len(removedMembers) == 0 {
		return snapshot
	}
	snapshot = cloneSnapshot(snapshot)
	removedSlots := make(map[uint16]struct{}, len(removedMembers))
	members := make([]Member, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		if _, isRemoved := removedMembers[member.UserID]; isRemoved {
			removedSlots[member.Slot] = struct{}{}
			continue
		}
		members = append(members, member)
	}
	snapshot.Members = members
	heroes := make([]Hero, 0, len(snapshot.Heroes))
	for _, hero := range snapshot.Heroes {
		if _, isRemoved := removedMembers[hero.UserID]; !isRemoved {
			heroes = append(heroes, hero)
		}
	}
	snapshot.Heroes = heroes
	squads := make([]Squad, 0, len(snapshot.Squads))
	for _, restoredSquad := range snapshot.Squads {
		if _, isRemoved := removedMembers[restoredSquad.UserID]; !isRemoved {
			squads = append(squads, restoredSquad)
		}
	}
	snapshot.Squads = squads
	crystals := make([]Crystal, 0, len(snapshot.Crystals))
	for _, crystal := range snapshot.Crystals {
		if _, isRemoved := removedMembers[crystal.UserID]; !isRemoved {
			crystals = append(crystals, crystal)
		}
	}
	snapshot.Crystals = crystals
	experienceAwards := make(
		[]ExperienceAward, 0, len(snapshot.ExperienceAwards),
	)
	for _, award := range snapshot.ExperienceAwards {
		for userID := range removedMembers {
			delete(award.MemberAwards, userID)
		}
		if len(award.MemberAwards) != 0 {
			experienceAwards = append(experienceAwards, award)
		}
	}
	snapshot.ExperienceAwards = experienceAwards
	for objectiveIndex := range snapshot.Objectives {
		for slot := range removedSlots {
			snapshot.Objectives[objectiveIndex].State[slot] = 0
			snapshot.Objectives[objectiveIndex].Token[slot] = [3]int32{}
		}
	}
	return snapshot
}
