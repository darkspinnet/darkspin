package game

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// ResumeCheckpoint is the durable game membership needed to recreate a Blaze
// game before its zone state is restored by gameplay.
type ResumeCheckpoint struct {
	GameID              uint32
	Level               string
	Difficulty          uint32
	Slot                uint16
	ExpectedPlayerCount uint16
}

type ResumeSource interface {
	FindResume(context.Context, int64) (ResumeCheckpoint, bool, error)
}

type ResumeRepository interface {
	ResumeSource
	Discard(uint64)
}

// SessionState is the Blaze-visible lifecycle of a game session.
type SessionState uint32

// Mode identifies the Game gameplay mode selected for an instance.
type Mode uint32

const (
	StateNew          SessionState = 0
	StateInitializing SessionState = 1
	StatePreGame      SessionState = 0x82
	StateInGame       SessionState = 0x83
	StatePostGame     SessionState = 4
	StateMigrating    SessionState = 5
	StateDestructing  SessionState = 6
	StateResettable   SessionState = 7
	StateReplaySetup  SessionState = 8
)

const (
	// ModeChain is the client's campaign chain gameplay mode. Its wire value is
	// zero in the Blaze game configuration; the later RakNet GameType is 2.
	ModeChain Mode = 0

	// ModeTutorial is the client's first-run gameplay mode.
	ModeTutorial Mode = 1

	// ModeArena is the client's competitive Arena gameplay mode.
	ModeArena Mode = 3

	// InitialChainLevel is the first authored build-103 campaign level. The
	// client selects 1-1 without including its asset name in the reset request.
	InitialChainLevel = "zelems_1"

	// TutorialLevel is the retail client's first tutorial map.
	TutorialLevel = "Darkspore_Tutorial_cryos_1_v2"

	// TutorialDirectorLevel is the canonical content name returned for the
	// retail tutorial alias requested by the client.
	TutorialDirectorLevel = "Darkspore_Tutorial_cryos_1"

	// MaxGamePlayers is the executable-supported capacity of one game instance.
	MaxGamePlayers uint16 = 4
)

func IsTutorialLevel(level string) bool {
	return strings.EqualFold(level, TutorialLevel) ||
		strings.EqualFold(level, TutorialDirectorLevel)
}

// Info is the mutable replicated game configuration.
type Info struct {
	Attributes          map[string]string
	Capacity            []uint32
	Name                string
	Level               string
	Type                string
	Mode                Mode
	Version             string
	UUID                string
	Settings            uint64
	State               SessionState
	NetworkTopology     uint32
	PresenceMode        uint32
	VoIPTopology        uint32
	MaxPlayers          uint16
	QueueCapacity       uint16
	Port                uint16
	HostNetwork         NetworkPair
	TeamCapacity        uint16
	TeamIndex           uint16
	PlaygroupID         string
	ExpectedPlayerCount uint16
	IsResettable        bool
	IsIgnored           bool
	IsWarped            bool
}

// NetworkEndpoint is one gameplay address advertised through Blaze.
type NetworkEndpoint struct {
	IP   uint32
	Port uint16
}

// NetworkPair contains the external and internal routes to a gameplay host.
type NetworkPair struct {
	External NetworkEndpoint
	Internal NetworkEndpoint
}

// Instance owns one game and its joined users.
type Instance struct {
	mu                         sync.RWMutex
	ID                         uint32
	Info                       Info
	CreatedAt                  time.Time
	StartedAt                  time.Time
	players                    map[int64]*sporenet.User
	slots                      map[int64]uint16
	teams                      map[int64]uint16
	hostUserID                 int64
	readyPlayers               map[int64]struct{}
	joinPublications           map[int64]struct{}
	launchHandoffRemovals      map[int64]struct{}
	isLaunchHandoffReserved    bool
	isStartPublicationReserved bool
	isStartPublished           bool
	isAdmissionReserved        bool
	isCheckpointRestore        bool
	tutorialCompletions        map[int64]struct{}
	effectPreviews             map[int64]EffectPreview
	resourceCommands           map[int64]PlayerResourceCommand
	eventCommands              map[int64][]PlayerEventCommand
	levelUpdates               map[int64]PlayerLevelUpdate
	dnaUpdates                 map[int64]PlayerDNAUpdate
	itemPresentations          map[int64]sporenet.Part
}

// PlayerLevelUpdate is a pending live reflection of persisted account progress.
type PlayerLevelUpdate struct {
	Level uint32
	XP    float32
}

// PlayerDNAUpdate is a pending live reflection of persisted account currency.
type PlayerDNAUpdate struct {
	DNA uint32
}

// EffectPreview is one validated developer presentation request.
type EffectPreview struct {
	Asset uint32
}

// PlayerResourceCommand is one pending gameplay-owned developer resource mutation.
type PlayerResourceCommand struct {
	Damage         float32
	PowerReduction float32
	IsHeal         bool
	IsPowerFill    bool
}

