package encounter

import (
	"errors"
	"fmt"

	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type DamageTransition struct {
	Horde zonehorde.Transition
	Boss  zoneboss.Transition
}

func ObserveDamage(
	hordeSession *zonehorde.Session,
	bossSession *zoneboss.Session,
	result zonenpc.DamageResult,
) (DamageTransition, error) {
	if result.ObjectID == 0 {
		return DamageTransition{}, errors.New("damage observation invalid")
	}
	transition := DamageTransition{}
	if result.IsDefeated && hordeSession != nil {
		hordeTransition, err := hordeSession.Defeat(result)
		if err != nil {
			return DamageTransition{}, fmt.Errorf("damageHorde: %w", err)
		}
		transition.Horde = hordeTransition
	}
	if bossSession == nil {
		return transition, nil
	}
	bossTransition, err := bossSession.ObserveDamage(result)
	if err != nil {
		return DamageTransition{}, fmt.Errorf("damageBoss: %w", err)
	}
	transition.Boss = bossTransition
	return transition, nil
}

func AdmitHordeWave(
	npcSession *zonenpc.Session,
	hordeSession *zonehorde.Session,
	targetObjectID uint32, markerSetName string,
	plans []zonenpc.SpawnPlan,
) error {
	if npcSession == nil || hordeSession == nil ||
		targetObjectID == 0 || markerSetName == "" || len(plans) == 0 {
		return errors.New("horde admission invalid")
	}
	err := npcSession.CanAdd(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("hordeNPCCheck: %w", err)
	}
	err = hordeSession.CanAdmitNextWave(markerSetName, plans)
	if err != nil {
		return fmt.Errorf("hordeStateCheck: %w", err)
	}
	err = npcSession.Add(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("hordeNPCAdd: %w", err)
	}
	err = hordeSession.AdmitNextWave(markerSetName, plans)
	if err != nil {
		rollbackErr := npcSession.RollbackAdd(plans)
		return fmt.Errorf(
			"hordeStateAdmit: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}

func AdmitBossSecondWave(
	npcSession *zonenpc.Session,
	bossSession *zoneboss.Session,
	targetObjectID uint32, plans []zonenpc.SpawnPlan,
	difficulty uint32, playerCount uint32,
) error {
	if npcSession == nil || bossSession == nil ||
		targetObjectID == 0 || len(plans) == 0 || difficulty == 0 ||
		playerCount == 0 {
		return errors.New("boss admission invalid")
	}
	err := npcSession.CanAdd(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("bossNPCCheck: %w", err)
	}
	err = bossSession.CanAdmitSecondWave(plans)
	if err != nil {
		return fmt.Errorf("bossStateCheck: %w", err)
	}
	err = npcSession.Add(plans, targetObjectID)
	if err != nil {
		return fmt.Errorf("bossNPCAdd: %w", err)
	}
	err = npcSession.ConfigureShieldedAffix(plans, difficulty, playerCount)
	if err != nil {
		rollbackErr := npcSession.RollbackAdd(plans)
		return fmt.Errorf(
			"bossShield: %w", errors.Join(err, rollbackErr),
		)
	}
	err = npcSession.ConfigureCarapaceAffix(plans, difficulty)
	if err != nil {
		rollbackErr := npcSession.RollbackAdd(plans)
		return fmt.Errorf(
			"bossCarapace: %w", errors.Join(err, rollbackErr),
		)
	}
	err = bossSession.AdmitSecondWave(plans)
	if err != nil {
		rollbackErr := npcSession.RollbackAdd(plans)
		return fmt.Errorf(
			"bossStateAdmit: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}

// AdmitBoss commits an armed boss encounter while retaining the leader as a
// dormant boss-session plan. Only the add plans enter the live NPC session.
func AdmitBoss(
	npcSession *zonenpc.Session,
	bossSession *zoneboss.Session,
	targetObjectID uint32,
	plans []zonenpc.SpawnPlan,
) error {
	if npcSession == nil || bossSession == nil ||
		targetObjectID == 0 || len(plans) < 2 {
		return errors.New("boss admission invalid")
	}
	addPlans := plans[1:]
	err := npcSession.CanAdd(addPlans, targetObjectID)
	if err != nil {
		return fmt.Errorf("bossNPCCheck: %w", err)
	}
	err = bossSession.CanAdmit(plans)
	if err != nil {
		return fmt.Errorf("bossStateCheck: %w", err)
	}
	err = npcSession.Add(addPlans, targetObjectID)
	if err != nil {
		return fmt.Errorf("bossNPCAdd: %w", err)
	}
	err = bossSession.Admit(plans)
	if err != nil {
		rollbackErr := npcSession.RollbackAdd(addPlans)
		return fmt.Errorf(
			"bossStateAdmit: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}
