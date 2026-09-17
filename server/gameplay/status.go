package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	developerfeature "github.com/darkspinnet/darkspin/server/developer"
	developerraknet "github.com/darkspinnet/darkspin/server/developer/raknet103"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	memberraknet "github.com/darkspinnet/darkspin/server/zone/member/raknet103"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
	outcomeraknet "github.com/darkspinnet/darkspin/server/zone/outcome/raknet103"
	zonepreview "github.com/darkspinnet/darkspin/server/zone/preview"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
	resultraknet "github.com/darkspinnet/darkspin/server/zone/result/raknet103"
	resultsporenet "github.com/darkspinnet/darkspin/server/zone/result/sporenet"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
	tutorialprogress "github.com/darkspinnet/darkspin/server/zone/tutorial/progression"
)

const tutorialCompletionBeamDuration = 650 * time.Millisecond

type gameplayStatusRuntime struct {
	registry     *gameplaySessionRegistry
	gameplayJoin *game.GameplayJoin
	progression  tutorialStatusProgression
	modifierPool *modifierPool
	effectPool   *attachedEffectPool
	preparation  campaignPreparation
	setup        gameplaySetupRuntime
	chainLevels  []string
	logger       *log.Logger
}

type tutorialStatusProgression interface {
	tutorialprogress.CompletionStore
	tutorialprogress.RestartStore
	GrantAccountExperience(context.Context, int64, uint32) (sporenet.TutorialExperience, error)
	GrantCampaignExperience(context.Context, int64, uint64, uint32) (sporenet.TutorialExperience, error)
}

type gameplayStatusContext struct {
	status      raknet.PlayerStatus
	peerSession gameplayPeerSession
	isFound     bool
}

type tutorialCompletionStep struct {
	runtime     gameplayStatusRuntime
	sessionKey  string
	generation  uint64
	packets     [][]byte
	isPublished bool
}

func (e *tutorialCompletionStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.Boss() != nil &&
		peerSession.zone.Boss().IsBeamOutCommitted()
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	e.isPublished = true
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"RakNet tutorial completion snapshot queued for %s generation=%d game=%d delay_ms=%d snapshot=TutorialGame/0",
			e.sessionKey, e.generation, peerSession.binding.GameID,
			tutorialCompletionBeamDuration.Milliseconds(),
		)
	}
	return e.packets, nil
}

func (e *tutorialCompletionStep) finish() {
	if !e.isPublished {
		return
	}
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return
	}
	// This autonomous scheduled continuation outlives the initiating request.
	// Publish POST_GAME only after transport writes the completion snapshot.
	// Its native callback sends RemovePlayer; that handshake owns retirement.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := e.runtime.gameplayJoin.EndTutorial(ctx,
		int64(peerSession.binding.UserID), peerSession.binding.GameID)
	if err != nil {
		e.runtime.logger.Printf("RakNet tutorial post-game publication failed for %s: %v", e.sessionKey, err)
		return
	}
	e.runtime.logger.Printf("RakNet tutorial post-game published for %s game=%d; awaiting Blaze RemovePlayer", e.sessionKey, peerSession.binding.GameID)
}

func (r gameplayStatusRuntime) handle(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	statusContext, err := r.begin(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("statusBegin: %w", err)
	}
	if !statusContext.isFound {
		return nil, nil
	}
	peerSession := statusContext.peerSession
	switch peerSession.binding.Mode {
	case game.ModeTutorial:
		if statusContext.status.Status == 0x20 {
			return r.completeTutorial(ctx, packet, peerSession)
		}
		return r.handleChain(ctx, packet, peerSession, statusContext.status)
	case game.ModeChain:
		return r.handleChain(ctx, packet, peerSession, statusContext.status)
	case game.ModeArena:
		return r.handleArena(ctx, packet, peerSession, statusContext.status)
	default:
		return nil, nil
	}
}

func (r gameplayStatusRuntime) handleArena(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
	status raknet.PlayerStatus,
) ([][]byte, error) {
	if !peerSession.isArenaPreparationSent {
		return nil, nil
	}
	switch status.Status {
	case 2, 4:
		peerSession = r.acknowledgeArenaPreparation(packet.Address.String(), peerSession)
		return r.handleChain(ctx, packet, peerSession, status)
	case 8:
		peerSession = r.acknowledgeArenaPreparation(packet.Address.String(), peerSession)
		startPackets, err := r.startArena(packet, peerSession, status)
		if err != nil {
			return nil, fmt.Errorf("statusArenaStart: %w", err)
		}
		setupPackets, isPublished, err := r.setup.publishDungeon(ctx, packet)
		if err != nil {
			return nil, fmt.Errorf("statusArenaSetup: %w", err)
		}
		if !isPublished {
			r.logger.Printf(
				"RakNet Arena hero setup deferred for %s after status 8",
				packet.Address,
			)
		}
		return append(startPackets, setupPackets...), nil
	default:
		return nil, nil
	}
}

func (r gameplayStatusRuntime) acknowledgeArenaPreparation(
	sessionKey string, peerSession gameplayPeerSession,
) gameplayPeerSession {
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == peerSession.generation
	if isCurrent {
		currentSession.isArenaPreparationAcknowledged = true
		r.registry.sessions[sessionKey] = currentSession
		peerSession = currentSession
	}
	r.registry.mutex.Unlock()
	return peerSession
}

func (r gameplayStatusRuntime) handleChain(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
	status raknet.PlayerStatus,
) ([][]byte, error) {
	if status.Status == 0x20 {
		if status.Progress != 1 {
			return nil, nil
		}
		return r.campaignBeamOut(ctx, packet, peerSession)
	}
	if status.Status == 2 {
		playerPacket, err := marshalCampaignInitialPlayer(peerSession.binding, status)
		if err != nil {
			return nil, fmt.Errorf("statusChainInitial: %w", err)
		}
		crystalPackets, err := marshalGameplayCrystalState(peerSession)
		if err != nil {
			return nil, fmt.Errorf("statusChainCrystals: %w", err)
		}
		statusRecipients := r.queueMemberStatus(
			packet.Address.String(), peerSession, playerPacket,
		)
		r.logger.Printf(
			"RakNet campaign initial player sent to %s creatures=%d difficulty=%d peers=%d",
			packet.Address, zonehero.CreatureCount(peerSession.binding),
			peerSession.binding.Difficulty, statusRecipients,
		)
		return append([][]byte{playerPacket}, crystalPackets...), nil
	}
	if status.Status == 8 {
		return r.startCampaign(ctx, packet, peerSession, status)
	}
	if status.Status != 4 {
		return nil, nil
	}
	playerPacket, err := memberraknet.Status(memberraknet.StatusRequest{
		PlayerIndex: uint8(peerSession.binding.Slot),
		Status:      status.Status, Progress: status.Progress,
	})
	if err != nil {
		return nil, fmt.Errorf("statusChainPlayer: %w", err)
	}
	setupPacket, err := marshalZoneSetup(peerSession.binding)
	if err != nil {
		return nil, fmt.Errorf("statusChainSetup: %w", err)
	}
	statusRecipients := r.queueMemberStatus(
		packet.Address.String(), peerSession, playerPacket,
	)
	r.logger.Printf(
		"RakNet campaign player prepare sent to %s level=%q peers=%d",
		packet.Address, peerSession.binding.Level, statusRecipients,
	)
	return [][]byte{playerPacket, setupPacket}, nil
}

