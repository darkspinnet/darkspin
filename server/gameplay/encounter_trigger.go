package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
	unlockraknet "github.com/darkspinnet/darkspin/server/zone/unlock/raknet103"
)

// Shared by movement and stationary polling; callers hold the session registry.
func (e campaignEncounterRuntime) acceptTriggerPublicationsLocked(
	packet raknet.Packet, peerSession *gameplayPeerSession, result campaignEncounterAdvance,
) (campaignEncounterAdvance, error) {
	for _, publication := range result.publications {
		if publication.IsDwellComplete && zoneboss.IsNamedCallback(publication.CallbackName) {
			if publication.CallbackName == zoneboss.GenericCallback {
				// The named-boss operation owns its transactional admission.
				continue
			}
			if peerSession.zone.Boss().IsDormant() {
				plan, err := peerSession.zone.PlanBossFromTrigger(publication,
					peerSession.binding.GameID, peerSession.binding.ChainLevelIndex)
				if err != nil {
					return result, fmt.Errorf("tutorialTriggerPlan: %w", err)
				}
				plan.Publication.CallbackName = publication.CallbackName
				err = e.startTutorialActivationLocked(peerSession, plan, false)
				if err != nil {
					return result, fmt.Errorf("tutorialTriggerStart: %w", err)
				}
			}
			err := peerSession.zone.AcceptPublication(publication)
			if err != nil {
				return result, fmt.Errorf("tutorialTriggerAccept: %w", err)
			}
			continue
		}
		if zonehorde.IsTrigger(publication) &&
			peerSession.zone.Horde().IsComplete(publication.MarkerSetName) {
			acceptErr := peerSession.zone.AcceptPublication(publication)
			if acceptErr != nil {
				return result, fmt.Errorf(
					"moveCampaignRestoredHordeAccept: %w", acceptErr,
				)
			}
			continue
		}
		if zonecallback.IsClientOnly(publication.CallbackName) &&
			publication.EventName == "" {
			if publication.CallbackName == zonecallback.OverdriveClient {
				peerSession.isClientBossBoundaryPending = true
			}
			acceptErr := peerSession.zone.AcceptPublication(publication)
			if acceptErr != nil {
				return result, fmt.Errorf(
					"moveCampaignClientCallbackAccept: %w", acceptErr,
				)
			}
			continue
		}
		encounterPlan, planErr := peerSession.zone.PlanInitialEncounter(
			publication, peerSession.binding.GameID,
			peerSession.binding.ChainLevelIndex,
		)
		if planErr != nil {
			return result, fmt.Errorf("moveCampaignEncounterPlan: %w", planErr)
		}
		abilityCount, isAbilityCountFound := peerSession.zone.AbilityCount(
			zoneResultMember(*peerSession),
		)
		if zoneunlock.IsRandomPublication(publication) &&
			isAbilityCountFound &&
			abilityCount == zoneunlock.InitialAbilityBoundary {
			unlockErr := peerSession.zone.AcceptPublication(publication)
			if unlockErr != nil {
				return result, fmt.Errorf("moveCampaignRandomUnlockAccept: %w", unlockErr)
			}
			result.randomUnlockPublication = publication
			continue
		}
		if zoneunlock.IsSupportPublication(publication) {
			unlockErr := peerSession.zone.CanAcceptPublication(publication)
			if unlockErr != nil {
				return result, fmt.Errorf("moveCampaignSupportUnlockCheck: %w", unlockErr)
			}
			abilityCount, isAbilityCountFound := peerSession.zone.AbilityCount(
				zoneResultMember(*peerSession),
			)
			isUnlockNeeded := abilityCount ==
				zoneunlock.FullAbilityBoundary &&
				isAbilityCountFound &&
				peerSession.binding.ChainProgression <
					zoneunlock.SecondChainLevelIndex
			if isUnlockNeeded && len(encounterPlan.Boss) == 0 {
				if peerSession.campaignUnlockPresentationSession().Support() != nil {
					return result, errors.New("moveCampaignSupportUnlock: already active")
				}
				result.supportUnlockRun, unlockErr = unlockraknet.NewSupportRun(
					e.supportUnlock, uint8(peerSession.binding.Slot),
					abilityCount,
				)
				if unlockErr != nil {
					return result, fmt.Errorf("moveCampaignSupportUnlockRun: %w", unlockErr)
				}
			}
			// The initial 1-1 final-arena trigger owns both the support
			// presentation and Illust's encounter. Let ArmBossEncounter accept
			// that shared publication so the support route cannot consume it
			// before boss planning.
			if len(encounterPlan.Boss) == 0 {
				unlockErr = peerSession.zone.AcceptPublication(publication)
				if unlockErr != nil {
					if result.supportUnlockRun != nil {
						result.supportUnlockRun.Stop()
					}
					return result, fmt.Errorf("moveCampaignSupportUnlockAccept: %w", unlockErr)
				}
			}
			if result.supportUnlockRun != nil && len(encounterPlan.Boss) == 0 {
				unlockErr = peerSession.campaignUnlockPresentationSession().
					InstallSupport(result.supportUnlockRun)
				if unlockErr != nil {
					result.supportUnlockRun.Stop()
					return result, fmt.Errorf(
						"moveCampaignSupportUnlockInstall: %w", unlockErr,
					)
				}
				result.supportUnlockPublication = publication
			}
			if len(encounterPlan.Boss) == 0 {
				continue
			}
		}
		if encounterPlan.IsBossDeferred {
			if isAbilityCountFound &&
				abilityCount == zoneunlock.RandomAbilityBoundary {
				unlockPacket, marshalErr := unlockraknet.AbilityCount(
					peerSession.binding.Slot,
					zoneunlock.FullAbilityBoundary,
				)
				if marshalErr != nil {
					return result, fmt.Errorf(
						"moveCampaignBossAbilityMarshal: %w", marshalErr,
					)
				}
				isRaised := peerSession.zone.RaiseAbilityCount(
					zoneResultMember(*peerSession),
					zoneunlock.FullAbilityBoundary,
				)
				if isRaised {
					result.unlockPackets = append(
						result.unlockPackets, unlockPacket,
					)
					e.logger.Printf(
						"RakNet campaign squad ability unlocked at deferred boss arena for user=%d",
						peerSession.binding.UserID,
					)
				}
			}
			if result.supportUnlockRun != nil {
				result.supportUnlockRun.Stop()
				result.supportUnlockRun = nil
			}
			continue
		}
		plannedHorde := encounterPlan.Horde
		if len(plannedHorde) == 0 {
			plannedBoss := encounterPlan.Boss
			if len(plannedBoss) == 0 {
				continue
			}
			plannedBossPackets, marshalErr := npcraknet.TargetedSpawns(
				plannedBoss[1:], peerSession.deployedObjectID,
			)
			if marshalErr != nil {
				return result, fmt.Errorf("moveCampaignBossMarshal: %w", marshalErr)
			}
			activePacket, marshalErr := bossraknet.AddPhase()
			if marshalErr != nil {
				return result, fmt.Errorf("moveCampaignBossStateMarshal: %w", marshalErr)
			}
			bossErr := peerSession.zone.ArmBossEncounter(
				publication, peerSession.deployedObjectID, plannedBoss,
			)
			if bossErr != nil {
				if result.supportUnlockRun != nil {
					result.supportUnlockRun.Stop()
					result.supportUnlockRun = nil
				}
				if errors.Is(bossErr, zoneboss.ErrHordeActive) {
					continue
				}
				return result, fmt.Errorf("moveCampaignBossArm: %w", bossErr)
			}
			if result.supportUnlockRun != nil {
				installErr := peerSession.campaignUnlockPresentationSession().
					InstallSupport(result.supportUnlockRun)
				if installErr != nil {
					result.supportUnlockRun.Stop()
					return result, fmt.Errorf(
						"moveCampaignBossSupportInstall: %w", installErr,
					)
				}
				result.supportUnlockPublication = publication
			}
			result.bossPlans = plannedBoss
			result.bossPackets = append(plannedBossPackets, activePacket)
			result.bossPublication = publication
			if e.logger != nil {
				e.logger.Printf(
					"Campaign boss admission stage=planned level=%q marker_set=%q trigger=%d leader=%q actors=%d source=initial",
					peerSession.binding.Level, publication.MarkerSetName,
					publication.TriggerMarkerID, plannedBoss[0].NounName,
					len(plannedBoss),
				)
			}
			continue
		}
		if zoneunlock.IsTutorialActivationCallback(publication.CallbackName) {
			activationErr := e.startTutorialActivationLocked(peerSession,
				zone.NamedBossPlan{Publication: publication, Actors: plannedHorde}, true)
			if activationErr != nil {
				return result, fmt.Errorf("hordeActivation: %w", activationErr)
			}
			continue
		}
		plannedPackets, marshalErr := npcraknet.TargetedSpawns(
			plannedHorde, peerSession.deployedObjectID,
		)
		if marshalErr != nil {
			return result, fmt.Errorf("moveCampaignHordeMarshal: %w", marshalErr)
		}
		barrierPlans := peerSession.zone.HordeBarrierPlans(publication.MarkerSetName)
		barrierPackets := make([][]byte, 0)
		if len(barrierPlans) != 0 {
			barrierPackets, marshalErr = barrierraknet.Create(barrierPlans)
			if marshalErr != nil {
				return result, fmt.Errorf("moveCampaignHordeBarrierMarshal: %w", marshalErr)
			}
		}
		plannedPackets = append(barrierPackets, plannedPackets...)
		hordeStatePacket, stateErr := raknet.MarshalApplication(raknet.DirectorStateMessage{
			IsHordeSpawned: true, IsHordeSpawnedPresent: true,
		})
		if stateErr != nil {
			return result, fmt.Errorf("hordeState: %w", stateErr)
		}
		plannedPackets = append(plannedPackets, hordeStatePacket)
		hordeErr := peerSession.zone.AdmitFirstHorde(
			publication, peerSession.deployedObjectID, plannedHorde,
		)
		if hordeErr != nil {
			if errors.Is(hordeErr, zone.ErrHordeDeferred) {
				continue
			}
			return result, fmt.Errorf("moveCampaignHordeAdmit: %w", hordeErr)
		}
		result.hordePlans = append(result.hordePlans, plannedHorde...)
		result.hordePackets = append(result.hordePackets, plannedPackets...)
	}

	return result, nil
}
