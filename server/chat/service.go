// Package chat owns player-chat validation and audience selection.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var (
	// ErrTargetNotFound means the requested player or multiplayer group is not active.
	ErrTargetNotFound = errors.New("chat target not found")
	// ErrTargetType means the client requested an unsupported chat audience.
	ErrTargetType = errors.New("chat target type invalid")
	// ErrSenderNotMember means a player attempted to address a group they have not joined.
	ErrSenderNotMember = errors.New("chat sender is not an audience member")
	// ErrBodyMissing means a player submitted no chat body.
	ErrBodyMissing = errors.New("chat body missing")
	// ErrEffectPreviewUnavailable means the actor has no active gameplay binding.
	ErrEffectPreviewUnavailable = errors.New("effect preview unavailable")
	// ErrItemSummonUnavailable means an inventory grant cannot be applied.
	ErrItemSummonUnavailable = errors.New("item summon unavailable")
	// ErrItemExists means the requested stable inventory identity is occupied.
	ErrItemExists = errors.New("item already exists")
	// ErrLevelUnavailable means the requested account-level mutation is invalid.
	ErrLevelUnavailable = errors.New("level unavailable")
	// ErrWarpUnavailable means a one-shot campaign destination cannot be queued.
	ErrWarpUnavailable = errors.New("warp unavailable")
	// ErrNPCSpawnUnavailable means an NPC cannot be queued in the active warped zone.
	ErrNPCSpawnUnavailable = errors.New("npc spawn unavailable")
	// ErrDNAUnavailable means a DNA grant cannot be applied.
	ErrDNAUnavailable = errors.New("DNA unavailable")
	// ErrDNAOverflow means a DNA grant would exceed the account currency limit.
	ErrDNAOverflow = errors.New("DNA overflow")
	// ErrResourceUnavailable means no active gameplay hero can receive the debug mutation.
	ErrResourceUnavailable = errors.New("resource unavailable")
	// ErrEventUnavailable means the requested developer event cannot run in the active game.
	ErrEventUnavailable = errors.New("event unavailable")
	// ErrFollowUnavailable means the actor has no active multiplayer ally to follow.
	ErrFollowUnavailable = errors.New("follow unavailable")
	// ErrHintUnavailable means the actor has no active gameplay state to inspect.
	ErrHintUnavailable = errors.New("hint unavailable")
	// ErrLocationUnavailable means the actor has no deployed gameplay position.
	ErrLocationUnavailable = errors.New("location unavailable")
	// ErrResourceStatusUnavailable means the actor has no deployed hero resources.
	ErrResourceStatusUnavailable = errors.New("resource status unavailable")
	// ErrBugReportUnavailable means the server cannot create a local diagnostic bundle.
	ErrBugReportUnavailable = errors.New("bug report unavailable")
	// ErrSnapshotUnavailable means the sync snapshot service is disabled or unavailable.
	ErrSnapshotUnavailable = errors.New("sync snapshot unavailable")
)

// Scope identifies a protocol-independent chat audience.
type Scope uint8

const (
	ScopeDirect Scope = iota + 1
	ScopeParty
	ScopeGame
	ScopeLobby
	// ScopeGlobal is the temporary server-wide General channel used while
	// cross-game lobby membership is not yet wired through the client flow.
	ScopeGlobal
)

// Target identifies one player or multiplayer group.
type Target struct {
	Scope Scope
	ID    uint64
}

// Participant is the chat-safe identity exposed to the feature.
type Participant struct {
	ID   int64
	Name string
}

// Directory resolves the current members of a chat target.
type Directory interface {
	Members(context.Context, Target) ([]Participant, error)
}

// Recorder persists or emits an accepted chat message. Implementations are
// adapters owned by the composition root, such as SQLite and JSONL writers.
type Recorder interface {
	Record(context.Context, Message) error
}

// SendCommand contains the trusted sender and decoded client intent.
type SendCommand struct {
	Sender Participant
	Target Target
	Body   string
}

// PingCommand requests safe live-session statistics for an authenticated actor.
type PingCommand struct {
	Sender Participant
	GameID uint32
}

// PingResult is the chat-safe subset of the actor's current game and server population.
type PingResult struct {
	GameID      uint32
	PlayerCount int
}

// EffectPreviewCommand requests a one-shot effect on the authenticated
// participant's currently deployed hero.
type EffectPreviewCommand struct {
	Sender Participant
	GameID uint32
	Asset  uint32
}

