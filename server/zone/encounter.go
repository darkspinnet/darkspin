package zone

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

var ErrHordeDeferred = errors.New("horde publication deferred")

type InitialEncounterPlan struct {
	Horde          []zonenpc.SpawnPlan
	Boss           []zonenpc.SpawnPlan
	IsBossDeferred bool
}

type NamedBossPlan struct {
	NamedPublication game.CampaignDirectorNamedEventPublication
	Publication      game.CampaignDirectorPublication
	Actors           []zonenpc.SpawnPlan
}

type DeveloperBossPlan struct {
	Publication        game.CampaignDirectorPublication
	Actors             []zonenpc.SpawnPlan
	IsPositionAuthored bool
}

func (e *Zone) AssignSpawnPlanIDs(
	plans []zonenpc.SpawnPlan,
) ([]zonenpc.SpawnPlan, error) {
	if len(plans) == 0 {
		return plans, nil
	}
	if e == nil {
		return nil, errors.New("spawn plan zone unavailable")
	}
	e.mu.RLock()
	state := e.state
	objectID := e.info.ObjectID
	e.mu.RUnlock()
	if state != StateActive || objectID == nil {
		return nil, errors.New("spawn plan allocator unavailable")
	}
	firstObjectID, err := objectID.Reserve(uint32(len(plans)))
	if err != nil {
		return nil, fmt.Errorf("spawnPlanReserve: %w", err)
	}
	assigned := append([]zonenpc.SpawnPlan(nil), plans...)
	for index := range assigned {
		assigned[index].ObjectID = firstObjectID + uint32(index)
	}
	return assigned, nil
}

func (e *Zone) PlanInitialEncounter(
	publication game.CampaignDirectorPublication,
	gameID uint32, chainLevelIndex uint32,
) (InitialEncounterPlan, error) {
	if e == nil {
		return InitialEncounterPlan{}, errors.New("encounter planning zone unavailable")
	}
	e.mu.RLock()
	directorDefinition := e.info.DirectorDefinition
	objectID := e.info.ObjectID
	horde := e.info.Horde
	e.mu.RUnlock()
	if objectID == nil {
		return InitialEncounterPlan{}, errors.New("encounter planning allocator unavailable")
	}
	hordePlans, _, err := zonehorde.PlanFirstWave(
		directorDefinition, publication, objectID.Next(), gameID,
	)
	if err != nil {
		return InitialEncounterPlan{}, fmt.Errorf("initialHordePlan: %w", err)
	}
	hordePlans, err = e.AssignSpawnPlanIDs(hordePlans)
	if err != nil {
		return InitialEncounterPlan{}, fmt.Errorf("initialHordeID: %w", err)
	}
	if len(hordePlans) != 0 {
		return InitialEncounterPlan{Horde: hordePlans}, nil
	}
	bossPlans, _, err := zoneboss.PlanInitialEncounter(
		directorDefinition, publication, objectID.Next(), gameID, chainLevelIndex,
	)
	if err != nil {
		return InitialEncounterPlan{}, fmt.Errorf("initialBossPlan: %w", err)
	}
	if len(bossPlans) != 0 {
		if horde == nil {
			return InitialEncounterPlan{}, errors.New("initial boss order unavailable")
		}
		err = zoneboss.ValidateInitialOrder(horde)
		if errors.Is(err, zoneboss.ErrSecondHordeIncomplete) {
			return InitialEncounterPlan{
				Boss: bossPlans, IsBossDeferred: true,
			}, nil
		}
		if err != nil {
			return InitialEncounterPlan{}, fmt.Errorf("initialBossOrder: %w", err)
		}
	}
	bossPlans, err = e.AssignSpawnPlanIDs(bossPlans)
	if err != nil {
		return InitialEncounterPlan{}, fmt.Errorf("initialBossID: %w", err)
	}
	return InitialEncounterPlan{Boss: bossPlans}, nil
}