// PlayerEventCommand is one allowlisted gameplay-owned developer event.
type PlayerEventCommand struct {
	Name         string
	NounName     string
	Position     Vec3
	TargetUserID uint64
}

func (g *Instance) AddPlayer(user *sporenet.User) bool {
	if user == nil {
		return false
	}
	g.mu.Lock()
	existing, isFound := g.players[user.Account.ID]
	if isFound && existing == user {
		g.mu.Unlock()
		return true
	}
	if isFound {
		if !user.ClaimGame(g.ID) {
			g.mu.Unlock()
			return false
		}
		g.players[user.Account.ID] = user
		g.mu.Unlock()
		if existing != nil {
			existing.ReleaseGame(g.ID)
		}
		return true
	}
	if g.Info.State == StateInGame || g.Info.State == StatePostGame ||
		g.Info.State == StateDestructing {
		g.mu.Unlock()
		return false
	}
	if g.Info.MaxPlayers != 0 && len(g.players) >= int(g.Info.MaxPlayers) {
		g.mu.Unlock()
		return false
	}
	slot, isFound := g.nextPlayerSlot()
	if !isFound {
		g.mu.Unlock()
		return false
	}
	if !user.ClaimGame(g.ID) {
		g.mu.Unlock()
		return false
	}
	g.players[user.Account.ID] = user
	g.slots[user.Account.ID] = slot
	hostSlot, isHostFound := g.slots[g.hostUserID]
	if !isHostFound || slot < hostSlot {
		g.hostUserID = user.Account.ID
	}
	g.mu.Unlock()
	return true
}

func (g *Instance) AddPlayerAtSlot(user *sporenet.User, slot uint16) error {
	return g.addPlayerAtSlot(user, slot, false)
}

func (g *Instance) restorePlayerAtSlot(user *sporenet.User, slot uint16) error {
	return g.addPlayerAtSlot(user, slot, true)
}

func (g *Instance) addPlayerAtSlot(
	user *sporenet.User, slot uint16, isCheckpointRestore bool,
) error {
	if g == nil || user == nil {
		return errors.New("game member invalid")
	}
	g.mu.Lock()
	if existingSlot, isFound := g.slots[user.Account.ID]; isFound {
		if existingSlot != slot {
			g.mu.Unlock()
			return errors.New("game member slot mismatch")
		}
		existing := g.players[user.Account.ID]
		if existing == user {
			g.mu.Unlock()
			return nil
		}
		if !user.ClaimGame(g.ID) {
			g.mu.Unlock()
			return errors.New("game member already active")
		}
		g.players[user.Account.ID] = user
		g.mu.Unlock()
		if existing != nil {
			existing.ReleaseGame(g.ID)
		}
		return nil
	}
	if !isCheckpointRestore && (g.Info.State == StateInGame ||
		g.Info.State == StatePostGame || g.Info.State == StateDestructing) {
		g.mu.Unlock()
		return errors.New("game member admission closed")
	}
	if g.Info.MaxPlayers != 0 && slot >= g.Info.MaxPlayers {
		g.mu.Unlock()
		return errors.New("game member slot out of range")
	}
	for _, occupiedSlot := range g.slots {
		if occupiedSlot == slot {
			g.mu.Unlock()
			return errors.New("game member slot occupied")
		}
	}
	if !user.ClaimGame(g.ID) {
		g.mu.Unlock()
		return errors.New("game member already active")
	}
	g.players[user.Account.ID] = user
	g.slots[user.Account.ID] = slot
	hostSlot, isHostFound := g.slots[g.hostUserID]
	if !isHostFound || slot < hostSlot {
		g.hostUserID = user.Account.ID
	}
	g.mu.Unlock()
	return nil
}

// IsCheckpointRestore reports that the game shell represents a durable zone
// rather than a new campaign waiting for chain selection and countdown.
func (g *Instance) IsCheckpointRestore() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	isCheckpointRestore := g.isCheckpointRestore
	g.mu.RUnlock()
	return isCheckpointRestore
}

// IsWarped reports whether the game was created from a consumed developer warp.
func (g *Instance) IsWarped() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	isWarped := g.Info.IsWarped
	g.mu.RUnlock()
	return isWarped
}

// SetExpectedPlayerCount records the frozen admission size projected by the
// client's ExpectedPlayerCount game attribute. A missing value remains a solo
// compatible one-member barrier.
func (g *Instance) SetExpectedPlayerCount(count uint16) {
	if g == nil {
		return
	}
	if count > MaxGamePlayers {
		count = MaxGamePlayers
	}
	g.mu.Lock()
	g.Info.ExpectedPlayerCount = count
	if g.Info.Attributes != nil {
		g.Info.Attributes["ExpectedPlayerCount"] = fmt.Sprintf("%d", count)
	}
	g.mu.Unlock()
}