// EffectPreviewer queues presentation requests for gameplay-owned execution.
type EffectPreviewer interface {
	RequestEffectPreview(context.Context, EffectPreviewCommand) error
}

// ResourceCommand requests one developer-owned squad resource mutation.
type ResourceCommand struct {
	Sender         Participant
	GameID         uint32
	Damage         float32
	PowerReduction float32
	IsHeal         bool
	IsPowerFill    bool
}

// ResourceMutator queues developer resource mutations for gameplay-owned execution.
type ResourceMutator interface {
	RequestResourceMutation(context.Context, ResourceCommand) error
}

// EventCommand requests one named allowlisted gameplay event.
type EventCommand struct {
	Sender   Participant
	GameID   uint32
	Name     string
	Category string
	X        float32
	Y        float32
	Z        float32
}

// EventTriggerer queues developer events for gameplay-owned execution.
type EventTriggerer interface {
	RequestEvent(context.Context, EventCommand) error
}

// FollowCommand requests that the actor's deployed hero follow one active ally.
type FollowCommand struct {
	Sender     Participant
	GameID     uint32
	TargetName string
}

// FollowResult describes either the selected ally or the names available for selection.
type FollowResult struct {
	Target     Participant
	Candidates []Participant
	IsQueued   bool
}

// FollowRequester queues an authenticated multiplayer follow request for gameplay.
type FollowRequester interface {
	RequestFollow(context.Context, FollowCommand, Participant) error
}

// HintRequest identifies the authenticated player whose live encounter state is inspected.
type HintRequest struct {
	Sender Participant
	GameID uint32
}

// HintResult describes the nearest living hostile relative to the requesting hero.
type HintResult struct {
	Name      string
	Direction string
}

// HintProvider reads authoritative gameplay state without exposing it to the chat transport.
type HintProvider interface {
	Hint(context.Context, HintRequest) (HintResult, error)
}

// LocationRequest identifies the authenticated player whose live position is
// inspected.
type LocationRequest struct {
	Sender Participant
	GameID uint32
}

// LocationResult is the authoritative world position of one deployed hero.
type LocationResult struct {
	X float32
	Y float32
	Z float32
}

// LocationProvider reads authoritative gameplay position for chat diagnostics.
type LocationProvider interface {
	Location(context.Context, LocationRequest) (LocationResult, error)
}

// ResourceStatusRequest identifies the authoritative hero and optional Fang
// sample captured from the client immediately before sending the command.
type ResourceStatusRequest struct {
	Sender                Participant
	GameID                uint32
	ClientObjectID        uint32
	ClientHitPoint        float32
	ClientPowerPoint      float32
	IsClientHitPointSet   bool
	IsClientPowerPointSet bool
}

type ResourceStatusResult struct {
	ObjectID          uint32
	HitPoint          float32
	MaximumHitPoint   float32
	PowerPoint        float32
	MaximumPowerPoint float32
}

type ResourceStatusProvider interface {
	ResourceStatus(context.Context, ResourceStatusRequest) (ResourceStatusResult, error)
}

// ItemSummonCommand describes one explicit persistent inventory item.
type ItemSummonCommand struct {
	Sender          Participant
	GameID          uint32
	RigblockID      uint16
	PrimaryPrefix   uint16
	SecondaryPrefix uint16
	Suffix          uint16
}

// ItemSummoner owns persistent developer inventory grants.
type ItemSummoner interface {
	SummonItem(context.Context, ItemSummonCommand) error
}

// LevelCommand requests an exact developer account-level assignment.
type LevelCommand struct {
	Sender Participant
	GameID uint32
	Level  uint32
}

// LevelSetter owns persistent developer account-level changes.
type LevelSetter interface {
	SetLevel(context.Context, LevelCommand) error
}

// WarpCommand requests one exact Fang-resolved destination for the next
// campaign game accepted for the authenticated player.
type WarpCommand struct {
	Sender Participant
	Level  string
}

// WarpRequester owns pending one-shot campaign destinations.
type WarpRequester interface {
	RequestWarp(context.Context, WarpCommand) error
}

// NPCSpawnCommand requests one exact Fang-resolved combat noun beside the
// authenticated player's deployed hero.
type NPCSpawnCommand struct {
	Sender   Participant
	GameID   uint32
	NounName string
}

// NPCSpawner queues developer NPCs for gameplay-owned execution.
type NPCSpawner interface {
	RequestNPCSpawn(context.Context, NPCSpawnCommand) error
}