func (e *Zone) PlanHordeFollowup(
	transition zonehorde.Transition,
	targetObjectID uint32,
	gameID uint32,
) ([]zonenpc.SpawnPlan, error) {
	if e == nil || targetObjectID == 0 || transition.NextWaveActorCount == 0 {
		return nil, errors.New("horde followup planning invalid")
	}
	e.mu.RLock()
	directorDefinition := e.info.DirectorDefinition
	objectID := e.info.ObjectID
	horde := e.info.Horde
	npc := e.info.NPCs
	e.mu.RUnlock()
	if objectID == nil || horde == nil || npc == nil {
		return nil, errors.New("horde followup authority unavailable")
	}
	publication, isFound := horde.Publication(transition.MarkerSetName)
	if !isFound {
		return nil, errors.New("horde followup publication missing")
	}
	plans, _, err := zonehorde.PlanWave(
		directorDefinition, publication, objectID.Next(), gameID,
		transition.NextWaveActorCount, 2,
	)
	if err != nil {
		return nil, fmt.Errorf("hordeFollowupPlan: %w", err)
	}
	plans, err = e.AssignSpawnPlanIDs(plans)
	if err != nil {
		return nil, fmt.Errorf("hordeFollowupID: %w", err)
	}
	err = npc.CanAdd(plans, targetObjectID)
	if err != nil {
		return nil, fmt.Errorf("hordeFollowupNPC: %w", err)
	}
	err = horde.CanAdmitNextWave(transition.MarkerSetName, plans)
	if err != nil {
		return nil, fmt.Errorf("hordeFollowupState: %w", err)
	}
	return plans, nil
}

func (e *Zone) PlanBossFollowup(
	targetObjectID uint32,
	gameID uint32,
	actorCount int,
) ([]zonenpc.SpawnPlan, error) {
	if e == nil || targetObjectID == 0 || actorCount <= 0 {
		return nil, errors.New("boss followup planning invalid")
	}
	e.mu.RLock()
	directorDefinition := e.info.DirectorDefinition
	objectID := e.info.ObjectID
	boss := e.info.Boss
	npc := e.info.NPCs
	e.mu.RUnlock()
	if objectID == nil || boss == nil || npc == nil {
		return nil, errors.New("boss followup authority unavailable")
	}
	leaderPlan, isLeaderFound := boss.LeaderPlan()
	if !isLeaderFound {
		return nil, errors.New("boss followup leader missing")
	}
	if actorCount == 1 {
		plans := []zonenpc.SpawnPlan{leaderPlan}
		err := npc.CanAdd(plans, targetObjectID)
		if err != nil {
			return nil, fmt.Errorf("bossFollowupNPC: %w", err)
		}
		err = boss.CanAdmitSecondWave(plans)
		if err != nil {
			return nil, fmt.Errorf("bossFollowupState: %w", err)
		}
		return plans, nil
	}
	if actorCount != 3 {
		return nil, errors.New("boss followup actor count unsupported")
	}
	publication, isFound := boss.Publication()
	if !isFound {
		return nil, errors.New("boss followup publication missing")
	}
	addPlans, _, err := zoneboss.PlanInitialSecondWave(
		directorDefinition, publication, objectID.Next(), gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("bossFollowupPlan: %w", err)
	}
	addPlans, err = e.AssignSpawnPlanIDs(addPlans)
	if err != nil {
		return nil, fmt.Errorf("bossFollowupID: %w", err)
	}
	plans := append([]zonenpc.SpawnPlan{leaderPlan}, addPlans...)
	err = npc.CanAdd(plans, targetObjectID)
	if err != nil {
		return nil, fmt.Errorf("bossFollowupNPC: %w", err)
	}
	err = boss.CanAdmitSecondWave(plans)
	if err != nil {
		return nil, fmt.Errorf("bossFollowupState: %w", err)
	}
	return plans, nil
}