// MarkPlayerReady admits one joined member to the pre-game barrier. The first
// caller that observes the complete frozen roster performs the sole transition
// to StateInGame; later calls are idempotent.
func (g *Instance) MarkPlayerReady(id int64) (
	isStarted bool, isPublicationPending bool,
) {
	if g == nil {
		return false, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false, false
	}
	if g.readyPlayers == nil {
		g.readyPlayers = make(map[int64]struct{})
	}
	g.readyPlayers[id] = struct{}{}
	return g.tryStartReadyPlayersLocked()
}

// FinalizePreGame commits the Blaze setup boundary without claiming that the
// host has completed its independent network-mesh transition.
func (g *Instance) FinalizePreGame() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch g.Info.State {
	case StateNew, StateInitializing:
		g.Info.State = StatePreGame
		return true
	case StatePreGame:
		return true
	default:
		return false
	}
}

// TryStartReadyPlayers re-evaluates mesh readiness after FinalizePreGame. This
// covers clients that publish their genuine mesh state before the host sends
// FinalizeGameCreation without treating finalization itself as readiness.
func (g *Instance) TryStartReadyPlayers() (
	isStarted bool, isPublicationPending bool,
) {
	if g == nil {
		return false, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tryStartReadyPlayersLocked()
}

func (g *Instance) tryStartReadyPlayersLocked() (
	isStarted bool, isPublicationPending bool,
) {
	if g.Info.State == StateInGame {
		if g.isStartPublished || g.isStartPublicationReserved {
			return true, false
		}
		g.isStartPublicationReserved = true
		return true, true
	}
	if g.Info.State != StatePreGame {
		return false, false
	}
	expectedPlayerCount := int(g.Info.ExpectedPlayerCount)
	if expectedPlayerCount == 0 {
		expectedPlayerCount = 1
	}
	if len(g.players) < expectedPlayerCount || len(g.readyPlayers) < expectedPlayerCount {
		return false, false
	}
	for playerID := range g.players {
		if _, isReady := g.readyPlayers[playerID]; !isReady {
			return false, false
		}
	}
	g.Info.State = StateInGame
	if g.StartedAt.IsZero() {
		g.StartedAt = time.Now()
	}
	g.isStartPublicationReserved = true
	return true, true
}

// ReservePlayerJoinPublication elects the completed ready-barrier publication
// to send one player's connected and join-complete notifications. The
// reservation remains after success so repeated client updates stay idempotent.
func (g *Instance) ReservePlayerJoinPublication(id int64) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.joinPublications == nil {
		g.joinPublications = make(map[int64]struct{})
	}
	if _, isFound := g.joinPublications[id]; isFound {
		return false
	}
	g.joinPublications[id] = struct{}{}
	return true
}

// CancelPlayerJoinPublication reopens a failed notification boundary so the
// next mesh update can retry the complete transition.
func (g *Instance) CancelPlayerJoinPublication(id int64) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.joinPublications, id)
	g.mu.Unlock()
}

// MarkStartPublished closes the retryable Blaze start-publication boundary
// after every roster notification has been queued successfully.
func (g *Instance) MarkStartPublished() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.Info.State == StateInGame {
		g.isStartPublicationReserved = false
		g.isStartPublished = true
	}
	g.mu.Unlock()
}

// CancelStartPublication lets a later ready or state request retry after the
// elected publisher could not queue the complete roster transition.
func (g *Instance) CancelStartPublication() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if !g.isStartPublished {
		g.isStartPublicationReserved = false
	}
	g.mu.Unlock()
}

// HostUserID returns the stable Blaze host selected when the first admitted
// member joined the game.
func (g *Instance) HostUserID() int64 {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	hostUserID := g.hostUserID
	g.mu.RUnlock()
	return hostUserID
}

// IsStarted reports whether the complete admitted roster crossed the pre-game
// ready barrier.
func (g *Instance) IsStarted() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	isStarted := g.Info.State == StateInGame
	g.mu.RUnlock()
	return isStarted
}

// IsGameplayJoinable reports whether Blaze has finalized the game far enough
// for an admitted member to establish its RakNet session. Build 103 connects
// gameplay while the shared game is still pre-game; the later ready barrier
// owns the transition to StateInGame.
func (e *Instance) IsGameplayJoinable() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	isJoinable := e.Info.State == StatePreGame || e.Info.State == StateInGame
	e.mu.RUnlock()
	return isJoinable
}

// ParticipantCount returns the frozen combat-scaling cardinality. An explicit
// expected count wins over the current presence count so disconnect and rejoin
// cannot rescale an already-created zone.
func (g *Instance) ParticipantCount() uint16 {
	if g == nil {
		return 1
	}
	g.mu.RLock()
	count := g.Info.ExpectedPlayerCount
	if count == 0 {
		count = uint16(len(g.players))
	}
	g.mu.RUnlock()
	if count == 0 {
		return 1
	}
	return min(count, MaxGamePlayers)
}