func (r gameplayStatusRuntime) queueMemberStatus(
	sessionKey string, peerSession gameplayPeerSession, statusPacket []byte,
) int {
	if r.registry == nil || sessionKey == "" || len(statusPacket) == 0 {
		return 0
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == peerSession.generation &&
		currentSession.binding.GameID == peerSession.binding.GameID
	if !isCurrent {
		return 0
	}
	recipients := 0
	for candidateKey, candidate := range r.registry.sessions {
		if candidateKey == sessionKey ||
			candidate.binding.GameID != peerSession.binding.GameID {
			continue
		}
		candidate.queuePackets([][]byte{statusPacket})
		r.registry.sessions[candidateKey] = candidate
		recipients++
	}
	return recipients
}

func (r gameplayStatusRuntime) startCampaign(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
	status raknet.PlayerStatus,
) ([][]byte, error) {
	if r.preparation.setup != nil &&
		(peerSession.zone == nil ||
			len(peerSession.zone.DirectorDefinition().Pools) == 0) {
		err := r.preparation.initialize(ctx, peerSession.binding, &peerSession)
		if err != nil {
			return nil, fmt.Errorf("statusChainInitialize: %w", err)
		}
	}
	playerPacket, err := memberraknet.Status(memberraknet.StatusRequest{
		PlayerIndex: uint8(peerSession.binding.Slot),
		Status:      status.Status, Progress: status.Progress,
	})
	if err != nil {
		return nil, fmt.Errorf("statusChainPlayer: %w", err)
	}
	if !peerSession.arePassiveModifiersAllocated {
		peerSession.passiveModifierInstance, err = zoneability.AllocatePassiveModifiers(
			r.modifierPool, peerSession.binding.Creatures,
		)
		if err != nil {
			return nil, fmt.Errorf("statusChainPassive: %w", err)
		}
		peerSession.arePassiveModifiersAllocated = true
	}
	r.registry.mutex.Lock()
	peerSession.stage.EnterDungeon()
	r.registry.sessions[packet.Address.String()] = peerSession
	r.registry.mutex.Unlock()
	statusRecipients := r.queueMemberStatus(
		packet.Address.String(), peerSession, playerPacket,
	)
	startPacket, err := memberraknet.GameStart(0)
	if err != nil {
		return nil, fmt.Errorf("statusChainStart: %w", err)
	}
	pingPacket, err := developerraknet.Ping(uint64(time.Now().Unix()))
	if err != nil {
		return nil, fmt.Errorf("statusChainPing: %w", err)
	}
	if peerSession.binding.Mode == game.ModeTutorial {
		r.logger.Printf(
			"RakNet tutorial game start handshake sent to %s level=%q",
			packet.Address, peerSession.binding.Level,
		)
		return [][]byte{playerPacket, startPacket, pingPacket}, nil
	}
	r.logger.Printf(
		"RakNet campaign game start handshake sent to %s level=%q difficulty=%d director_pools=%d director_marker_sets=%d director_markers=%d director_triggers=%d script_objects=%d peers=%d",
		packet.Address, peerSession.binding.Level, peerSession.binding.Difficulty,
		len(peerSession.zone.DirectorDefinition().Pools),
		len(peerSession.zone.DirectorDefinition().MarkerSets),
		peerSession.zone.DirectorDefinition().MarkerCount(),
		peerSession.zone.DirectorDefinition().TriggerCount(),
		len(peerSession.zone.ScriptObjects()), statusRecipients,
	)
	return [][]byte{playerPacket, startPacket, pingPacket}, nil
}

// startArena completes the shared loading handshake without entering the
// campaign director path. Arena maps own their match population and round
// state; treating status 8 as campaign initialization rejects the mode and
// causes the RakNet transport to discard an otherwise healthy peer.
func (r gameplayStatusRuntime) startArena(
	packet raknet.Packet, peerSession gameplayPeerSession,
	status raknet.PlayerStatus,
) ([][]byte, error) {
	playerPacket, err := memberraknet.Status(memberraknet.StatusRequest{
		PlayerIndex: uint8(peerSession.binding.Slot),
		Status:      status.Status, Progress: status.Progress,
	})
	if err != nil {
		return nil, fmt.Errorf("statusArenaPlayer: %w", err)
	}
	if !peerSession.arePassiveModifiersAllocated {
		peerSession.passiveModifierInstance, err = zoneability.AllocatePassiveModifiers(
			r.modifierPool, peerSession.binding.Creatures,
		)
		if err != nil {
			return nil, fmt.Errorf("statusArenaPassive: %w", err)
		}
		peerSession.arePassiveModifiersAllocated = true
	}
	r.registry.mutex.Lock()
	peerSession.stage.EnterDungeon()
	r.registry.sessions[packet.Address.String()] = peerSession
	r.registry.mutex.Unlock()
	statusRecipients := r.queueMemberStatus(
		packet.Address.String(), peerSession, playerPacket,
	)
	startPacket, err := memberraknet.GameStart(0)
	if err != nil {
		return nil, fmt.Errorf("statusArenaStart: %w", err)
	}
	r.logger.Printf(
		"RakNet Arena game start handshake sent to %s level=%q team=%d peers=%d",
		packet.Address, peerSession.binding.Level, peerSession.binding.Team,
		statusRecipients,
	)
	return [][]byte{playerPacket, startPacket}, nil
}

func (r gameplayStatusRuntime) campaignBeamOut(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
) ([][]byte, error) {
	sessionKey := packet.Address.String()
	generation := peerSession.generation
	currentSession, isReserved := r.reserveCampaignBeamOut(sessionKey, generation)
	if !isReserved {
		r.logger.Printf(
			"RakNet campaign Beam Out rejected for %s: boss incomplete or already accepted",
			packet.Address,
		)
		return nil, nil
	}
	if currentSession.deployedCreatureIndex >= uint32(len(currentSession.binding.Creatures)) {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignBeamCreature: %w",
			errors.Join(errors.New("deployed creature missing"), rollbackErr),
		)
	}
	creature := currentSession.binding.Creatures[currentSession.deployedCreatureIndex]
	beamPackets, err := heroraknet.BeamOut(
		currentSession.deployedObjectID, campaignCharacterBeam(creature, false),
		zonePosition(currentSession.playerPosition), packet.SourceTime,
	)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignBeamPresentation: %w", errors.Join(err, rollbackErr),
		)
	}
	hiddenPacket, err := heroraknet.Visibility(
		currentSession.deployedObjectID,
		zonePosition(currentSession.playerPosition), false,
	)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignBeamHidden: %w", errors.Join(err, rollbackErr),
		)
	}
	beamPackets = append(beamPackets, hiddenPacket)
	bossObjectID, isBossFound := currentSession.zone.
		ReservedResultBossObjectID(zoneResultMember(currentSession))
	if !isBossFound {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignResultBoss: %w",
			errors.Join(errors.New("unavailable"), rollbackErr),
		)
	}
	planetsCompleted := currentSession.chainPlanetsCompleted + 1
	startingExperience := uint32(max(float32(0), currentSession.binding.StartingAvatarXP))
	if currentSession.chainResult != nil {
		previousResult := currentSession.chainResult.Snapshot()
		planetsCompleted = max(planetsCompleted, previousResult.PlanetsCompleted+1)
		if previousResult.StartingExperience != 0 {
			startingExperience = previousResult.StartingExperience
		}
	}
	snapshot, err := zoneresult.NewSnapshotWithResultID(
		currentSession.binding, generation, currentSession.zone.CompletionID(),
		bossObjectID, packet.SourceTime,
		planetsCompleted, r.chainLevels,
	)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignResultSnapshot: %w", errors.Join(err, rollbackErr),
		)
	}
	experienceCommit, err := r.commitCampaignExperience(ctx, currentSession)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignExperience: %w", errors.Join(err, rollbackErr),
		)
	}
	snapshot.StartingExperience = startingExperience
	snapshot.FinalExperience = experienceCommit.CumulativeXP
	snapshot.FinalLevel = experienceCommit.Level
	snapshot.RewardClassType = creature.ClassType
	snapshot.RewardElementType = creature.ElementType
	nextBinding := currentSession.binding
	nextBinding.Level = snapshot.NextLevel
	nextBinding.ChainLevelIndex = snapshot.CompletedIndex
	if !snapshot.IsTerminal {
		nextBinding.ChainLevelIndex++
	}
	nextBinding.Difficulty = nextBinding.ChainLevelIndex
	nextBinding.RefreshReplay()
	nextDirector, err := r.preparation.loadPreview(ctx, nextBinding)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignResultPreview: %w", errors.Join(err, rollbackErr),
		)
	}
	snapshot.EnemyNouns, err = zonepreview.Nouns(
		nextDirector, nextBinding.Level, nextBinding.Difficulty,
	)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignResultRoster: %w", errors.Join(err, rollbackErr),
		)
	}
	err = currentSession.zone.CompleteObjectives(ctx, time.Now())
	if err != nil {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet campaign objective completion omitted for %s: %v",
				sessionKey, err,
			)
		}
	}
	snapshot.MedalCounts, err = campaignMedalCounts(
		currentSession.zone.Objective().State(),
	)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignObjectiveMedals: %w", errors.Join(err, rollbackErr),
		)
	}
	for playerIndex := range snapshot.MedalCounts {
		snapshot.MedalCounts[playerIndex].Gold +=
			currentSession.chainMedalCounts[playerIndex].Gold
		snapshot.MedalCounts[playerIndex].Silver +=
			currentSession.chainMedalCounts[playerIndex].Silver
		snapshot.MedalCounts[playerIndex].Bronze +=
			currentSession.chainMedalCounts[playerIndex].Bronze
	}
	objectivePacket, err := marshalCampaignObjectivesComplete(
		currentSession.zone.Objective().State(),
	)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignObjectiveComplete: %w", errors.Join(err, rollbackErr),
		)
	}
	votingPacket, err := resultraknet.VotingEntry(packet.SourceTime)
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignVotingEntry: %w", errors.Join(err, rollbackErr),
		)
	}
	transitionPacket, err := resultraknet.VotingTransition()
	if err != nil {
		rollbackErr := r.rollbackCampaignBeamOut(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusCampaignVotingTransition: %w", errors.Join(err, rollbackErr),
		)
	}
	committedSession, isCommitted := r.commitCampaignBeamOut(
		sessionKey, generation, snapshot,
	)
	if !isCommitted {
		return nil, nil
	}
	stopGameplayPeerRuntime(committedSession, r.modifierPool, r.effectPool)
	r.logger.Printf(
		"RakNet campaign Beam Out accepted for %s result=%d; chain voting entered with A9 body pending",
		packet.Address, snapshot.ResultID,
	)
	packets := append(beamPackets, objectivePacket, votingPacket)
	return append(packets, transitionPacket), nil
}

func campaignMedalCounts(state *sim.ObjectiveState) ([4]zoneresult.MedalCount, error) {
	medalCounts := [4]zoneresult.MedalCount{}
	records, err := zoneobjective.SnapshotRecords(state)
	if err != nil {
		return medalCounts, fmt.Errorf("objectiveSnapshot: %w", err)
	}
	for _, record := range records {
		for playerIndex, medal := range record.State {
			switch medal {
			case 0, 1:
			case 2:
				medalCounts[playerIndex].Bronze++
			case 3:
				medalCounts[playerIndex].Silver++
			case 4:
				medalCounts[playerIndex].Gold++
			default:
				return medalCounts, fmt.Errorf(
					"objectiveMedal[%#x:%d]: %d",
					record.ObjectiveID, playerIndex, medal,
				)
			}
		}
	}
	return medalCounts, nil
}