// DNACommand requests an additive developer account-currency grant.
type DNACommand struct {
	Sender Participant
	GameID uint32
	Amount uint32
}

// DNAGranter owns persistent developer account-currency grants.
type DNAGranter interface {
	GrantDNA(context.Context, DNACommand) (uint32, error)
}

// BugCommand requests a local diagnostic bundle for an authenticated actor.
type BugCommand struct {
	Sender       Participant
	GameID       uint32
	Description  string
	Context      BugContext
	ContextError string
}

// BugReporter creates a server-owned diagnostic bundle without uploading it.
type BugReporter interface {
	Report(context.Context, BugCommand) (string, error)
}

// SnapshotCommand contains one authenticated in-game sync snapshot operation.
type SnapshotCommand struct {
	Sender   Participant
	GameID   uint32
	Action   string
	Argument string
}

// SnapshotResult is safe to echo to the requesting player's game chat.
type SnapshotResult struct {
	Message string
}

// SnapshotManager owns rolling diagnostics and local snapshot artifacts.
type SnapshotManager interface {
	Execute(context.Context, SnapshotCommand) (SnapshotResult, error)
}

// Message is an accepted transient chat message and its delivery audience.
type Message struct {
	ID           uint64
	Sender       Participant
	Target       Target
	Body         string
	RecipientIDs []int64
	SentAt       time.Time
}

// Service validates chat intent before transport notifications are emitted.
type Service struct {
	directory              Directory
	recorders              []Recorder
	nextID                 atomic.Uint64
	now                    func() time.Time
	previewer              EffectPreviewer
	resourceMutator        ResourceMutator
	eventTriggerer         EventTriggerer
	followRequester        FollowRequester
	hintProvider           HintProvider
	locationProvider       LocationProvider
	resourceStatusProvider ResourceStatusProvider
	summoner               ItemSummoner
	leveler                LevelSetter
	warpRequester          WarpRequester
	npcSpawner             NPCSpawner
	dnaGranter             DNAGranter
	bugReporter            BugReporter
	bugContext             BugContextProvider
	snapshotManager        SnapshotManager
}

// UseEffectPreviewer installs the gameplay-owned effect preview adapter.
func (s *Service) UseEffectPreviewer(previewer EffectPreviewer) {
	s.previewer = previewer
}

// UseResourceMutator installs the gameplay-owned developer resource adapter.
func (s *Service) UseResourceMutator(resourceMutator ResourceMutator) {
	s.resourceMutator = resourceMutator
}

// UseEventTriggerer installs the gameplay-owned developer event adapter.
func (s *Service) UseEventTriggerer(eventTriggerer EventTriggerer) {
	s.eventTriggerer = eventTriggerer
}

// UseFollowRequester installs the gameplay-owned multiplayer follow adapter.
func (s *Service) UseFollowRequester(followRequester FollowRequester) {
	s.followRequester = followRequester
}

// UseHintProvider installs the gameplay-owned live encounter hint source.
func (s *Service) UseHintProvider(hintProvider HintProvider) {
	s.hintProvider = hintProvider
}

// UseLocationProvider installs the gameplay-owned live position source.
func (s *Service) UseLocationProvider(locationProvider LocationProvider) {
	s.locationProvider = locationProvider
}

func (s *Service) UseResourceStatusProvider(provider ResourceStatusProvider) {
	s.resourceStatusProvider = provider
}

// UseItemSummoner installs the persistent inventory command adapter.
func (s *Service) UseItemSummoner(summoner ItemSummoner) {
	s.summoner = summoner
}

// UseLevelSetter installs the persistent account-level command adapter.
func (s *Service) UseLevelSetter(leveler LevelSetter) {
	s.leveler = leveler
}

// UseWarpRequester installs the game-owned one-shot campaign warp adapter.
func (s *Service) UseWarpRequester(warpRequester WarpRequester) {
	s.warpRequester = warpRequester
}

// UseNPCSpawner installs the gameplay-owned warped-zone NPC adapter.
func (s *Service) UseNPCSpawner(npcSpawner NPCSpawner) {
	s.npcSpawner = npcSpawner
}

// UseDNAGranter installs the persistent account-currency command adapter.
func (s *Service) UseDNAGranter(dnaGranter DNAGranter) {
	s.dnaGranter = dnaGranter
}