func (e *Zone) PlanDeveloperBoss(
	level string,
	gameID uint32,
	chainLevelIndex uint32,
) (DeveloperBossPlan, error) {
	if e == nil {
		return DeveloperBossPlan{}, errors.New("developer boss zone unavailable")
	}
	e.mu.RLock()
	directorDefinition := e.info.DirectorDefinition
	objectID := e.info.ObjectID
	e.mu.RUnlock()
	if objectID == nil {
		return DeveloperBossPlan{}, errors.New("developer boss allocator unavailable")
	}
	publication := game.CampaignDirectorPublication{}
	plans := make([]zonenpc.SpawnPlan, 0)
	isPositionAuthored := false
	var err error
	if strings.EqualFold(level, game.InitialChainLevel) ||
		strings.EqualFold(directorDefinition.Level, game.InitialChainLevel) {
		publication, err = zoneboss.InitialDeveloperPublication(
			directorDefinition,
		)
		if err == nil {
			plans, _, err = zoneboss.PlanInitialEncounter(
				directorDefinition, publication, objectID.Next(), gameID,
				chainLevelIndex,
			)
		}
	} else {
		namedPublication, publicationErr := zoneboss.DeveloperPublication(
			directorDefinition,
		)
		if publicationErr != nil {
			return DeveloperBossPlan{},
				fmt.Errorf("developerBossPublication: %w", publicationErr)
		}
		publication, plans, _, err = zoneboss.PlanNamedEncounter(
			directorDefinition, namedPublication, objectID.Next(),
			gameID, chainLevelIndex,
		)
		isPositionAuthored = true
	}
	if err != nil {
		return DeveloperBossPlan{}, fmt.Errorf("developerBossPlan: %w", err)
	}
	plans, err = e.AssignSpawnPlanIDs(plans)
	if err != nil {
		return DeveloperBossPlan{}, fmt.Errorf("developerBossID: %w", err)
	}
	if len(plans) == 0 {
		return DeveloperBossPlan{}, errors.New("developer boss plan empty")
	}
	return DeveloperBossPlan{
		Publication:        publication,
		Actors:             plans,
		IsPositionAuthored: isPositionAuthored,
	}, nil
}

func (e *Zone) AdmitDeveloperBoss(
	publication game.CampaignDirectorPublication,
	targetObjectID uint32,
	plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("developer boss zone unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || targetObjectID == 0 || len(plans) == 0 {
		return errors.New("developer boss admission invalid")
	}
	if e.info.NPCs == nil || e.info.Boss == nil {
		return errors.New("developer boss authority unavailable")
	}
	err := e.info.Boss.CanArm(publication, plans)
	if err == nil {
		err = e.info.NPCs.CanAdd(plans, targetObjectID)
	}
	if err != nil {
		return fmt.Errorf("developerBossCheck: %w", err)
	}
	err = e.info.NPCs.Add(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("developerBossNPCAdd: %w", err)
	}
	err = e.info.Boss.Arm(publication, plans)
	if err == nil {
		err = e.info.Boss.Admit(plans)
	}
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(plans)
		return fmt.Errorf(
			"developerBossCommit: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}

// ArmBossEncounter commits an authored director publication and reserves its
// boss plans. The adds remain dormant until AdmitBoss publishes the delayed
// opening phase.
func (e *Zone) ArmBossEncounter(
	publication game.CampaignDirectorPublication,
	targetObjectID uint32,
	plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("boss encounter zone unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || targetObjectID == 0 || len(plans) < 2 {
		return errors.New("boss encounter admission invalid")
	}
	err := zoneboss.ValidateInitialOrder(e.info.Horde)
	if err != nil {
		return fmt.Errorf("bossOrder: %w", err)
	}
	if e.info.Director == nil || e.info.Boss == nil || e.info.NPCs == nil {
		return errors.New("boss encounter authority unavailable")
	}
	err = e.info.Director.CanAccept(publication)
	if err != nil {
		return fmt.Errorf("bossPublicationCheck: %w", err)
	}
	err = e.info.Boss.CanArm(publication, plans)
	if err != nil {
		return fmt.Errorf("bossStateCheck: %w", err)
	}
	err = e.info.NPCs.CanAdd(plans[1:], targetObjectID)
	if err != nil {
		return fmt.Errorf("bossNPCCheck: %w", err)
	}
	err = e.info.Director.Accept(publication)
	if err != nil {
		return fmt.Errorf("bossPublicationAccept: %w", err)
	}
	err = e.info.Boss.Arm(publication, plans)
	if err != nil {
		return fmt.Errorf("bossStateArm: %w", err)
	}
	return nil
}

// AdmitFirstHorde commits the director publication, live NPCs, and horde
// state under one zone lock. Later authored triggers remain pending while an
// earlier horde owns the route.
func (e *Zone) AdmitFirstHorde(
	publication game.CampaignDirectorPublication,
	targetObjectID uint32,
	plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("horde encounter zone unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || targetObjectID == 0 || len(plans) == 0 {
		return errors.New("horde encounter admission invalid")
	}
	if e.info.Director == nil || e.info.Horde == nil || e.info.NPCs == nil {
		return errors.New("horde encounter authority unavailable")
	}
	err := zonehorde.ValidateInitialOrder(publication, e.info.Horde)
	if err != nil {
		if zonehorde.IsInitialDeferred(publication, e.info.Horde) {
			return ErrHordeDeferred
		}
		return fmt.Errorf("hordeOrder: %w", err)
	}
	err = e.info.Director.CanAccept(publication)
	if err != nil {
		return fmt.Errorf("hordePublicationCheck: %w", err)
	}
	err = e.info.NPCs.CanAdd(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("hordeNPCCheck: %w", err)
	}
	err = e.info.Horde.CanAdmitFirstWave(publication, plans)
	if err != nil {
		return fmt.Errorf("hordeStateCheck: %w", err)
	}
	err = e.info.NPCs.Add(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("hordeNPCAdd: %w", err)
	}
	err = e.info.Director.Accept(publication)
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(plans)
		return fmt.Errorf(
			"hordePublicationAccept: %w", errors.Join(err, rollbackErr),
		)
	}
	err = e.info.Horde.AdmitFirstWave(publication, plans)
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(plans)
		return fmt.Errorf(
			"hordeStateAdmit: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}

func (e *Zone) CanAdmitBossAfterHorde(
	completion zonehorde.Completion,
) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state == StateActive &&
		e.info.Director != nil &&
		e.info.NPCs != nil &&
		e.info.Boss != nil &&
		e.info.Boss.IsDormant() &&
		zoneboss.IsFinalAuthoredHordeCompletion(
			e.info.DirectorDefinition, completion,
		)
}

func (e *Zone) PlanBossAfterHorde(
	completion zonehorde.Completion,
	gameID uint32,
	chainLevelIndex uint32,
) (NamedBossPlan, bool, error) {
	if !e.CanAdmitBossAfterHorde(completion) {
		return NamedBossPlan{}, false, nil
	}
	e.mu.RLock()
	directorDefinition := e.info.DirectorDefinition
	objectID := e.info.ObjectID
	e.mu.RUnlock()
	if objectID == nil {
		return NamedBossPlan{}, false,
			errors.New("named boss allocator unavailable")
	}
	bossIdentity, err := zoneboss.DeveloperPublication(directorDefinition)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("namedBossIdentity: %w", err)
	}
	namedPublication, err := e.prepareNamedEvent(
		bossIdentity.MarkerSetOrdinal,
		completion.TriggerMarkerID,
		bossIdentity.EventName,
	)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("namedBossPrepare: %w", err)
	}
	publication, plans, _, err := zoneboss.PlanNamedEncounter(
		directorDefinition, namedPublication, objectID.Next(),
		gameID, chainLevelIndex,
	)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("namedBossPlan: %w", err)
	}
	plans, err = e.AssignSpawnPlanIDs(plans)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("namedBossID: %w", err)
	}
	return NamedBossPlan{
		NamedPublication: namedPublication,
		Publication:      publication,
		Actors:           plans,
	}, true, nil
}