func (r gameplayStatusRuntime) reserveCampaignBeamOut(
	sessionKey string, generation uint64,
) (gameplayPeerSession, bool) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil ||
		peerSession.zone.Boss() == nil {
		return gameplayPeerSession{}, false
	}
	_, isReserved := peerSession.zone.ReserveResult(
		zoneResultMember(peerSession),
	)
	if !isReserved {
		return gameplayPeerSession{}, false
	}
	return peerSession, true
}

func (r gameplayStatusRuntime) commitCampaignExperience(
	ctx context.Context, peerSession gameplayPeerSession,
) (zone.ExperienceCommit, error) {
	if peerSession.zone == nil || peerSession.binding.UserID == 0 || r.progression == nil {
		return zone.ExperienceCommit{}, errors.New("campaign experience unavailable")
	}
	commit, isReserved := peerSession.zone.ReserveExperienceCommit(peerSession.binding.UserID)
	if !isReserved {
		if commit.IsCommitted {
			return commit, nil
		}
		return zone.ExperienceCommit{}, errors.New("campaign experience busy")
	}
	experience, err := r.progression.GrantCampaignExperience(
		ctx, int64(peerSession.binding.UserID), peerSession.zone.CompletionID(),
		commit.Amount,
	)
	if err != nil {
		peerSession.zone.RollbackExperienceCommit(peerSession.binding.UserID)
		return zone.ExperienceCommit{}, fmt.Errorf("experienceGrant: %w", err)
	}
	if !peerSession.zone.CommitExperience(
		peerSession.binding.UserID, experience.CumulativeXP, experience.Level,
	) {
		return zone.ExperienceCommit{}, errors.New("campaign experience commit rejected")
	}
	commit.CumulativeXP = experience.CumulativeXP
	commit.Level = experience.Level
	commit.IsReserved = false
	commit.IsCommitted = true
	return commit, nil
}

func (r gameplayStatusRuntime) rollbackCampaignBeamOut(
	sessionKey string, generation uint64,
) error {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil {
		return nil
	}
	err := peerSession.zone.RollbackResult(zoneResultMember(peerSession))
	if err != nil {
		return fmt.Errorf("campaignBeamRollback: %w", err)
	}
	return nil
}

func (r gameplayStatusRuntime) commitCampaignBeamOut(
	sessionKey string, generation uint64, snapshot zoneresult.Snapshot,
) (gameplayPeerSession, bool) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil ||
		peerSession.zone.Boss() == nil {
		return gameplayPeerSession{}, false
	}
	bossObjectID, isReserved := peerSession.zone.
		ReservedResultBossObjectID(zoneResultMember(peerSession))
	if !isReserved {
		return gameplayPeerSession{}, false
	}
	if snapshot.BossObjectID != 0 && snapshot.BossObjectID != bossObjectID {
		return gameplayPeerSession{}, false
	}
	snapshot.BossObjectID = bossObjectID
	resultSession, err := peerSession.zone.CommitResult(
		zoneResultMember(peerSession), snapshot,
	)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet campaign result transaction rejected for %s: %v",
				sessionKey, err,
			)
		}
		return gameplayPeerSession{}, false
	}
	peerSession.chainResult = resultSession
	if snapshot.FinalLevel != 0 {
		peerSession.binding.AvatarLevel = snapshot.FinalLevel
		peerSession.binding.AvatarXP = float32(snapshot.FinalExperience)
		peerSession.binding.StartingAvatarXP = float32(snapshot.FinalExperience)
	}
	r.registry.sessions[sessionKey] = peerSession
	return peerSession, true
}

func zoneResultMember(peerSession gameplayPeerSession) zone.Member {
	return zone.Member{
		UserID:         peerSession.binding.UserID,
		PeerGeneration: peerSession.generation,
	}
}

func (r gameplayStatusRuntime) completeTutorial(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
) ([][]byte, error) {
	sessionKey := packet.Address.String()
	generation := peerSession.generation
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isReserved := false
	var err error
	if isFound && currentSession.generation == generation {
		isReserved, err = currentSession.beginTutorialCompletion()
	}
	if isReserved {
		r.registry.sessions[sessionKey] = currentSession
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("statusBeamReserve: %w", err)
	}
	if !isReserved {
		r.logger.Printf(
			"RakNet tutorial Beam Out rejected for %s: incomplete or already terminal",
			packet.Address,
		)
		return nil, nil
	}
	r.registry.mutex.RLock()
	currentSession, isFound = r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == generation &&
		currentSession.zone != nil && currentSession.zone.Boss() != nil
	if isCurrent {
		_, isCurrent = currentSession.zone.Boss().
			ReservedBeamOutBossObjectID()
	}
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if currentSession.deployedCreatureIndex >=
		uint32(len(currentSession.binding.Creatures)) {
		rollbackErr := r.rollbackTutorialCompletion(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusBeamCharacter: %w",
			errors.Join(errors.New("tutorial beam character missing"), rollbackErr),
		)
	}
	beamName := campaignCharacterBeam(
		currentSession.binding.Creatures[currentSession.deployedCreatureIndex], false,
	)
	beamPackets, err := heroraknet.BeamOut(
		currentSession.deployedObjectID, beamName,
		zonePosition(currentSession.playerPosition), packet.SourceTime,
	)
	if err != nil {
		rollbackErr := r.rollbackTutorialCompletion(sessionKey, generation)
		return nil, fmt.Errorf("statusBeamPresentation: %w", errors.Join(err, rollbackErr))
	}
	completion, err := tutorialprogress.Complete(
		ctx, r.progression, int64(currentSession.binding.UserID),
	)
	if err != nil {
		rollbackErr := r.rollbackTutorialCompletion(sessionKey, generation)
		return nil, fmt.Errorf(
			"statusTutorialComplete: %w", errors.Join(err, rollbackErr),
		)
	}
	completionPacket, err := raknet.MarshalApplication(raknet.TutorialCompleteMessage{
		CumulativeXP: int32(completion.CumulativeXP),
	})
	if err != nil {
		return nil, fmt.Errorf("statusTutorialSnapshot: %w", err)
	}
	// TutorialGame/0 applies XP and starts the fade. It does not leave the
	// network game. POST_GAME on Blaze must trigger the native leave handshake;
	// QuickGame/0 only switches scenes and leaves the client marked as joined.
	r.registry.mutex.Lock()
	currentSession, isFound = r.registry.sessions[sessionKey]
	isCommitted := false
	if isFound && currentSession.generation == generation {
		isCommitted, err = currentSession.commitTutorialCompletion()
	}
	if isCommitted {
		r.registry.sessions[sessionKey] = currentSession
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("statusBeamCommit: %w", err)
	}
	if !isCommitted {
		return nil, nil
	}
	completionStep := tutorialCompletionStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		packets: [][]byte{completionPacket},
	}
	autonomousPacket := packet.Autonomous()
	isCompletionScheduled := false
	if autonomousPacket.ScheduleGroup != nil || autonomousPacket.ScheduleGroupResult != nil {
		cancelCompletion, scheduleErr := autonomousPacket.ScheduleProducers([]raknet.ScheduledPacketProducer{{
			Delay: tutorialCompletionBeamDuration, Produce: completionStep.produce,
			AfterCommit: completionStep.finish,
		}})
		err = scheduleErr
		if err == nil && cancelCompletion == nil {
			err = errors.New("tutorial completion cancellation unavailable")
		}
		// The completion is autonomous; transport disconnect cancels its timer.
		isCompletionScheduled = err == nil
		if err != nil && r.logger != nil {
			r.logger.Printf(
				"RakNet tutorial delayed completion not scheduled for %s: %v",
				packet.Address, err,
			)
		}
	} else if r.logger != nil {
		r.logger.Printf(
			"RakNet tutorial delayed completion not scheduled for %s: scheduler unavailable",
			packet.Address,
		)
	}
	stopGameplayPeerSession(currentSession, r.modifierPool, r.effectPool)
	r.logger.Printf(
		"RakNet tutorial Beam Out accepted for %s; progression persisted cumulativeXP=%d changed=%t transition=BlazePostGame transition_delay_ms=%d transition_scheduled=%t tutorialSnapshot=true",
		packet.Address, completion.CumulativeXP, completion.IsChanged,
		tutorialCompletionBeamDuration.Milliseconds(), isCompletionScheduled,
	)
	return beamPackets, nil
}

func (r gameplayStatusRuntime) begin(
	ctx context.Context, packet raknet.Packet,
) (gameplayStatusContext, error) {
	status, err := raknet.DecodePlayerStatus(packet.Payload)
	if err != nil {
		return gameplayStatusContext{}, fmt.Errorf("statusDecode: %w", err)
	}
	r.logger.Printf(
		"RakNet gameplay status from %s status=%d progress=%g",
		packet.Address, status.Status, status.Progress,
	)
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	r.registry.mutex.RUnlock()
	result := gameplayStatusContext{
		status: status, peerSession: peerSession, isFound: isFound,
	}
	if !isFound || status.Status != 8 ||
		peerSession.binding.Mode != game.ModeTutorial ||
		!peerSession.isZoneGameOver() {
		return result, nil
	}
	restartedSession, isRestarted, err := r.restartTutorial(
		ctx, packet.Address.String(), peerSession,
	)
	if err != nil {
		return gameplayStatusContext{}, fmt.Errorf("statusRestart: %w", err)
	}
	if !isRestarted {
		return gameplayStatusContext{}, nil
	}
	result.peerSession = restartedSession
	result.isFound = true
	return result, nil
}