// UseBugReporter installs the local diagnostic bundle adapter.
func (s *Service) UseBugReporter(bugReporter BugReporter) {
	s.bugReporter = bugReporter
}

// UseBugContextProvider installs the gameplay-owned live diagnostic source.
func (s *Service) UseBugContextProvider(provider BugContextProvider) {
	s.bugContext = provider
}

// UseSnapshotManager installs the server-owned sync snapshot feature.
func (s *Service) UseSnapshotManager(snapshotManager SnapshotManager) {
	s.snapshotManager = snapshotManager
}

// NewService creates a chat service backed by the active multiplayer directory.
func NewService(directory Directory, recorders ...Recorder) (*Service, error) {
	if directory == nil {
		return nil, errors.New("create chat service: nil directory")
	}
	return &Service{directory: directory, recorders: recorders, now: time.Now}, nil
}

// Send validates and resolves a player-authored chat message.
func (s *Service) Send(ctx context.Context, command SendCommand) (Message, error) {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return Message{}, ErrSenderNotMember
	}
	if command.Body == "" {
		return Message{}, ErrBodyMissing
	}
	if !validScope(command.Target.Scope) {
		return Message{}, ErrTargetType
	}
	members, err := s.directory.Members(ctx, command.Target)
	if err != nil {
		return Message{}, fmt.Errorf("audienceResolve: %w", err)
	}
	if len(members) == 0 {
		return Message{}, ErrTargetNotFound
	}

	recipients := make([]int64, 0, len(members)+1)
	isSenderFound := command.Target.Scope == ScopeDirect
	for _, member := range members {
		if member.ID == 0 {
			continue
		}
		if member.ID == command.Sender.ID {
			isSenderFound = true
		}
		recipients = appendUnique(recipients, member.ID)
	}
	if !isSenderFound {
		return Message{}, ErrSenderNotMember
	}
	if command.Target.Scope == ScopeDirect {
		recipients = appendUnique(recipients, command.Sender.ID)
	}
	if len(recipients) == 0 {
		return Message{}, ErrTargetNotFound
	}

	message := Message{
		ID: s.nextID.Add(1), Sender: command.Sender, Target: command.Target,
		Body: command.Body, RecipientIDs: recipients, SentAt: s.now().UTC(),
	}
	for index, recorder := range s.recorders {
		if recorder == nil {
			continue
		}
		err = recorder.Record(ctx, message)
		if err != nil {
			return Message{}, fmt.Errorf("chatRecord[%d]: %w", index, err)
		}
	}
	return message, nil
}

// Ping validates that the actor is online and returns safe server-wide statistics.
func (s *Service) Ping(ctx context.Context, command PingCommand) (PingResult, error) {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return PingResult{}, ErrSenderNotMember
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGlobal})
	if err != nil {
		return PingResult{}, fmt.Errorf("pingAudience: %w", err)
	}
	for _, member := range members {
		if member.ID == command.Sender.ID {
			return PingResult{GameID: command.GameID, PlayerCount: len(members)}, nil
		}
	}
	return PingResult{}, ErrSenderNotMember
}

// PreviewEffect validates active game membership before queueing presentation.
func (s *Service) PreviewEffect(ctx context.Context, command EffectPreviewCommand) error {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return ErrSenderNotMember
	}
	if command.GameID == 0 || command.Asset == 0 || s.previewer == nil {
		return ErrEffectPreviewUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(command.GameID)})
	if err != nil {
		return fmt.Errorf("effectAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != command.Sender.ID {
			continue
		}
		err = s.previewer.RequestEffectPreview(ctx, command)
		if err != nil {
			return fmt.Errorf("effectRequest: %w", err)
		}
		return nil
	}
	return ErrSenderNotMember
}

// MutateResource validates membership before queueing a gameplay resource command.
func (s *Service) MutateResource(ctx context.Context, command ResourceCommand) error {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return ErrSenderNotMember
	}
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
	if command.GameID == 0 || operationCount != 1 || s.resourceMutator == nil {
		return ErrResourceUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(command.GameID)})
	if err != nil {
		return fmt.Errorf("resourceAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != command.Sender.ID {
			continue
		}
		err = s.resourceMutator.RequestResourceMutation(ctx, command)
		if err != nil {
			return fmt.Errorf("resourceRequest: %w", err)
		}
		return nil
	}
	return ErrSenderNotMember
}