// PlanBossNearPosition resolves a dormant server boss listener at its authored
// arena. Build 103 exposes client and server halves of the listener graph, but
// the client callback is transient and cannot itself admit the server-owned
// boss, so proximity remains the conservative recovery authority.
func (e *Zone) PlanBossNearPosition(
	position game.Vec3, gameID uint32, chainLevelIndex uint32,
) (NamedBossPlan, bool, error) {
	if e == nil || !isFiniteEncounterPosition(position) {
		return NamedBossPlan{}, false, nil
	}
	e.mu.RLock()
	isAvailable := e.state == StateActive && e.info.Director != nil &&
		e.info.NPCs != nil && e.info.Boss != nil && e.info.Boss.IsDormant() &&
		!strings.EqualFold(e.info.DirectorDefinition.Level, game.InitialChainLevel)
	directorDefinition := e.info.DirectorDefinition
	objectID := e.info.ObjectID
	e.mu.RUnlock()
	if !isAvailable || objectID == nil {
		return NamedBossPlan{}, false, nil
	}
	bossIdentity, err := zoneboss.DeveloperPublicationNearPosition(
		directorDefinition, position, zoneboss.NamedBossArenaRadius,
	)
	if err != nil {
		return NamedBossPlan{}, false, nil
	}
	anchor, isAnchorFound := namedBossAnchor(bossIdentity)
	if !isAnchorFound {
		return NamedBossPlan{}, false, nil
	}
	namedPublication, err := e.prepareNamedEvent(
		bossIdentity.MarkerSetOrdinal, anchor.MarkerID, bossIdentity.EventName,
	)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("nearBossPrepare: %w", err)
	}
	publication, plans, _, err := zoneboss.PlanNamedEncounter(
		directorDefinition, namedPublication, objectID.Next(), gameID,
		chainLevelIndex,
	)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("nearBossPlan: %w", err)
	}
	plans, err = e.AssignSpawnPlanIDs(plans)
	if err != nil {
		return NamedBossPlan{}, false, fmt.Errorf("nearBossID: %w", err)
	}
	return NamedBossPlan{
		NamedPublication: namedPublication,
		Publication:      publication,
		Actors:           plans,
	}, true, nil
}