func (r gameplayStatusRuntime) restartTutorial(
	ctx context.Context, sessionKey string, previousSession gameplayPeerSession,
) (gameplayPeerSession, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isReserved := isFound && peerSession.generation == previousSession.generation &&
		peerSession.beginTutorialRestart()
	if isReserved {
		r.registry.sessions[sessionKey] = peerSession
		previousSession = peerSession
	}
	r.registry.mutex.Unlock()
	if !isReserved {
		return gameplayPeerSession{}, false, nil
	}
	if r.progression == nil {
		r.rollbackTutorialRestart(sessionKey, previousSession.generation)
		return gameplayPeerSession{}, false, errors.New("tutorial restart progression unavailable")
	}
	experience, err := tutorialprogress.Restart(
		ctx, r.progression, int64(previousSession.binding.UserID),
	)
	if err != nil {
		r.rollbackTutorialRestart(sessionKey, previousSession.generation)
		return gameplayPeerSession{}, false, fmt.Errorf("tutorialExperience: %w", err)
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	isCurrent := isCurrentFound && currentSession.generation == previousSession.generation &&
		currentSession.isTutorialRestartReserved()
	if isCurrent {
		peerSession = restartTutorialSession(
			currentSession, r.registry.lifecycle.nextGeneration(), experience,
		)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		return gameplayPeerSession{}, false, nil
	}
	stopGameplayPeerSession(previousSession, r.modifierPool, r.effectPool)
	r.logger.Printf(
		"RakNet tutorial terminal session replaced for %s epoch=%d",
		sessionKey, peerSession.generation,
	)
	return peerSession, true, nil
}

func (r gameplayStatusRuntime) rollbackTutorialRestart(
	sessionKey string, generation uint64,
) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		return
	}
	peerSession.rollbackTutorialRestart()
	r.registry.sessions[sessionKey] = peerSession
}

func (r gameplayStatusRuntime) rollbackTutorialCompletion(
	sessionKey string, generation uint64,
) error {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		return nil
	}
	err := peerSession.rollbackTutorialCompletion()
	if err != nil {
		return fmt.Errorf("beamRollback: %w", err)
	}
	r.registry.sessions[sessionKey] = peerSession
	return nil
}

type campaignResultRuntime struct {
	registry           *gameplaySessionRegistry
	chain              campaignChainProgression
	cashOutProgression campaignCashOutProgression
	gameplayJoin       *game.GameplayJoin
	modifierPool       *modifierPool
	effectPool         *attachedEffectPool
	preparation        campaignPreparation
	logger             *log.Logger
}

func (r campaignResultRuntime) handle(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	r.registry.mutex.RUnlock()
	if !isFound {
		return nil, nil
	}
	if peerSession.binding.Mode == game.ModeChain && peerSession.chainResult != nil {
		return r.handleActiveResult(ctx, packet, peerSession)
	}
	return r.handlePreparation(ctx, packet, peerSession)
}

func (r campaignResultRuntime) handleActiveResult(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
) ([][]byte, error) {
	resultSnapshot := peerSession.chainResult.Snapshot()
	r.logger.Printf(
		"RakNet campaign result command from %s phase=%d payload=%x",
		packet.Address, resultSnapshot.Phase, packet.Payload,
	)
	if resultSnapshot.Phase == zoneresult.ChainVoting &&
		len(packet.Payload) == 1 && packet.Payload[0] == 0 {
		return r.votingData(packet, resultSnapshot)
	}
	chainCommand, commandErr := raknet.DecodeChainPlayerCommand(packet.Payload)
	isContinueRequest := commandErr == nil &&
		chainCommand.Type == raknet.ChainPlayerSelectContinue &&
		chainCommand.Choice == 1 &&
		chainCommand.SelectedRecordID != 0
	squadID := chainCommand.SelectedRecordID
	nextLevel := resultSnapshot.NextLevel
	isContinuePhase := resultSnapshot.Phase == zoneresult.ChainVoting ||
		resultSnapshot.IsContinueReplay(squadID, nextLevel)
	if isContinueRequest && isContinuePhase && squadID == peerSession.binding.SquadID {
		if resultSnapshot.IsTerminal {
			return r.castVote(
				ctx, packet, peerSession, resultSnapshot,
				zoneresult.VoteChoiceCashOut,
			)
		}
		return r.castVote(
			ctx, packet, peerSession, resultSnapshot,
			zoneresult.VoteChoiceContinue,
		)
	}
	if resultSnapshot.Phase == zoneresult.ChainVoting &&
		len(packet.Payload) == 1 && packet.Payload[0] == 2 {
		return r.castVote(
			ctx, packet, peerSession, resultSnapshot,
			zoneresult.VoteChoiceCashOut,
		)
	}
	if resultSnapshot.Phase == zoneresult.ChainCashOut &&
		len(packet.Payload) == 1 && packet.Payload[0] == 4 {
		return r.presentCashOut(ctx, packet, peerSession, resultSnapshot)
	}
	if commandErr != nil {
		r.logger.Printf(
			"RakNet campaign result command rejected for %s phase=%d payload=%x error=%v",
			packet.Address, resultSnapshot.Phase, packet.Payload, commandErr,
		)
		return nil, nil
	}
	r.logger.Printf(
		"RakNet campaign result command ignored for %s phase=%d type=%d",
		packet.Address, resultSnapshot.Phase, chainCommand.Type,
	)
	return nil, nil
}