// TriggerEvent validates membership before queueing an allowlisted gameplay event.
func (s *Service) TriggerEvent(ctx context.Context, command EventCommand) error {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return ErrSenderNotMember
	}
	if command.GameID == 0 || command.Name == "" || s.eventTriggerer == nil {
		return ErrEventUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(command.GameID)})
	if err != nil {
		return fmt.Errorf("eventAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != command.Sender.ID {
			continue
		}
		err = s.eventTriggerer.RequestEvent(ctx, command)
		if err != nil {
			return fmt.Errorf("eventRequest: %w", err)
		}
		return nil
	}
	return ErrSenderNotMember
}

// SpawnNPC validates game membership before queueing one Fang-approved combat
// noun.
func (s *Service) SpawnNPC(ctx context.Context, command NPCSpawnCommand) error {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return ErrSenderNotMember
	}
	if command.GameID == 0 || command.NounName == "" || s.npcSpawner == nil {
		return ErrNPCSpawnUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(command.GameID)})
	if err != nil {
		return fmt.Errorf("npcSpawnAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != command.Sender.ID {
			continue
		}
		err = s.npcSpawner.RequestNPCSpawn(ctx, command)
		if err != nil {
			return fmt.Errorf("npcSpawnRequest: %w", err)
		}
		return nil
	}
	return ErrSenderNotMember
}

// Follow resolves one ally from the active game before queueing gameplay movement.
func (s *Service) Follow(ctx context.Context, command FollowCommand) (FollowResult, error) {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return FollowResult{}, ErrSenderNotMember
	}
	if command.GameID == 0 || s.followRequester == nil {
		return FollowResult{}, ErrFollowUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(command.GameID)})
	if err != nil {
		return FollowResult{}, fmt.Errorf("followAudience: %w", err)
	}
	isSenderFound := false
	candidates := make([]Participant, 0, len(members))
	for _, member := range members {
		if member.ID == command.Sender.ID {
			isSenderFound = true
			continue
		}
		candidates = append(candidates, member)
	}
	if !isSenderFound {
		return FollowResult{}, ErrSenderNotMember
	}
	if len(candidates) == 0 {
		return FollowResult{}, ErrFollowUnavailable
	}
	targetName := strings.TrimSpace(command.TargetName)
	if targetName == "" && len(candidates) != 1 {
		return FollowResult{Candidates: candidates}, nil
	}
	target := Participant{}
	if targetName == "" {
		target = candidates[0]
	} else {
		for _, candidate := range candidates {
			if strings.EqualFold(candidate.Name, targetName) {
				target = candidate
				break
			}
		}
	}
	if target.ID == 0 {
		return FollowResult{Candidates: candidates}, nil
	}
	err = s.followRequester.RequestFollow(ctx, command, target)
	if err != nil {
		return FollowResult{}, fmt.Errorf("followRequest: %w", err)
	}
	return FollowResult{Target: target, Candidates: candidates, IsQueued: true}, nil
}

// Hint validates active game membership before inspecting live encounter state.
func (s *Service) Hint(ctx context.Context, req HintRequest) (HintResult, error) {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return HintResult{}, ErrSenderNotMember
	}
	if req.GameID == 0 || s.hintProvider == nil {
		return HintResult{}, ErrHintUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(req.GameID)})
	if err != nil {
		return HintResult{}, fmt.Errorf("hintAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != req.Sender.ID {
			continue
		}
		result, hintErr := s.hintProvider.Hint(ctx, req)
		if hintErr != nil {
			return HintResult{}, fmt.Errorf("hintInspect: %w", hintErr)
		}
		return result, nil
	}
	return HintResult{}, ErrSenderNotMember
}

// Location validates active game membership before reading live player position.
func (s *Service) Location(
	ctx context.Context, req LocationRequest,
) (LocationResult, error) {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return LocationResult{}, ErrSenderNotMember
	}
	if req.GameID == 0 || s.locationProvider == nil {
		return LocationResult{}, ErrLocationUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(req.GameID)})
	if err != nil {
		return LocationResult{}, fmt.Errorf("locationAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != req.Sender.ID {
			continue
		}
		result, locationErr := s.locationProvider.Location(ctx, req)
		if locationErr != nil {
			return LocationResult{}, fmt.Errorf("locationInspect: %w", locationErr)
		}
		return result, nil
	}
	return LocationResult{}, ErrSenderNotMember
}