// SetMaxPlayers applies the Game game-instance player limit.
func (g *Instance) SetMaxPlayers(requested uint16) {
	if requested == 0 || requested > MaxGamePlayers {
		requested = MaxGamePlayers
	}
	g.mu.Lock()
	if requested < uint16(len(g.players)) {
		requested = uint16(len(g.players))
	}
	g.Info.MaxPlayers = requested
	g.mu.Unlock()
}

func (g *Instance) nextPlayerSlot() (uint16, bool) {
	limit := g.Info.MaxPlayers
	if limit == 0 {
		limit = ^uint16(0)
	}
	used := make(map[uint16]struct{}, len(g.slots))
	for _, slot := range g.slots {
		used[slot] = struct{}{}
	}
	for slot := uint16(0); slot < limit; slot++ {
		if _, isFound := used[slot]; !isFound {
			return slot, true
		}
	}
	return 0, false
}

// PlayerSlot returns the stable game-local slot assigned to a member.
func (g *Instance) PlayerSlot(id int64) (uint16, bool) {
	slot, _, isFound := g.PlayerBinding(id)
	return slot, isFound
}

// AssignPlayerTeam binds one admitted member to a stable one-based team.
func (e *Instance) AssignPlayerTeam(id int64, team uint16) bool {
	if e == nil || e.Info.TeamCapacity == 0 || team == 0 || team > e.Info.TeamCapacity {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.players[id]; !isFound {
		return false
	}
	if e.teams == nil {
		e.teams = make(map[int64]uint16)
	}
	e.teams[id] = team
	return true
}

// PlayerTeam returns the stable team assigned to an admitted member.
func (e *Instance) PlayerTeam(id int64) (uint16, bool) {
	if e == nil {
		return 0, false
	}
	e.mu.RLock()
	team, isFound := e.teams[id]
	e.mu.RUnlock()
	return team, isFound
}

// HasPlayer reports whether the user is already part of the frozen game roster.
func (g *Instance) HasPlayer(id int64) bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	_, isFound := g.players[id]
	g.mu.RUnlock()
	return isFound
}

// ReserveAdmission closes this game to users outside its frozen roster.
func (e *Instance) ReserveAdmission() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.isAdmissionReserved = true
	e.mu.Unlock()
}

// IsAdmissionReserved reports whether only the frozen roster may join.
func (e *Instance) IsAdmissionReserved() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	isReserved := e.isAdmissionReserved
	e.mu.RUnlock()
	return isReserved
}

// PlayerBinding snapshots one member's slot and the complete occupied-slot mask.
func (g *Instance) PlayerBinding(id int64) (uint16, uint32, bool) {
	g.mu.RLock()
	slot, isFound := g.slots[id]
	mask := g.playerMaskLocked()
	g.mu.RUnlock()
	return slot, mask, isFound
}

// PlayerMask returns the build-103 bitset for the instance's occupied slots.
func (g *Instance) PlayerMask() uint32 {
	g.mu.RLock()
	mask := g.playerMaskLocked()
	g.mu.RUnlock()
	return mask
}

// ReserveLaunchHandoffRemovals records the one guest-side Blaze removal that
// build 103 emits while switching an admitted co-op roster into RakNet play.
func (e *Instance) ReserveLaunchHandoffRemovals() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isLaunchHandoffReserved || !e.isAdmissionReserved || len(e.players) <= 1 {
		return
	}
	e.isLaunchHandoffReserved = true
	if e.launchHandoffRemovals == nil {
		e.launchHandoffRemovals = make(map[int64]struct{}, len(e.players)-1)
	}
	for playerID := range e.players {
		if playerID == e.hostUserID {
			continue
		}
		e.launchHandoffRemovals[playerID] = struct{}{}
	}
}

// ConsumeLaunchHandoffRemoval accepts one reserved guest transition without
// releasing that guest from the authoritative game roster.
func (e *Instance) ConsumeLaunchHandoffRemoval(id int64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, isReserved := e.launchHandoffRemovals[id]
	if !isReserved {
		return false
	}
	delete(e.launchHandoffRemovals, id)
	return true
}

func (g *Instance) playerMaskLocked() uint32 {
	var mask uint32
	for _, slot := range g.slots {
		if slot < 32 {
			mask |= uint32(1) << slot
		}
	}
	return mask
}

func (g *Instance) RemovePlayer(id int64) {
	g.mu.Lock()
	user := g.players[id]
	delete(g.players, id)
	delete(g.slots, id)
	delete(g.teams, id)
	delete(g.readyPlayers, id)
	delete(g.joinPublications, id)
	delete(g.launchHandoffRemovals, id)
	delete(g.tutorialCompletions, id)
	delete(g.effectPreviews, id)
	delete(g.resourceCommands, id)
	delete(g.eventCommands, id)
	delete(g.levelUpdates, id)
	delete(g.dnaUpdates, id)
	delete(g.itemPresentations, id)
	if g.hostUserID == id {
		g.hostUserID = 0
		var hostSlot uint16
		for playerID, slot := range g.slots {
			if g.hostUserID == 0 || slot < hostSlot {
				g.hostUserID = playerID
				hostSlot = slot
			}
		}
	}
	g.mu.Unlock()
	if user != nil {
		user.ReleaseGame(g.ID)
	}
}