func (r campaignResultRuntime) poll(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	if packet.Address == nil {
		return nil, errors.New("campaign result poll address unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	r.registry.mutex.RUnlock()
	if !isFound || peerSession.binding.Mode != game.ModeChain ||
		peerSession.chainResult == nil {
		return nil, nil
	}
	snapshot := peerSession.chainResult.Snapshot()
	switch snapshot.Phase {
	case zoneresult.ChainVoting:
		return r.consumeVote(ctx, packet, peerSession, snapshot)
	case zoneresult.ContinueReserved:
		if peerSession.stage.IsDungeon() {
			return nil, nil
		}
		setupPacket, err := marshalZoneSetup(peerSession.binding)
		if err != nil {
			return nil, fmt.Errorf("campaignContinueReplay: %w", err)
		}
		return [][]byte{setupPacket}, nil
	case zoneresult.ChainCashOut:
		if !snapshot.CashOutReceipt.IsCommitted {
			cashOutPacket, err := resultraknet.CashOutRoute()
			if err != nil {
				return nil, fmt.Errorf("campaignCashOutReplay: %w", err)
			}
			return [][]byte{cashOutPacket}, nil
		}
		cashOutPacket, err := resultraknet.CashOutReceipt(
			resultraknet.CashOutReceiptRequest{
				Receipt:            snapshot.CashOutReceipt,
				CompletedIndex:     snapshot.CompletedIndex,
				PlanetsCompleted:   snapshot.PlanetsCompleted,
				MedalCounts:        snapshot.MedalCounts,
				StartingExperience: snapshot.StartingExperience,
				FinalExperience:    snapshot.FinalExperience,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("campaignCashOutReceiptReplay: %w", err)
		}
		return [][]byte{cashOutPacket}, nil
	default:
		return nil, nil
	}
}

func (r campaignResultRuntime) castVote(
	ctx context.Context,
	packet raknet.Packet,
	peerSession gameplayPeerSession,
	snapshot zoneresult.Snapshot,
	choice zoneresult.VoteChoice,
) ([][]byte, error) {
	if peerSession.zone == nil {
		return nil, errors.New("campaign result vote unavailable")
	}
	voteSnapshot, isAccepted := peerSession.zone.CastResultVote(
		zoneResultMember(peerSession), choice,
	)
	if r.logger != nil {
		r.logger.Printf(
			"RakNet campaign result vote for %s result=%d accepted=%t members=%d continue=%d cashout=%d decision=%d",
			packet.Address, snapshot.ResultID, isAccepted,
			voteSnapshot.MemberCount, voteSnapshot.ContinueCount,
			voteSnapshot.CashOutCount, voteSnapshot.Decision,
		)
	}
	if !isAccepted && voteSnapshot.Decision == zoneresult.VoteDecisionPending {
		return nil, nil
	}
	return r.consumeVote(ctx, packet, peerSession, snapshot)
}

func (r campaignResultRuntime) consumeVote(
	ctx context.Context,
	packet raknet.Packet,
	peerSession gameplayPeerSession,
	snapshot zoneresult.Snapshot,
) ([][]byte, error) {
	if peerSession.zone == nil {
		return nil, nil
	}
	decision, isFound := peerSession.zone.ResultVoteDecision(
		zoneResultMember(peerSession),
	)
	if !isFound {
		return nil, nil
	}
	switch decision {
	case zoneresult.VoteDecisionContinue:
		return r.continueChain(
			ctx, packet, peerSession, snapshot, peerSession.binding.SquadID,
		)
	case zoneresult.VoteDecisionCashOut:
		return r.cashOut(packet, peerSession, snapshot)
	default:
		return nil, fmt.Errorf("campaignVoteDecision: %d", decision)
	}
}

func (r campaignResultRuntime) votingData(
	packet raknet.Packet, snapshot zoneresult.Snapshot,
) ([][]byte, error) {
	votePacket, err := resultraknet.Vote(snapshot)
	if err != nil {
		return nil, fmt.Errorf("campaignVote: %w", err)
	}
	countdownPacket, err := resultraknet.Countdown(
		float32(zoneresult.VoteDuration.Seconds()),
	)
	if err != nil {
		return nil, fmt.Errorf("campaignCountdown: %w", err)
	}
	r.logger.Printf(
		"RakNet campaign compatibility voting data sent to %s result=%d",
		packet.Address, snapshot.ResultID,
	)
	return [][]byte{votePacket, countdownPacket}, nil
}

func (s gameplayPeerSession) deployedResourceMaximum() (float32, float32, error) {
	if s.deployedObjectID == 0 {
		return 0, 0, errors.New("deployed hero unavailable")
	}
	if s.binding.Mode == game.ModeArena {
		creatureIndex := s.deployedCreatureIndex
		if creatureIndex >= uint32(len(s.binding.Creatures)) ||
			creatureIndex >= uint32(len(s.maximumHitPoints)) ||
			creatureIndex >= uint32(len(s.maximumManaPoints)) {
			return 0, 0, errors.New("deployed arena hero unavailable")
		}
		maximumHitPoint := s.maximumHitPoints[creatureIndex]
		maximumManaPoint := s.maximumManaPoints[creatureIndex]
		if maximumHitPoint <= 0 || maximumManaPoint <= 0 {
			return 0, 0, errors.New("deployed arena hero resources unavailable")
		}
		return maximumHitPoint, maximumManaPoint, nil
	}
	if s.zone == nil || s.zone.Hero() == nil {
		return 0, 0, errors.New("deployed hero unavailable")
	}
	actor, isFound := s.zone.Hero().Snapshot(
		s.binding.UserID, s.generation,
	)
	if !isFound || actor.ObjectID != s.deployedObjectID {
		return 0, 0, errors.New("deployed hero resources unavailable")
	}
	return actor.MaximumHitPoint, actor.MaximumManaPoint, nil
}

func (s *gameplayPeerSession) applyDeveloperResourceCommand(
	command game.PlayerResourceCommand, timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s == nil || s.squad == nil || s.isZoneTerminal() {
		return nil, sporenet.PlayerStatDelta{}, errors.New("resource session unavailable")
	}
	if command.IsHeal {
		packets, err := s.healLivingSquadToFull()
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("healSquad: %w", err)
		}
		return packets, sporenet.PlayerStatDelta{}, nil
	}
	hitPointMaximum, powerPointMaximum, err := s.deployedResourceMaximum()
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("resourceMaximum: %w", err)
	}
	mutation, err := developerfeature.ApplyResource(
		developerfeature.ResourceState{
			HitPoint: s.deployedHitPoint(), HitPointMaximum: hitPointMaximum,
			PowerPoint: s.deployedManaPoint(), PowerPointMaximum: powerPointMaximum,
		},
		developerfeature.ResourceCommand{
			Damage: command.Damage, PowerReduction: command.PowerReduction,
			IsHeal: command.IsHeal, IsPowerFill: command.IsPowerFill,
		},
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("resourcePlan: %w", err)
	}
	if mutation.IsHitPointChanged && mutation.Damage == 0 {
		_, err = s.setDeployedHitPoints(mutation.HitPoint)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("healApply: %w", err)
		}
		packet, marshalErr := heroraknet.HitPoint(
			s.deployedObjectID, mutation.HitPoint,
		)
		if marshalErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("healMarshal: %w", marshalErr)
		}
		packets := [][]byte{packet}
		if s.binding.Mode == game.ModeChain {
			resourcePacket, resourceErr := s.marshalCampaignCharacterResource(s.deployedCreatureIndex)
			if resourceErr != nil {
				return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("healResource: %w", resourceErr)
			}
			packets = append(packets, resourcePacket)
		}
		return packets, sporenet.PlayerStatDelta{}, nil
	}
	if mutation.IsPowerPointChanged {
		err = s.setDeployedManaPoints(mutation.PowerPoint)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("powerApply: %w", err)
		}
		packet, marshalErr := heroraknet.ManaPoint(
			s.deployedObjectID, mutation.PowerPoint,
		)
		if marshalErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("powerMarshal: %w", marshalErr)
		}
		packets := [][]byte{packet}
		if s.binding.Mode == game.ModeChain {
			resourcePacket, resourceErr := s.marshalCampaignCharacterResource(s.deployedCreatureIndex)
			if resourceErr != nil {
				return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("powerResource: %w", resourceErr)
			}
			packets = append(packets, resourcePacket)
		}
		return packets, sporenet.PlayerStatDelta{}, nil
	}
	packets, err := developerraknet.Damage(developerraknet.DamageRequest{
		ObjectID: s.deployedObjectID,
		Position: game.Vec3{
			X: s.playerPosition.X,
			Y: s.playerPosition.Y,
			Z: s.playerPosition.Z,
		},
		Damage: mutation.Damage, HitPoint: mutation.HitPoint,
	})
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("damagePackets: %w", err)
	}
	if s.binding.Mode == game.ModeChain {
		packets, statDelta, transitionErr := s.applyCampaignDamageHitPackets(
			packets, s.deployedObjectID, mutation.HitPoint, timestamp, now,
		)
		if transitionErr != nil {
			return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("damageCampaign: %w", transitionErr)
		}
		return packets, statDelta, nil
	}
	packets, statDelta, transitionErr := s.applyDamageHitPackets(
		packets, mutation.HitPoint, timestamp,
	)
	if transitionErr != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("damageTutorial: %w", transitionErr)
	}
	return packets, statDelta, nil
}

func (s *gameplayPeerSession) healLivingSquadToFull() ([][]byte, error) {
	packets := make([][]byte, 0, squad.Size+1)
	for creatureIndex := uint32(0); creatureIndex < squad.Size; creatureIndex++ {
		character, isFound := s.squad.Character(creatureIndex)
		if !isFound || !character.IsAvailable || character.HitPoints <= 0 {
			continue
		}
		maximumHitPoint := s.characterHitPointMaximum(creatureIndex)
		_, err := s.setCampaignCharacterHitPoints(creatureIndex, maximumHitPoint)
		if err != nil {
			return nil, fmt.Errorf("healCharacter[%d]: %w", creatureIndex, err)
		}
		if creatureIndex == s.deployedCreatureIndex {
			packet, marshalErr := heroraknet.HitPoint(
				s.deployedObjectID, maximumHitPoint,
			)
			if marshalErr != nil {
				return nil, fmt.Errorf("healHitPoint[%d]: %w", creatureIndex, marshalErr)
			}
			packets = append(packets, packet)
		}
		resourcePacket, marshalErr := s.marshalCampaignCharacterResource(creatureIndex)
		if marshalErr != nil {
			return nil, fmt.Errorf("healResource[%d]: %w", creatureIndex, marshalErr)
		}
		packets = append(packets, resourcePacket)
	}
	return packets, nil
}

var campaignBossDeveloperPosition = raknet.Vector3{X: 948.3736, Y: 674.0089, Z: 0.0880}