// IsStandaloneBossEncounter reports whether the active chain occurrence uses
// a zone boss rather than a captain promoted into the boss encounter.
func (e *Zone) IsStandaloneBossEncounter(chainLevelIndex uint32) bool {
	if e == nil || chainLevelIndex == 0 {
		return false
	}
	e.mu.RLock()
	level := e.info.DirectorDefinition.Level
	e.mu.RUnlock()
	nounName, isFound := zoneboss.NamedBossNoun(chainLevelIndex, level)
	return isFound && zoneboss.IsFinalBossNoun(nounName)
}

func namedBossAnchor(
	publication game.CampaignDirectorNamedEventPublication,
) (game.CampaignDirectorListenerPublication, bool) {
	for _, listener := range publication.Listeners {
		if zoneboss.IsNamedCallback(listener.CallbackName) &&
			strings.EqualFold(listener.NounName, "SpawnPoint_DirectorBoss.Noun") {
			return listener, true
		}
	}
	return game.CampaignDirectorListenerPublication{}, false
}

func isFiniteEncounterPosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsInf(float64(position.Z), 0)
}

// AdmitNamedBossEncounter atomically validates the candidate boss state and
// commits its named director event and NPC population to this zone.
func (e *Zone) AdmitNamedBossEncounter(
	namedPublication game.CampaignDirectorNamedEventPublication,
	publication game.CampaignDirectorPublication,
	targetObjectID uint32,
	plans []zonenpc.SpawnPlan,
) error {
	if e == nil {
		return errors.New("named boss zone unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || targetObjectID == 0 || len(plans) == 0 {
		return errors.New("named boss admission invalid")
	}
	if e.info.Director == nil || e.info.NPCs == nil || e.info.Boss == nil {
		return errors.New("named boss authority unavailable")
	}
	candidateBoss := zoneboss.NewSession()
	err := candidateBoss.Arm(publication, plans)
	if err == nil {
		err = candidateBoss.Admit(plans)
	}
	livePlans := plans
	if err == nil && candidateBoss.IsLeaderDeferred() {
		livePlans = plans[1:]
	}
	if err == nil {
		err = e.info.NPCs.CanAdd(livePlans, targetObjectID)
	}
	if err == nil {
		err = e.info.Director.CanAcceptNamedEvent(namedPublication)
	}
	if err != nil {
		return fmt.Errorf("namedBossCheck: %w", err)
	}
	err = e.info.NPCs.Add(livePlans, targetObjectID)
	if err != nil {
		return fmt.Errorf("namedBossNPCAdd: %w", err)
	}
	playerCount := max(uint32(1), uint32(len(e.members)))
	err = e.info.NPCs.ConfigureShieldedAffix(
		livePlans, e.info.Difficulty, playerCount,
	)
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(livePlans)
		return fmt.Errorf(
			"namedBossShield: %w", errors.Join(err, rollbackErr),
		)
	}
	err = e.info.NPCs.ConfigureCarapaceAffix(livePlans, e.info.Difficulty)
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(livePlans)
		return fmt.Errorf(
			"namedBossCarapace: %w", errors.Join(err, rollbackErr),
		)
	}
	err = e.info.Director.AcceptNamedEvent(namedPublication)
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(livePlans)
		return fmt.Errorf(
			"namedBossPublication: %w", errors.Join(err, rollbackErr),
		)
	}
	err = e.info.Boss.Replace(candidateBoss)
	if err != nil {
		rollbackErr := e.info.NPCs.RollbackAdd(livePlans)
		return fmt.Errorf(
			"namedBossReplace: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}