// RequestPlayerLevelUpdate replaces the pending live progression reflection.
func (g *Instance) RequestPlayerLevelUpdate(id int64, update PlayerLevelUpdate) bool {
	if update.Level == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.levelUpdates == nil {
		g.levelUpdates = make(map[int64]PlayerLevelUpdate)
	}
	g.levelUpdates[id] = update
	return true
}

// ConsumePlayerLevelUpdate returns and clears one pending progression reflection.
func (g *Instance) ConsumePlayerLevelUpdate(id int64) (PlayerLevelUpdate, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	update, isFound := g.levelUpdates[id]
	if isFound {
		delete(g.levelUpdates, id)
	}
	return update, isFound
}

// RequestPlayerDNAUpdate replaces the pending live account-currency reflection.
func (g *Instance) RequestPlayerDNAUpdate(id int64, update PlayerDNAUpdate) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.dnaUpdates == nil {
		g.dnaUpdates = make(map[int64]PlayerDNAUpdate)
	}
	g.dnaUpdates[id] = update
	return true
}

// ConsumePlayerDNAUpdate returns and clears one pending currency reflection.
func (g *Instance) ConsumePlayerDNAUpdate(id int64) (PlayerDNAUpdate, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	update, isFound := g.dnaUpdates[id]
	if isFound {
		delete(g.dnaUpdates, id)
	}
	return update, isFound
}

// RequestItemPresentation queues the latest persisted item for live pickup presentation.
func (g *Instance) RequestItemPresentation(id int64, part sporenet.Part) bool {
	if part.ID == 0 || part.ReferenceID == 0 || part.RigblockAssetHash == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.itemPresentations == nil {
		g.itemPresentations = make(map[int64]sporenet.Part)
	}
	g.itemPresentations[id] = part
	return true
}

// ConsumeItemPresentation returns and clears one pending live item presentation.
func (g *Instance) ConsumeItemPresentation(id int64) (sporenet.Part, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	part, isFound := g.itemPresentations[id]
	if isFound {
		delete(g.itemPresentations, id)
	}
	return part, isFound
}

// RequestEffectPreview replaces the pending one-shot presentation asset for a
// joined player. Gameplay binds it to that player's currently deployed hero.
func (g *Instance) RequestEffectPreview(id int64, preview EffectPreview) bool {
	if preview.Asset == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.effectPreviews == nil {
		g.effectPreviews = make(map[int64]EffectPreview)
	}
	g.effectPreviews[id] = preview
	return true
}

// ConsumeEffectPreview returns and clears one pending effect preview.
func (g *Instance) ConsumeEffectPreview(id int64) (EffectPreview, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	preview, isFound := g.effectPreviews[id]
	if isFound {
		delete(g.effectPreviews, id)
	}
	return preview, isFound
}

// RequestPlayerResourceCommand replaces the pending resource mutation for a joined player.
func (g *Instance) RequestPlayerResourceCommand(id int64, command PlayerResourceCommand) bool {
	operationCount := 0
	if command.Damage > 0 {
		operationCount++
	}
	if command.PowerReduction > 0 {
		operationCount++
	}
	if command.IsHeal {
		operationCount++
	}
	if command.IsPowerFill {
		operationCount++
	}
	if operationCount != 1 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.resourceCommands == nil {
		g.resourceCommands = make(map[int64]PlayerResourceCommand)
	}
	g.resourceCommands[id] = command
	return true
}

// ConsumePlayerResourceCommand returns and clears one pending resource mutation.
func (g *Instance) ConsumePlayerResourceCommand(id int64) (PlayerResourceCommand, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	command, isFound := g.resourceCommands[id]
	if isFound {
		delete(g.resourceCommands, id)
	}
	return command, isFound
}

// RequestPlayerEventCommand appends one pending developer event for a joined player.
func (g *Instance) RequestPlayerEventCommand(id int64, command PlayerEventCommand) bool {
	isGoto := command.Name == "goto" && isFinitePlayerEventPosition(command.Position)
	isFollow := command.Name == "follow" && command.TargetUserID != 0 &&
		command.TargetUserID != uint64(id)
	isSpawn := command.Name == "spawn" && isValidPlayerEventNoun(command.NounName)
	if command.Name != "security-next" && command.Name != "boss-start" &&
		command.Name != "boss-complete" && command.Name != "kill" &&
		command.Name != "reset" && command.Name != "victory" &&
		command.Name != "defeat" && command.Name != "recap" && !isGoto && !isFollow &&
		command.Name != "ai" && !isSpawn {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if isSpawn && !g.Info.IsWarped {
		return false
	}
	if command.Name == "ai" && (len(g.players) < 2 ||
		(g.Info.Mode != ModeChain && g.Info.Mode != ModeArena)) {
		return false
	}
	if isFollow {
		_, isTargetFound := g.players[int64(command.TargetUserID)]
		if !isTargetFound {
			return false
		}
	}
	if g.eventCommands == nil {
		g.eventCommands = make(map[int64][]PlayerEventCommand)
	}
	g.eventCommands[id] = append(g.eventCommands[id], command)
	return true
}

func isFinitePlayerEventPosition(position Vec3) bool {
	return !math.IsNaN(float64(position.X)) && !math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) && !math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) && !math.IsInf(float64(position.Z), 0)
}