func (s *gameplayPeerSession) applyDeveloperEventCommand(
	command game.PlayerEventCommand, now time.Time, timestamp uint64,
) ([][]byte, error) {
	if s == nil || s.binding.Mode != game.ModeChain || s.squad == nil ||
		s.deployedObjectID == 0 || s.isZoneTerminal() {
		return nil, errors.New("event session unavailable")
	}
	plan, err := developerfeature.PlanEvent(developerfeature.EventCommand{
		Name: command.Name,
		Position: developerfeature.Position{
			X: command.Position.X, Y: command.Position.Y, Z: command.Position.Z,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("eventPlan: %w", err)
	}
	switch plan.Kind {
	case developerfeature.EventReset:
		return s.applyDeveloperResetCommand(now)
	case developerfeature.EventRecap:
		return s.applyDeveloperRecapCommand()
	case developerfeature.EventGoto:
		destination := raknet.Vector3{
			X: plan.Position.X, Y: plan.Position.Y, Z: plan.Position.Z,
		}
		interruptedBasic := s.interruptBasicForMovement()
		if interruptedBasic != nil {
			interruptedBasic.Stop()
		}
		err := s.teleportPlayer(now, destination)
		if err != nil {
			return nil, fmt.Errorf("gotoMove: %w", err)
		}
		contactPackets, err := s.collectCampaignTeleportContacts(
			destination, now, timestamp, raknet.Quaternion{W: 1},
		)
		if err != nil {
			return nil, fmt.Errorf("gotoContact: %w", err)
		}
		packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
			ObjectID: s.deployedObjectID, Position: zonePosition(destination),
			Orientation: game.Quaternion{W: 1},
		})
		if err != nil {
			return nil, fmt.Errorf("gotoMarshal: %w", err)
		}
		return append(packets, contactPackets...), nil
	case developerfeature.EventBossStart:
		bossPlan, err := s.zone.PlanDeveloperBoss(
			s.binding.Level, s.binding.GameID, s.binding.ChainLevelIndex,
		)
		if err != nil {
			return nil, fmt.Errorf("eventBossPlan: %w", err)
		}
		publication := bossPlan.Publication
		plans := bossPlan.Actors
		bossPosition := campaignBossDeveloperPosition
		if bossPlan.IsPositionAuthored {
			bossPosition = raknet.Vector3{
				X: plans[0].Position.X, Y: plans[0].Position.Y, Z: plans[0].Position.Z,
			}
		}
		packets, err := npcraknet.TargetedSpawns(plans, s.deployedObjectID)
		if err != nil {
			return nil, fmt.Errorf("eventBossSpawnMarshal: %w", err)
		}
		if !isCampaignBossIntroDelayed(plans[0]) {
			activePacket, activeErr := bossraknet.Active(
				plans[0].ObjectID, zoneboss.IsFinalBossNoun(plans[0].NounName),
			)
			if activeErr != nil {
				return nil, fmt.Errorf("eventBossActiveMarshal: %w", activeErr)
			}
			packets = append(packets, activePacket)
		}
		teleportPackets, err := developerraknet.Teleport(
			s.deployedObjectID, bossPosition,
		)
		if err != nil {
			return nil, fmt.Errorf("eventBossTeleportMarshal: %w", err)
		}
		err = s.zone.AdmitDeveloperBoss(
			publication, s.deployedObjectID, plans,
		)
		if err != nil {
			return nil, fmt.Errorf("eventBossCommit: %w", err)
		}
		err = s.teleportPlayer(now, bossPosition)
		if err != nil {
			return nil, fmt.Errorf("eventBossTeleport: %w", err)
		}
		return append(teleportPackets, packets...), nil
	case developerfeature.EventBossComplete:
		if s.zone.Boss() == nil {
			return nil, errors.New("boss event unavailable")
		}
		leaderObjectID, err := s.zone.Boss().CompleteForDeveloper()
		if err != nil {
			return nil, fmt.Errorf("eventBossComplete: %w", err)
		}
		packet, err := bossraknet.Complete(leaderObjectID)
		if err != nil {
			return nil, fmt.Errorf("eventBossMarshal: %w", err)
		}
		return [][]byte{packet}, nil
	case developerfeature.EventSecurityNext:
	default:
		return nil, errors.New("event plan unavailable")
	}
	if s.zone.Security() == nil {
		return nil, errors.New("security session unavailable")
	}
	routeIndex, securityObjectID, isRouteFound :=
		s.zone.Security().Current()
	if !isRouteFound || routeIndex >= zonesecurity.CountRoutes() {
		return nil, errors.New("security route complete")
	}
	teleport, isTeleportFound := zonesecurity.Route(routeIndex)
	if !isTeleportFound {
		return nil, fmt.Errorf("eventSecurityRoute[%d]: missing", routeIndex)
	}
	activationPackets, err := securityraknet.State(
		securityObjectID, teleport, true, true,
	)
	if err != nil {
		return nil, fmt.Errorf("eventSecurityActivation: %w", err)
	}
	packets, err := securityraknet.Teleport(
		securityraknet.TeleportRequest{
			ObjectID: s.deployedObjectID, Teleport: teleport,
			Orientation: raknet.Quaternion{W: 1}, Timestamp: timestamp,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("eventSecurityMarshal: %w", err)
	}
	destination := raknet.Vector3{
		X: teleport.Destination.X, Y: teleport.Destination.Y, Z: teleport.Destination.Z,
	}
	err = s.teleportPlayer(now, destination)
	if err != nil {
		return nil, fmt.Errorf("eventSecurityMove: %w", err)
	}
	err = s.zone.Security().Complete(routeIndex)
	if err != nil {
		return nil, fmt.Errorf("eventSecurityComplete: %w", err)
	}
	contactPackets, err := s.collectCampaignTeleportContacts(
		destination, now, timestamp, raknet.Quaternion{W: 1},
	)
	if err != nil {
		return nil, fmt.Errorf("eventSecurityContact: %w", err)
	}
	response := append(activationPackets, packets...)
	return append(response, contactPackets...), nil
}

func (s *gameplayPeerSession) applyDeveloperRecapCommand() ([][]byte, error) {
	if s == nil || s.squad == nil || s.binding.Mode != game.ModeChain ||
		s.deployedHitPoint() <= 0 || s.isHeroSelectionPending || s.isZoneTerminal() {
		return nil, errors.New("recap session unavailable")
	}
	packets := make([][]byte, 0, squad.Size)
	revivedIndexes := make([]uint32, 0, squad.Size-1)
	for index := uint32(0); index < squad.Size; index++ {
		character, isFound := s.squad.Character(index)
		if !isFound || !character.IsAvailable || character.HitPoints > 0 {
			continue
		}
		maximumHitPoint := s.characterHitPointMaximum(index)
		if maximumHitPoint <= 0 {
			return nil, fmt.Errorf("recapMaximum[%d]: invalid", index)
		}
		packet, err := s.marshalCampaignCharacterResourceValues(
			index, maximumHitPoint, character.ManaPoints,
		)
		if err != nil {
			return nil, fmt.Errorf("recapMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
		revivedIndexes = append(revivedIndexes, index)
	}
	for _, index := range revivedIndexes {
		maximumHitPoint := s.characterHitPointMaximum(index)
		_, err := s.squad.SetHitPoints(index, maximumHitPoint)
		if err != nil {
			return nil, fmt.Errorf("recapHealth[%d]: %w", index, err)
		}
	}
	if len(revivedIndexes) == 0 {
		return nil, nil
	}
	err := s.syncZoneSquadCheckpoint()
	if err != nil {
		return nil, fmt.Errorf("recapCheckpoint: %w", err)
	}
	return packets, nil
}

// applyDeveloperResetCommand releases transient player-side admission gates
// without changing campaign progress, enemies, loot, health, power, or durable
// profile state. The authoritative position is re-published so the client can
// recover from a stale local movement or deployment clock.
func (s *gameplayPeerSession) applyDeveloperResetCommand(
	now time.Time,
) ([][]byte, error) {
	teleportPackets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: s.deployedObjectID, Position: zonePosition(s.playerPosition),
		Orientation: game.Quaternion{W: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("resetMarshal: %w", err)
	}
	cooldownPacket, err := heroraknet.DeployCooldown(
		uint8(s.binding.Slot), s.deployedCreatureIndex, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("resetCooldown: %w", err)
	}
	baselinePackets, err := s.marshalDeveloperResetBaseline()
	if err != nil {
		return nil, fmt.Errorf("resetBaseline: %w", err)
	}

	s.campaignScheduleSession().StopAll()
	packets := make([][]byte, 0)
	summonPackets, summonErr := s.stopHeroSummons()
	if summonErr == nil {
		packets = append(packets, summonPackets...)
	}
	fireTempestPackets, fireTempestErr := s.stopFireTempestActive()
	if fireTempestErr == nil {
		packets = append(packets, fireTempestPackets...)
	}
	plasmaPackets, plasmaErr := s.stopPlasmaSentinelActive()
	if plasmaErr == nil {
		packets = append(packets, plasmaPackets...)
	}
	interruptedBasic := s.resetAbilityAdmissionForSwitch()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	if s.rideCancel != nil {
		s.rideCancel()
		s.rideCancel = nil
	}
	if s.obeliskCancel != nil {
		s.obeliskCancel()
		s.obeliskCancel = nil
	}
	if s.obeliskRun != nil {
		s.obeliskRun.Stop()
		s.obeliskRun = nil
	}
	if s.movementContactCancel != nil {
		s.movementContactCancel()
		s.movementContactCancel = nil
	}
	_ = s.stopPlayerMovement(now)
	s.abilityCooldownSession().ResetAll()
	if s.squad != nil {
		s.squad.ResetDeployCooldown()
	}
	s.teleporterContact = sim.SphereContactState{}
	s.campaignTunnelExitSource = game.Vec3{}
	s.campaignTunnelExitRadius = 0
	s.isCampaignTunnelExitPending = false
	s.isClientBossBoundaryPending = false
	packets = append(packets, teleportPackets...)
	packets = append(packets, baselinePackets...)
	packets = append(packets, cooldownPacket)
	return packets, nil
}

func (s *gameplayPeerSession) marshalDeveloperResetBaseline() ([][]byte, error) {
	if s == nil {
		return nil, errors.New("reset baseline unavailable")
	}
	return s.marshalResetBaselineAt(s.deployedHitPoint(), s.deployedManaPoint())
}

func (s *gameplayPeerSession) marshalResetBaselineAt(
	hitPoint float32, manaPoint float32,
) ([][]byte, error) {
	if s == nil || s.deployedObjectID == 0 {
		return nil, errors.New("reset baseline unavailable")
	}
	updatePacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID:  s.deployedObjectID,
		PositionX: s.playerPosition.X, PositionY: s.playerPosition.Y,
		PositionZ: s.playerPosition.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("resetObject: %w", err)
	}
	packets := [][]byte{updatePacket}
	for index, message := range raknet.HeroStateMessages(
		s.deployedObjectID, hitPoint, manaPoint,
	) {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("resetResource[%d]: %w", index, marshalErr)
		}
		packets = append(packets, packet)
	}
	resourcePacket, err := s.marshalCampaignCharacterResourceValues(
		s.deployedCreatureIndex, hitPoint, manaPoint,
	)
	if err != nil {
		return nil, fmt.Errorf("resetCharacter: %w", err)
	}
	controlledPacket, err := raknet.MarshalApplication(
		raknet.LabsPlayerControlledObjectMessage{
			Slot: uint8(s.binding.Slot), ObjectID: s.deployedObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resetControlled: %w", err)
	}
	deployPacket, err := raknet.MarshalApplication(
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex:   uint8(s.binding.Slot),
			CreatureIndex: s.deployedCreatureIndex,
			ObjectID:      s.deployedObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resetDeploy: %w", err)
	}
	return append(packets, resourcePacket, controlledPacket, deployPacket), nil
}

// applyDeveloperKillCommand defeats the current live campaign population
// through the same damage and encounter-transition authorities used by hero
// abilities. Presentation, death schedules, loot, and persistent statistics
// remain the transport caller's responsibility.
func (s *gameplayPeerSession) applyDeveloperKillCommand() (
	[]zoneability.AreaResult, []campaignDamageTransition, error,
) {
	if s == nil || s.binding.Mode != game.ModeChain || s.squad == nil ||
		s.deployedObjectID == 0 || s.isZoneTerminal() || s.zone.NPCs() == nil {
		return nil, nil, errors.New("kill session unavailable")
	}
	snapshot := s.zone.NPCs().LiveSnapshots()
	result := make([]zoneability.AreaResult, 0, len(snapshot))
	transition := make([]campaignDamageTransition, 0, len(snapshot))
	for index, enemy := range snapshot {
		if enemy.IsShieldActive {
			err := s.zone.NPCs().EndShield(enemy.Plan.ObjectID)
			if err != nil {
				return nil, nil, fmt.Errorf("killShield[%d]: %w", index, err)
			}
		}
		if enemy.IsTurtleActive {
			err := s.zone.NPCs().EndTurtle(enemy.Plan.ObjectID)
			if err != nil {
				return nil, nil, fmt.Errorf("killTurtle[%d]: %w", index, err)
			}
		}
		damage, err := s.zone.NPCs().Defeat(
			s.deployedObjectID, enemy.Plan.ObjectID,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("killDamage[%d]: %w", index, err)
		}
		next, err := s.applyCampaignDamageTransition(damage)
		if err != nil {
			return nil, nil, fmt.Errorf("killTransition[%d]: %w", index, err)
		}
		result = append(result, zoneability.AreaResult{Snapshot: enemy, Damage: damage})
		transition = append(transition, next)
	}
	return result, transition, nil
}

func (s *gameplayPeerSession) applyDeveloperVictoryCommand() ([]byte, uint32, error) {
	if s == nil || s.binding.Mode != game.ModeChain || s.squad == nil ||
		s.deployedObjectID == 0 || s.isZoneTerminal() || s.chainResult != nil {
		return nil, 0, errors.New("victory session unavailable")
	}
	if s.zone == nil {
		return nil, 0, errors.New("victory zone unavailable")
	}
	bossObjectID, err := s.zone.CompleteForDeveloper()
	if err != nil {
		return nil, 0, fmt.Errorf("victoryComplete: %w", err)
	}
	completionPacket, err := bossraknet.Complete(bossObjectID)
	if err != nil {
		return nil, 0, fmt.Errorf("victoryMarshal: %w", err)
	}
	return completionPacket, bossObjectID, nil
}

func (s *gameplayPeerSession) applyDeveloperTutorialVictoryCommand() (
	[]byte, uint32, error,
) {
	if s == nil || s.binding.Mode != game.ModeTutorial || s.squad == nil ||
		s.deployedObjectID == 0 || s.isZoneTerminal() || s.zone == nil {
		return nil, 0, errors.New("tutorial victory session unavailable")
	}
	bossObjectID, err := s.zone.CompleteForDeveloper()
	if err != nil {
		return nil, 0, fmt.Errorf("tutorialVictoryComplete: %w", err)
	}
	completionPacket, err := bossraknet.Complete(bossObjectID)
	if err != nil {
		return nil, 0, fmt.Errorf("tutorialVictoryMarshal: %w", err)
	}
	return completionPacket, bossObjectID, nil
}

func (s *gameplayPeerSession) applyDeveloperDefeatCommand() ([][]byte, error) {
	if s == nil || s.binding.Mode != game.ModeChain || s.squad == nil ||
		s.deployedObjectID == 0 || s.isZoneTerminal() {
		return nil, errors.New("defeat session unavailable")
	}
	interruptedBasic := s.resetAbilityAdmissionForSwitch()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	s.stopCampaignNPCProjectiles()
	passivePackets, err := s.stopCampaignSagePassive()
	if err != nil {
		return nil, fmt.Errorf("defeatPassive: %w", err)
	}
	packets := append([][]byte(nil), passivePackets...)
	summonPackets, err := s.stopHeroSummons()
	if err != nil {
		return nil, fmt.Errorf("defeatHeroSummon: %w", err)
	}
	packets = append(packets, summonPackets...)
	fireTempestPackets, err := s.stopFireTempestActive()
	if err != nil {
		return nil, fmt.Errorf("defeatFireTempest: %w", err)
	}
	packets = append(packets, fireTempestPackets...)
	plasmaPackets, err := s.stopPlasmaSentinelActive()
	if err != nil {
		return nil, fmt.Errorf("defeatPlasmaSentinel: %w", err)
	}
	packets = append(packets, plasmaPackets...)
	for index := uint32(0); index < squad.Size; index++ {
		character, isFound := s.squad.Character(index)
		if !isFound || !character.IsAvailable {
			continue
		}
		_, err = s.setCampaignCharacterHitPoints(index, 0)
		if err != nil {
			return nil, fmt.Errorf("defeatHealth[%d]: %w", index, err)
		}
		resourcePacket, marshalErr := s.marshalCampaignCharacterResource(index)
		if marshalErr != nil {
			return nil, fmt.Errorf("defeatResource[%d]: %w", index, marshalErr)
		}
		packets = append(packets, resourcePacket)
	}
	if !s.squad.IsGameOver() {
		return nil, errors.New("defeat did not latch game over")
	}
	if s.zone.NPCs() != nil {
		err = s.zone.NPCs().ClearTargets()
		if err != nil {
			return nil, fmt.Errorf("defeatTargets: %w", err)
		}
		targetPackets, marshalErr := npcraknet.TargetUpdates(
			s.zone.NPCs().Snapshots(),
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("defeatTargetMarshal: %w", marshalErr)
		}
		packets = append(packets, targetPackets...)
	}
	s.isHeroSelectionPending = false
	s.isHeroSelectionScheduled = false
	s.heroSelectionReadyAt = time.Time{}
	gameOverPacket, err := outcomeraknet.GameOver()
	if err != nil {
		return nil, fmt.Errorf("defeatGameOver: %w", err)
	}
	return append(packets, gameOverPacket), nil
}

func (r campaignResultRuntime) continueChain(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
	snapshot zoneresult.Snapshot, squadID uint32,
) ([][]byte, error) {
	nextBinding := peerSession.binding
	nextBinding.Level = snapshot.NextLevel
	nextBinding.ChainLevelIndex = snapshot.CompletedIndex + 1
	nextBinding.Difficulty = nextBinding.ChainLevelIndex
	nextBinding.IsCatalystUnlocked = nextBinding.ChainLevelIndex >= 3 ||
		nextBinding.ChainProgression >= 3
	for index := range nextBinding.Creatures {
		maximumManaPoint := peerSession.maximumManaPoints[index]
		if maximumManaPoint <= 0 {
			continue
		}
		nextBinding.Creatures[index].PowerPoint = maximumManaPoint
	}
	nextBinding.RefreshReplay()
	setupPacket, err := marshalZoneSetup(nextBinding)
	if err != nil {
		return nil, fmt.Errorf("campaignContinueSetup: %w", err)
	}
	if snapshot.IsContinueReplay(squadID, snapshot.NextLevel) {
		r.logger.Printf(
			"RakNet campaign continue replayed for %s result=%d level=%q",
			packet.Address, snapshot.ResultID, snapshot.NextLevel,
		)
		return [][]byte{setupPacket}, nil
	}
	if r.chain == nil {
		return nil, errors.New("campaignContinueProgression: unavailable")
	}
	chainProgression, _, err := r.chain.AdvanceChainProgression(
		ctx, int64(snapshot.UserID), snapshot.CompletedIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignContinueProgression: %w", err)
	}
	r.registry.mutex.Lock()
	continueSession, isContinueFound := r.registry.sessions[packet.Address.String()]
	isAccepted := isContinueFound && continueSession.generation == peerSession.generation &&
		continueSession.chainResult != nil &&
		continueSession.chainResult.AcceptContinue(squadID, snapshot.NextLevel)
	if isAccepted {
		nextGeneration := r.registry.lifecycle.nextGeneration()
		crystalInventory, isCrystalFound := continueSession.zone.CrystalInventory(
			continueSession.binding.UserID, continueSession.generation,
		)
		if !isCrystalFound {
			r.registry.mutex.Unlock()
			return nil, errors.New("campaignContinueCrystal: unavailable")
		}
		nextBinding.ChainProgression = max(nextBinding.ChainProgression, chainProgression)
		nextBinding.RefreshReplay()
		r.registry.sessions[packet.Address.String()] = gameplayPeerSession{
			zoneMembership: zoneMembership{generation: nextGeneration},
			chainPeerRuntime: chainPeerRuntime{
				chainPlanetsCompleted: snapshot.PlanetsCompleted,
				chainMedalCounts:      snapshot.MedalCounts,
			},
			controlledHeroState: controlledHeroState{
				maximumHitPoints:  continueSession.maximumHitPoints,
				maximumManaPoints: continueSession.maximumManaPoints,
			},
			binding:             nextBinding,
			transportGeneration: continueSession.transportGeneration,
			schedulePackets:     continueSession.schedulePackets,
			crystalInventory:    crystalInventory,
		}
	}
	r.registry.mutex.Unlock()
	if !isAccepted {
		return nil, nil
	}
	stopGameplayPeerSession(continueSession, r.modifierPool, r.effectPool)
	r.logger.Printf(
		"RakNet campaign continue accepted for %s result=%d level=%q chain_progression=%d without fabricated reward",
		packet.Address, snapshot.ResultID, snapshot.NextLevel, chainProgression,
	)
	return [][]byte{setupPacket}, nil
}

func (r campaignResultRuntime) cashOut(
	packet raknet.Packet, peerSession gameplayPeerSession,
	snapshot zoneresult.Snapshot,
) ([][]byte, error) {
	cashOutPacket, err := resultraknet.CashOutRoute()
	if err != nil {
		return nil, fmt.Errorf("campaignCashOut: %w", err)
	}
	r.registry.mutex.Lock()
	cashOutSession, isCashOutFound := r.registry.sessions[packet.Address.String()]
	isAccepted := isCashOutFound && cashOutSession.generation == peerSession.generation &&
		cashOutSession.chainResult != nil && cashOutSession.chainResult.AcceptCashOut()
	if isAccepted {
		r.registry.sessions[packet.Address.String()] = cashOutSession
	}
	r.registry.mutex.Unlock()
	if !isAccepted {
		return nil, nil
	}
	r.logger.Printf(
		"RakNet campaign cashout accepted for %s result=%d; awaiting presentation request",
		packet.Address, snapshot.ResultID,
	)
	return [][]byte{cashOutPacket}, nil
}

func (r campaignResultRuntime) presentCashOut(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
	snapshot zoneresult.Snapshot,
) ([][]byte, error) {
	rewardSubjects := make([]zoneresult.RewardSubject, 0, len(peerSession.binding.ActivatedCreatures))
	for _, creature := range peerSession.binding.ActivatedCreatures {
		if creature.Noun == 0 || creature.ClassType == "" || creature.ElementType == "" {
			continue
		}
		rewardSubjects = append(rewardSubjects, zoneresult.RewardSubject{
			ClassType: creature.ClassType, ElementType: creature.ElementType,
			AccountLevel: peerSession.binding.AvatarLevel,
		})
	}
	if len(rewardSubjects) == 0 {
		for _, creature := range peerSession.binding.Creatures {
			if creature.Noun == 0 || creature.ClassType == "" || creature.ElementType == "" {
				continue
			}
			rewardSubjects = append(rewardSubjects, zoneresult.RewardSubject{
				ClassType: creature.ClassType, ElementType: creature.ElementType,
				AccountLevel: peerSession.binding.AvatarLevel,
			})
		}
	}
	if len(rewardSubjects) == 0 {
		return nil, errors.New("campaignCashOutSubject: unavailable")
	}
	primaryRewardSubject := rewardSubjects[0]
	primaryRewardSubject.Alternates = rewardSubjects[1:]
	collectResult, err := zoneresult.CollectCashOut(
		ctx,
		peerSession.chainResult,
		resultsporenet.PartGenerator{GameplayJoin: r.gameplayJoin},
		resultsporenet.RewardStore{Progression: r.cashOutProgression},
		zoneresult.CollectCommand{
			UserID:  int64(snapshot.UserID),
			Subject: primaryRewardSubject,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("campaignCashOutCollect: %w", err)
	}
	cashOutPacket, err := resultraknet.CashOutReceipt(
		resultraknet.CashOutReceiptRequest{
			Receipt:            collectResult.Receipt,
			CompletedIndex:     snapshot.CompletedIndex,
			PlanetsCompleted:   snapshot.PlanetsCompleted,
			MedalCounts:        snapshot.MedalCounts,
			StartingExperience: snapshot.StartingExperience,
			FinalExperience:    snapshot.FinalExperience,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("campaignCashOutMarshal: %w", err)
	}
	if !r.recordCashOutProgression(
		packet.Address.String(),
		peerSession.generation,
		peerSession.chainResult,
		collectResult.Receipt.ChainProgression,
	) {
		return nil, nil
	}
	if collectResult.IsReplay {
		r.logger.Printf(
			"RakNet campaign cashout presentation replayed for %s result=%d rewards=%d",
			packet.Address, snapshot.ResultID, len(collectResult.Receipt.Parts),
		)
		return [][]byte{cashOutPacket}, nil
	}
	r.logger.Printf(
		"RakNet campaign cashout presentation sent to %s result=%d chain_progression=%d rewards=%d reward_level=%d",
		packet.Address, snapshot.ResultID, collectResult.Receipt.ChainProgression,
		len(collectResult.Receipt.Parts), collectResult.RewardLevel,
	)
	return [][]byte{cashOutPacket}, nil
}

func (r campaignResultRuntime) recordCashOutProgression(
	sessionKey string,
	generation uint64,
	resultSession *zoneresult.Session,
	chainProgression uint32,
) bool {
	if resultSession == nil {
		return false
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isAccepted := isFound && peerSession.generation == generation &&
		peerSession.chainResult == resultSession
	if !isAccepted {
		return false
	}
	peerSession.binding.ChainProgression = max(
		peerSession.binding.ChainProgression, chainProgression,
	)
	r.registry.sessions[sessionKey] = peerSession
	return true
}

func (r campaignResultRuntime) handlePreparation(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
) ([][]byte, error) {
	chainCommand, commandErr := raknet.DecodeChainPlayerCommand(packet.Payload)
	if commandErr != nil {
		return nil, nil
	}
	if chainCommand.Type == raknet.ChainPlayerSelectContinue {
		if chainCommand.Choice != 1 || chainCommand.SelectedRecordID == 0 {
			return nil, nil
		}
		selectedBinding, selectionErr := r.gameplayJoin.SelectCampaignSquad(
			peerSession.binding, chainCommand.SelectedRecordID,
		)
		if selectionErr != nil {
			r.logger.Printf(
				"RakNet chain player squad rejected from %s squad=%d error=%v",
				packet.Address, chainCommand.SelectedRecordID, selectionErr,
			)
			return nil, nil
		}
		r.registry.mutex.Lock()
		currentSession, isFound := r.registry.sessions[packet.Address.String()]
		isCurrent := isFound && currentSession.generation == peerSession.generation
		if isCurrent {
			currentSession.binding = selectedBinding
			r.registry.sessions[packet.Address.String()] = currentSession
		}
		r.registry.mutex.Unlock()
		if !isCurrent {
			return nil, nil
		}
		packets, isPrepared, err := r.preparation.prepare(
			packet.Address.String(), peerSession.generation, false,
		)
		if err != nil {
			return nil, fmt.Errorf("chainPrepare: %w", err)
		}
		if !isPrepared {
			return nil, nil
		}
		r.logger.Printf(
			"RakNet chain player start accepted from %s squad=%d",
			packet.Address, selectedBinding.SquadID,
		)
		return packets, nil
	}
	if chainCommand.Type != raknet.ChainPlayerRequestVoteData {
		return nil, nil
	}
	previewDirector, err := r.preparation.loadPreview(ctx, peerSession.binding)
	if err != nil {
		return nil, fmt.Errorf("chainPreview: %w", err)
	}
	countdownPacket, err := resultraknet.Countdown(
		float32(zoneresult.VoteDuration.Seconds()),
	)
	if err != nil {
		return nil, fmt.Errorf("chainCountdown: %w", err)
	}
	type countdownMember struct {
		sessionKey string
		generation uint64
		binding    game.GameplayBinding
		votePacket []byte
	}
	r.registry.mutex.RLock()
	countdownMembers := make([]countdownMember, 0)
	for sessionKey, candidate := range r.registry.sessions {
		if candidate.binding.GameID != peerSession.binding.GameID {
			continue
		}
		countdownMembers = append(countdownMembers, countdownMember{
			sessionKey: sessionKey,
			generation: candidate.generation,
			binding:    candidate.binding,
		})
	}
	r.registry.mutex.RUnlock()
	for index := range countdownMembers {
		votePacket, marshalErr := marshalZoneChainVote(
			countdownMembers[index].binding, previewDirector,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("chainVote[%d]: %w", index, marshalErr)
		}
		countdownMembers[index].votePacket = votePacket
	}
	requesterKey := packet.Address.String()
	packets := make([][]byte, 0, 2)
	isCountdownNeeded := false
	countdownRecipients := 0
	r.registry.mutex.Lock()
	for _, member := range countdownMembers {
		currentSession, isFound := r.registry.sessions[member.sessionKey]
		isCurrent := isFound && currentSession.generation == member.generation &&
			currentSession.binding.GameID == peerSession.binding.GameID
		if !isCurrent {
			continue
		}
		isMemberCountdownNeeded := currentSession.stage.MarkChainCountdownSent()
		if member.sessionKey == requesterKey {
			packets = append(packets, member.votePacket)
			isCountdownNeeded = isMemberCountdownNeeded
			if isMemberCountdownNeeded {
				packets = append(packets, countdownPacket)
			}
		} else if isMemberCountdownNeeded {
			currentSession.queuePackets([][]byte{member.votePacket, countdownPacket})
		}
		if isMemberCountdownNeeded {
			countdownRecipients++
			r.registry.sessions[member.sessionKey] = currentSession
		}
	}
	r.registry.mutex.Unlock()
	if !isCountdownNeeded {
		return packets, nil
	}
	if packet.ScheduleFunc != nil {
		step := campaignPrepareFallbackStep{
			preparation: r.preparation, logger: r.logger,
			sessionKey: packet.Address.String(),
			generation: peerSession.generation,
		}
		err = packet.ScheduleFunc(campaignPrepareFallbackDelay, step.produce)
		if err != nil {
			r.logger.Printf(
				"RakNet chain countdown fallback schedule failed for %s: %v",
				packet.Address, err,
			)
		}
	}
	r.logger.Printf(
		"RakNet chain queue opened game=%d requester=%s recipients=%d countdown=2m30s",
		peerSession.binding.GameID, packet.Address, countdownRecipients,
	)
	return packets, nil
}