func (s *Service) ResourceStatus(
	ctx context.Context, req ResourceStatusRequest,
) (ResourceStatusResult, error) {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return ResourceStatusResult{}, ErrSenderNotMember
	}
	if req.GameID == 0 || s.resourceStatusProvider == nil {
		return ResourceStatusResult{}, ErrResourceStatusUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(req.GameID)})
	if err != nil {
		return ResourceStatusResult{}, fmt.Errorf("resourceStatusAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != req.Sender.ID {
			continue
		}
		result, statusErr := s.resourceStatusProvider.ResourceStatus(ctx, req)
		if statusErr != nil {
			return ResourceStatusResult{}, fmt.Errorf("resourceStatusInspect: %w", statusErr)
		}
		return result, nil
	}
	return ResourceStatusResult{}, ErrSenderNotMember
}

// SummonItem validates the authenticated actor before granting inventory.
func (s *Service) SummonItem(ctx context.Context, command ItemSummonCommand) error {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return ErrSenderNotMember
	}
	if command.RigblockID == 0 || s.summoner == nil {
		return ErrItemSummonUnavailable
	}
	err := s.summoner.SummonItem(ctx, command)
	if err != nil {
		return fmt.Errorf("summonItem: %w", err)
	}
	return nil
}

// SetLevel validates the authenticated actor before assigning account level.
func (s *Service) SetLevel(ctx context.Context, command LevelCommand) error {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return ErrSenderNotMember
	}
	if command.Level == 0 || s.leveler == nil {
		return ErrLevelUnavailable
	}
	err := s.leveler.SetLevel(ctx, command)
	if err != nil {
		return fmt.Errorf("setLevel: %w", err)
	}
	return nil
}

// RequestWarp validates the authenticated actor before queueing one exact
// destination resolved from Fang's packaged-level allowlist.
func (s *Service) RequestWarp(ctx context.Context, req WarpCommand) error {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return ErrSenderNotMember
	}
	req.Level = strings.TrimSpace(req.Level)
	if req.Level == "" || s.warpRequester == nil {
		return ErrWarpUnavailable
	}
	err := s.warpRequester.RequestWarp(ctx, req)
	if err != nil {
		return fmt.Errorf("warpRequest: %w", err)
	}
	return nil
}

// GrantDNA validates the authenticated actor before adding account currency.
func (s *Service) GrantDNA(ctx context.Context, command DNACommand) (uint32, error) {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return 0, ErrSenderNotMember
	}
	if command.Amount == 0 || s.dnaGranter == nil {
		return 0, ErrDNAUnavailable
	}
	dna, err := s.dnaGranter.GrantDNA(ctx, command)
	if err != nil {
		return 0, fmt.Errorf("grantDNA: %w", err)
	}
	return dna, nil
}

// ReportBug validates the actor and creates a local diagnostic bundle.
func (s *Service) ReportBug(ctx context.Context, command BugCommand) (string, error) {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return "", ErrSenderNotMember
	}
	command.Description = strings.TrimSpace(command.Description)
	if command.Description == "" || s.bugReporter == nil {
		return "", ErrBugReportUnavailable
	}
	if s.bugContext != nil {
		bugContext, contextErr := s.bugContext.BugContext(ctx, BugContextRequest{
			Sender: command.Sender, GameID: command.GameID,
		})
		command.Context = bugContext
		if contextErr != nil {
			command.ContextError = contextErr.Error()
		}
	}
	reportName, err := s.bugReporter.Report(ctx, command)
	if err != nil {
		return "", fmt.Errorf("bugReport: %w", err)
	}
	return reportName, nil
}

// Snapshot validates the actor before executing one diagnostic operation.
func (s *Service) Snapshot(
	ctx context.Context, command SnapshotCommand,
) (SnapshotResult, error) {
	if command.Sender.ID == 0 || command.Sender.Name == "" {
		return SnapshotResult{}, ErrSenderNotMember
	}
	if s.snapshotManager == nil {
		return SnapshotResult{}, ErrSnapshotUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGlobal})
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshotAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != command.Sender.ID {
			continue
		}
		result, executeErr := s.snapshotManager.Execute(ctx, command)
		if executeErr != nil {
			return SnapshotResult{}, fmt.Errorf("snapshotExecute: %w", executeErr)
		}
		return result, nil
	}
	return SnapshotResult{}, ErrSenderNotMember
}

func validScope(scope Scope) bool {
	return scope >= ScopeDirect && scope <= ScopeGlobal
}

func appendUnique(values []int64, value int64) []int64 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