func isValidPlayerEventNoun(nounName string) bool {
	const nounSuffix = ".Noun"
	if len(nounName) <= len(nounSuffix) || len(nounName) > 128 ||
		!strings.EqualFold(nounName[len(nounName)-len(nounSuffix):], nounSuffix) {
		return false
	}
	for index := 0; index < len(nounName)-len(nounSuffix); index++ {
		character := nounName[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	return true
}

// ConsumePlayerEventCommand returns and clears one pending developer event.
// Kill and spawn remain queued until the caller can schedule their autonomous
// publications.
func (g *Instance) ConsumePlayerEventCommand(
	id int64, isScheduleAvailable bool,
) (PlayerEventCommand, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	commands := g.eventCommands[id]
	if len(commands) == 0 {
		return PlayerEventCommand{}, false
	}
	command := commands[0]
	if (command.Name == "kill" || command.Name == "spawn") && !isScheduleAvailable {
		return PlayerEventCommand{}, false
	}
	if len(commands) == 1 {
		delete(g.eventCommands, id)
	} else {
		g.eventCommands[id] = commands[1:]
	}
	return command, true
}

func (g *Instance) Players() []*sporenet.User {
	g.mu.RLock()
	players := make([]*sporenet.User, 0, len(g.players))
	for _, player := range g.players {
		players = append(players, player)
	}
	sort.Slice(players, func(left int, right int) bool {
		return g.slots[players[left].Account.ID] < g.slots[players[right].Account.ID]
	})
	g.mu.RUnlock()
	return players
}

// MarkTutorialComplete records that gameplay reached the authored terminal
// boundary for a joined player. Durable account mutation remains separate and
// occurs only when that player accepts RETURN TO SHIP.
func (g *Instance) MarkTutorialComplete(id int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if g.tutorialCompletions == nil {
		g.tutorialCompletions = make(map[int64]struct{})
	}
	g.tutorialCompletions[id] = struct{}{}
	return true
}

func (g *Instance) IsTutorialComplete(id int64) bool {
	g.mu.RLock()
	_, isFound := g.tutorialCompletions[id]
	g.mu.RUnlock()
	return isFound
}

// RollbackTutorialComplete releases a tentative gameplay completion marker
// when the terminal presentation could not be scheduled.
func (g *Instance) RollbackTutorialComplete(id int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, isFound := g.players[id]; !isFound {
		return false
	}
	if _, isFound := g.tutorialCompletions[id]; !isFound {
		return false
	}
	delete(g.tutorialCompletions, id)
	return true
}

// SetState changes the Blaze-visible lifecycle without bypassing instance
// synchronization used by concurrent co-op members.
func (g *Instance) SetState(state SessionState) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.Info.State = state
	g.mu.Unlock()
}

// Manager owns active game instances.
type Manager struct {
	mu                    sync.RWMutex
	nextID                uint32
	hostNetwork           NetworkPair
	chainLevels           []string
	pendingCampaignWarps  map[int64]string
	games                 map[uint32]*Instance
	removalObserver       func(uint32)
	memberRemovalObserver func(uint32, uint64)
	memberResumePolicy    MemberResumePolicy
}

func NewManager() *Manager {
	return &Manager{
		nextID: 1, games: make(map[uint32]*Instance),
		pendingCampaignWarps: make(map[int64]string),
	}
}

func (m *Manager) Create() *Instance {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	hostNetwork := m.hostNetwork
	instance := &Instance{
		ID: id, CreatedAt: time.Now(), players: make(map[int64]*sporenet.User),
		slots: make(map[int64]uint16), teams: make(map[int64]uint16),
		readyPlayers:        make(map[int64]struct{}),
		joinPublications:    make(map[int64]struct{}),
		tutorialCompletions: make(map[int64]struct{}),
		Info: Info{
			Attributes: make(map[string]string), State: StateInitializing,
			Version: "5.3.0.127", UUID: "71bc4bdb-82ec-494d-8d75-ca5123b827ac",
			MaxPlayers: MaxGamePlayers, HostNetwork: hostNetwork,
		},
	}
	m.games[id] = instance
	m.mu.Unlock()
	return instance
}

// Restore recreates the Blaze-visible shell for a durable zone checkpoint.
// Gameplay restores the richer world state after RakNet identity binding.
func (m *Manager) Restore(
	checkpoint ResumeCheckpoint, user *sporenet.User,
) (*Instance, error) {
	isTutorial := strings.EqualFold(checkpoint.Level, TutorialLevel) ||
		strings.EqualFold(checkpoint.Level, TutorialDirectorLevel)
	isDifficultyValid := isTutorial && checkpoint.Difficulty == 0 ||
		!isTutorial && checkpoint.Difficulty > 0
	if m == nil || user == nil || checkpoint.GameID == 0 ||
		checkpoint.Level == "" || !isDifficultyValid ||
		checkpoint.Slot >= MaxGamePlayers ||
		checkpoint.ExpectedPlayerCount == 0 ||
		checkpoint.ExpectedPlayerCount > MaxGamePlayers ||
		isTutorial && checkpoint.ExpectedPlayerCount != 1 {
		return nil, errors.New("game restore invalid")
	}
	m.mu.Lock()
	instance := m.games[checkpoint.GameID]
	isCreated := false
	if instance == nil {
		mode := ModeChain
		if isTutorial {
			mode = ModeTutorial
		}
		instance = &Instance{
			ID: checkpoint.GameID, CreatedAt: time.Now(),
			players: make(map[int64]*sporenet.User), slots: make(map[int64]uint16),
			teams:               make(map[int64]uint16),
			readyPlayers:        make(map[int64]struct{}),
			joinPublications:    make(map[int64]struct{}),
			tutorialCompletions: make(map[int64]struct{}),
			Info: Info{
				Attributes: map[string]string{
					"SelectedDifficulty":  fmt.Sprintf("%d", checkpoint.Difficulty),
					"ExpectedPlayerCount": fmt.Sprintf("%d", checkpoint.ExpectedPlayerCount),
				},
				State: StateInitializing, Version: "5.3.0.127",
				UUID:       "71bc4bdb-82ec-494d-8d75-ca5123b827ac",
				MaxPlayers: MaxGamePlayers, HostNetwork: m.hostNetwork,
				Mode: mode, Level: checkpoint.Level,
			},
			isAdmissionReserved: checkpoint.ExpectedPlayerCount > 1,
			isCheckpointRestore: true,
		}
		m.games[checkpoint.GameID] = instance
		isCreated = true
		if checkpoint.GameID >= m.nextID {
			m.nextID = checkpoint.GameID + 1
		}
	} else {
		err := instance.validateRestore(checkpoint)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("gameRestoreShell: %w", err)
		}
		instance.mu.Lock()
		instance.isCheckpointRestore = true
		instance.mu.Unlock()
	}
	m.mu.Unlock()
	err := instance.restorePlayerAtSlot(user, checkpoint.Slot)
	if err != nil {
		if isCreated {
			m.mu.Lock()
			if m.games[checkpoint.GameID] == instance {
				delete(m.games, checkpoint.GameID)
			}
			m.mu.Unlock()
		}
		return nil, fmt.Errorf("gameRestoreMember: %w", err)
	}
	instance.SetExpectedPlayerCount(checkpoint.ExpectedPlayerCount)
	if checkpoint.ExpectedPlayerCount > 1 {
		instance.ReserveAdmission()
	}
	return instance, nil
}

// ReserveGameID prevents a fresh game from claiming an ID still owned by a
// durable checkpoint that has not recreated its in-memory shell yet.
func (m *Manager) ReserveGameID(gameID uint32) {
	if m == nil || gameID == 0 {
		return
	}
	m.mu.Lock()
	if gameID >= m.nextID {
		m.nextID = gameID + 1
	}
	m.mu.Unlock()
}

func (g *Instance) validateRestore(checkpoint ResumeCheckpoint) error {
	if g == nil {
		return errors.New("game restore shell unavailable")
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !strings.EqualFold(g.Info.Level, checkpoint.Level) {
		return errors.New("game restore level mismatch")
	}
	difficulty := fmt.Sprintf("%d", checkpoint.Difficulty)
	if g.Info.Attributes["SelectedDifficulty"] != difficulty {
		return errors.New("game restore difficulty mismatch")
	}
	if g.Info.ExpectedPlayerCount != 0 &&
		g.Info.ExpectedPlayerCount != checkpoint.ExpectedPlayerCount {
		return errors.New("game restore participant count mismatch")
	}
	return nil
}

// SetHostNetwork sets the gameplay endpoint advertised by dedicated games.
func (m *Manager) SetHostNetwork(hostNetwork NetworkPair) {
	m.mu.Lock()
	m.hostNetwork = hostNetwork
	m.mu.Unlock()
}

// HostNetwork returns the gameplay endpoint advertised by dedicated games.
func (m *Manager) HostNetwork() NetworkPair {
	m.mu.RLock()
	hostNetwork := m.hostNetwork
	m.mu.RUnlock()
	return hostNetwork
}

// SetChainLevelReferences installs the authored campaign order loaded from
// content.db. References retain their authored .Level suffix at the adapter
// boundary and are normalized only when bound to a live game instance.
func (m *Manager) SetChainLevelReferences(reference []string) {
	chainLevels := make([]string, 0, len(reference))
	for _, levelReference := range reference {
		levelReference = strings.TrimSpace(levelReference)
		if levelReference == "" {
			continue
		}
		chainLevels = append(chainLevels, levelReference)
	}
	m.mu.Lock()
	m.chainLevels = chainLevels
	m.mu.Unlock()
}

// ChainLevelForProgression resolves the next playable chain level after the
// account's highest completed one-based chain index.
func (m *Manager) ChainLevelForProgression(progression uint32) (string, uint32, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.chainLevels) == 0 {
		if progression == 0 {
			return InitialChainLevel, 1, true
		}
		return "", 0, false
	}
	if uint64(progression) >= uint64(len(m.chainLevels)) {
		return "", 0, false
	}
	level := strings.TrimSuffix(m.chainLevels[progression], ".Level")
	if level == "" {
		return "", 0, false
	}
	return level, progression + 1, true
}

// ChainLevelForSelection resolves one exact one-based campaign selection.
func (m *Manager) ChainLevelForSelection(selection uint32) (string, bool) {
	if selection == 0 {
		return "", false
	}
	level, resolvedSelection, isFound := m.ChainLevelForProgression(selection - 1)
	return level, isFound && resolvedSelection == selection
}

// ChainLevelIndex returns the authored one-based chain index for a level.
func (m *Manager) ChainLevelIndex(level string) (uint32, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for ordinal, levelReference := range m.chainLevels {
		candidate := strings.TrimSuffix(levelReference, ".Level")
		if strings.EqualFold(candidate, level) {
			return uint32(ordinal + 1), true
		}
	}
	if len(m.chainLevels) == 0 && strings.EqualFold(level, InitialChainLevel) {
		return 1, true
	}
	return 0, false
}

// RequestCampaignWarp replaces the one-shot developer destination for a player.
// Fang resolves partial input to one canonical packaged level before this boundary.
func (e *Manager) RequestCampaignWarp(id int64, level string) bool {
	level = strings.TrimSpace(level)
	if e == nil || id <= 0 || level == "" || len(level) > 128 {
		return false
	}
	for _, character := range level {
		isLetter := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLetter && !isDigit && character != '_' && character != '-' {
			return false
		}
	}
	e.mu.Lock()
	e.pendingCampaignWarps[id] = level
	e.mu.Unlock()
	return true
}

// CampaignWarp returns the current one-shot developer destination without
// consuming it. Game creation consumes the request only after admission succeeds.
func (e *Manager) CampaignWarp(id int64) (string, bool) {
	if e == nil || id <= 0 {
		return "", false
	}
	e.mu.RLock()
	level, isFound := e.pendingCampaignWarps[id]
	e.mu.RUnlock()
	return level, isFound
}

// ConsumeCampaignWarp clears the exact request applied to an accepted game.
// A newer request remains queued when it replaced the destination concurrently.
func (e *Manager) ConsumeCampaignWarp(id int64, level string) bool {
	if e == nil || id <= 0 || level == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	queuedLevel, isFound := e.pendingCampaignWarps[id]
	if !isFound || !strings.EqualFold(queuedLevel, level) {
		return false
	}
	delete(e.pendingCampaignWarps, id)
	return true
}

func (m *Manager) Game(id uint32) *Instance {
	m.mu.RLock()
	instance := m.games[id]
	m.mu.RUnlock()
	return instance
}

// RemoveTutorialForPlayer retires an interrupted tutorial before a new game
// request. Tutorials always restart until account completion is persisted.
func (m *Manager) RemoveTutorialForPlayer(user *sporenet.User) (uint32, bool) {
	if m == nil || user == nil {
		return 0, false
	}
	gameID := user.CurrentGameID()
	instance := m.Game(gameID)
	if instance == nil || instance.Info.Mode != ModeTutorial ||
		!IsTutorialLevel(instance.Info.Level) {
		return 0, false
	}
	m.Remove(gameID)
	return gameID, true
}

func (m *Manager) Remove(id uint32) {
	m.mu.Lock()
	instance := m.games[id]
	delete(m.games, id)
	removalObserver := m.removalObserver
	m.mu.Unlock()
	if instance != nil {
		for _, player := range instance.Players() {
			instance.RemovePlayer(player.Account.ID)
		}
		if removalObserver != nil {
			removalObserver(id)
		}
	}
}

// UseRemovalObserver connects the game shell lifecycle to the live gameplay
// owner without making the game feature depend on a transport package.
func (m *Manager) UseRemovalObserver(observer func(uint32)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.removalObserver = observer
	m.mu.Unlock()
}

func (m *Manager) Games() []*Instance {
	m.mu.RLock()
	games := make([]*Instance, 0, len(m.games))
	for _, instance := range m.games {
		games = append(games, instance)
	}
	m.mu.RUnlock()
	return games
}
