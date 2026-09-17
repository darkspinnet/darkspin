package snapshot

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func writeAnalysisMarkdown(path string, report analysisReport) error {
	var document strings.Builder
	document.WriteString("# Sync Snapshot analysis: ")
	document.WriteString(markdownText(report.SnapshotID))
	document.WriteString("\n\n")
	document.WriteString("- Likely cause: `")
	document.WriteString(report.LikelyCause)
	document.WriteString("` (")
	document.WriteString(report.Confidence)
	document.WriteString(" confidence)\n")
	document.WriteString("- Trigger: ")
	document.WriteString(markdownText(report.Trigger))
	document.WriteString("\n- Context: ")
	document.WriteString(markdownText(report.Context))
	document.WriteString("\n- Summary: ")
	document.WriteString(markdownText(report.Summary))
	document.WriteString("\n\n## Capture metrics\n\n")
	document.WriteString("| Signal | Count |\n| --- | ---: |\n")
	metrics := []struct {
		name  string
		count any
	}{
		{name: "Inbound / outbound datagrams", count: fmt.Sprintf("%d / %d", report.Metrics.InboundDatagramCount, report.Metrics.OutboundDatagramCount)},
		{name: "NACKs / retransmits", count: fmt.Sprintf("%d / %d", report.Metrics.NACKDatagramCount, report.Metrics.RetransmitCount)},
		{name: "Sequence gaps / late datagrams", count: fmt.Sprintf("%d / %d", report.Metrics.DatagramGapCount, report.Metrics.DatagramOutOfOrderCount)},
		{name: "Application emits / client receives", count: fmt.Sprintf("%d / %d", report.Metrics.ApplicationEmitCount, report.Metrics.ClientApplicationCount)},
		{name: "Client boundary action/input fields", count: report.Metrics.ClientBoundaryStateCount},
		{name: "Byte-identical server/client matches", count: report.Metrics.ServerClientMatchCount},
		{name: "Unmatched mature server payloads", count: report.Metrics.ServerClientMissingCount},
		{name: "Pending server output batches / packets / exact payloads", count: fmt.Sprintf("%d / %d / %d", report.Metrics.PendingServerBatchCount, report.Metrics.PendingServerPacketCount, report.Metrics.PendingServerPayloadCount)},
		{name: "Pending server output bytes / critical packets", count: fmt.Sprintf("%d / %d", report.Metrics.PendingServerPacketByteCount, report.Metrics.PendingServerCriticalCount)},
		{name: "Sessions with pending output overflow", count: report.Metrics.PendingServerOverflowCount},
		{name: "Server / client boundary objects", count: fmt.Sprintf("%d / %d", report.Metrics.ServerObjectCount, report.Metrics.ClientObjectCount)},
		{name: "Client boundary / RakNet-mapped objects", count: fmt.Sprintf("%d / %d", report.Metrics.ClientObjectCount, report.Metrics.MappedClientObjectCount)},
		{name: "Compared / drifted objects", count: fmt.Sprintf("%d / %d", report.Metrics.ComparedObjectCount, report.Metrics.DriftedObjectCount)},
		{name: "Client locomotion stalls", count: report.Metrics.ClientLocomotionStallCount},
		{name: "Server / compared / drifted projectiles", count: fmt.Sprintf("%d / %d / %d", report.Metrics.ServerProjectileCount, report.Metrics.ComparedProjectileCount, report.Metrics.DriftedProjectileCount)},
		{name: "Server projectiles missing on client", count: report.Metrics.MissingClientProjectileCount},
		{name: "Ordering / lifecycle anomalies", count: fmt.Sprintf("%d / %d", report.Metrics.OrderRegressionCount, report.Metrics.LifecycleAnomalyCount)},
		{name: "Dropped server events / malformed client lines", count: fmt.Sprintf("%d / %d", report.Metrics.DroppedEventCount, report.Metrics.MalformedClientLineCount)},
		{name: "Client ring lines / bytes", count: fmt.Sprintf("%d / %d", report.Metrics.ClientRingLineCount, report.Metrics.ClientRingByteCount)},
		{name: "Client capacity-dropped lines / bytes", count: fmt.Sprintf("%d / %d", report.Metrics.ClientCapacityDroppedLineCount, report.Metrics.ClientCapacityDroppedByteCount)},
		{name: "Client requested window truncated", count: report.Metrics.IsClientRingTruncated},
	}
	for _, metric := range metrics {
		document.WriteString("| ")
		document.WriteString(metric.name)
		document.WriteString(" | ")
		document.WriteString(fmt.Sprint(metric.count))
		document.WriteString(" |\n")
	}
	document.WriteString("\n## Findings\n\n")
	if len(report.Findings) == 0 {
		document.WriteString("No automatic outlier crossed the current diagnostic thresholds.\n")
	}
	for _, finding := range report.Findings {
		document.WriteString("### [")
		document.WriteString(strings.ToUpper(finding.Severity))
		document.WriteString("] ")
		document.WriteString(markdownText(finding.Title))
		document.WriteString("\n\n")
		document.WriteString(markdownText(finding.Detail))
		document.WriteString("\n")
		for _, evidence := range finding.Evidence {
			document.WriteString("\n- `")
			document.WriteString(evidence.Source)
			if evidence.Line > 0 {
				document.WriteString(fmt.Sprintf(":%d", evidence.Line))
			}
			document.WriteString("` — ")
			document.WriteString(markdownText(evidence.Description))
			if evidence.PacketName != "" {
				document.WriteString(" (")
				document.WriteString(evidence.PacketName)
				document.WriteString(")")
			}
			document.WriteString("\n")
		}
		document.WriteString("\n")
	}
	if len(report.ClientBoundaryStates) > 0 {
		document.WriteString("## Client boundary action and input state\n\n")
		document.WriteString("| Field | Value | Frame | Thread | Client source |\n")
		document.WriteString("| --- | ---: | ---: | ---: | --- |\n")
		for _, state := range report.ClientBoundaryStates {
			document.WriteString(fmt.Sprintf(
				"| %s | %d | %d | %d | `client-memory.jsonl:%d` |\n",
				markdownCell(state.Kind), state.Value, state.FrameSequence,
				state.ThreadID, state.ClientLine,
			))
		}
		document.WriteString("\n")
	}
	if len(report.ServerSessions) > 0 {
		document.WriteString("## Authoritative session replay state\n\n")
		document.WriteString("| User / hero | Stage | Reported position | Motion segment | Attack / basic input | Status timers |\n")
		document.WriteString("| --- | --- | --- | --- | --- | --- |\n")
		for _, session := range report.ServerSessions {
			control := session.PlayerControl
			motion := fmt.Sprintf(
				"`%.3f, %.3f, %.3f` → `%.3f, %.3f, %.3f`; velocity `%.3f, %.3f, %.3f`; speed %.3f; segment %d ms; rev %d; moving %t",
				control.MotionPosition[0], control.MotionPosition[1], control.MotionPosition[2],
				control.MotionGoal[0], control.MotionGoal[1], control.MotionGoal[2],
				control.MotionVelocity[0], control.MotionVelocity[1], control.MotionVelocity[2],
				control.MotionSpeed, control.MotionSegmentAtMS, control.MotionRevision,
				control.IsMotionMoving,
			)
			attack := fmt.Sprintf(
				"target %d; pose %t/%d ms; release rev %d/%d ms; basic index %d rev %d held %t started %t",
				control.AttackTargetObjectID, control.IsAttackPoseActive,
				control.AttackPoseRemainingMS, control.AbilityReleaseRevision,
				control.AbilityReleaseRemainingMS, control.BasicSequence.Index,
				control.BasicSequence.Revision, control.BasicSequence.IsHeld,
				control.BasicSequence.IsStarted,
			)
			status := fmt.Sprintf(
				"silence %d; sleep %d; stun %d→%d; root %d→%d; fear %d→%d; overdrive %d; trapper stealth %t",
				control.EnemySilenceRemainingMS, control.EnemySleepRemainingMS,
				control.EnemyStunRemainingMS, control.EnemyStunTargetObjectID,
				control.EnemyRootRemainingMS, control.EnemyRootTargetObjectID,
				control.EnemyFearRemainingMS, control.EnemyFearTargetObjectID,
				control.OverdriveRemainingMS, control.IsTrapperStealthed,
			)
			document.WriteString(fmt.Sprintf(
				"| %d / %d | %s | `%.3f, %.3f, %.3f` | %s | %s | %s |\n",
				session.UserID, session.DeployedObjectID, markdownCell(session.Stage),
				control.ReportedPosition[0], control.ReportedPosition[1],
				control.ReportedPosition[2], markdownCell(motion),
				markdownCell(attack), markdownCell(status),
			))
		}
		document.WriteString("\n")
		document.WriteString("| User | Cooldowns | Named schedules | Death runs | Objectives | Timeline tasks | Pending output | Deterministic random streams |\n")
		document.WriteString("| ---: | --- | --- | ---: | ---: | --- | --- | --- |\n")
		for _, session := range report.ServerSessions {
			activeCooldownCount := 0
			for _, cooldown := range session.Cooldowns {
				if cooldown.RemainingMS > 0 {
					activeCooldownCount++
				}
			}
			pendingOutput := fmt.Sprintf(
				"%d packets; overflow %t", session.PendingPacketCount,
				session.IsPendingOverflow,
			)
			document.WriteString(fmt.Sprintf(
				"| %d | %d active / %d retained | %s | %d | %d | %s | %s | %s |\n",
				session.UserID, activeCooldownCount, len(session.Cooldowns),
				markdownCell(scheduleSummary(session.ActionSchedules)),
				len(session.Deaths), len(session.Objectives),
				markdownCell(boundedStrings(session.TimelineKeys, 12)),
				markdownCell(pendingOutput), markdownCell(randomSummaryText(session.Randoms)),
			))
		}
		document.WriteString("\n")
	}
	writeSquadReport(&document, report.ServerSessions)
	writeAbilityRuntimeReport(&document, report.ServerSessions)
	writeZoneRuntimeReport(&document, report.ServerSessions)
	writeCooldownReport(&document, report.ServerSessions)
	writeDeathReport(&document, report.ServerSessions)
	writeObjectiveReport(&document, report.ServerSessions)
	if len(report.ServerNPCStates) > 0 {
		document.WriteString("## Authoritative NPC boundary state\n\n")
		document.WriteString("| Object | Noun / action | Position | Goal | Target | Speed / stop | Status remaining | Life / action |\n")
		document.WriteString("| ---: | --- | --- | --- | ---: | --- | --- | --- |\n")
		stateCount := len(report.ServerNPCStates)
		if stateCount > 40 {
			stateCount = 40
		}
		for _, state := range report.ServerNPCStates[:stateCount] {
			status := fmt.Sprintf(
				"stun %d ms; sleep %d ms; root %d ms; silence %d ms; fear %d ms",
				state.StunRemainingMS, state.SleepRemainingMS,
				state.RootRemainingMS, state.SilenceRemainingMS,
				state.FearRemainingMS,
			)
			life := fmt.Sprintf(
				"HP %.2f; defeated %t; targetable %t; action %t rev %d; self-res %t; nav collision %t",
				state.HitPoint, state.IsDefeated, state.IsTargetable,
				state.IsActionStarted, state.ActionRevision,
				state.IsSelfResurrectionTriggered,
				state.IsNavigationCollisionEnabled,
			)
			document.WriteString(fmt.Sprintf(
				"| %d | %s | `%.3f, %.3f, %.3f` | `%.3f, %.3f, %.3f` | %d | %.3f / %.3f (slow %.3f) | %s | %s |\n",
				state.ObjectID, markdownCell(comparisonObjectLabel(objectComparison{
					NounName: state.NounName, AbilityName: state.AbilityName,
				})), state.Position[0], state.Position[1], state.Position[2],
				state.GoalPosition[0], state.GoalPosition[1], state.GoalPosition[2],
				state.TargetObjectID, state.Speed, state.StopDistance,
				state.SlowMovementScale, markdownCell(status), markdownCell(life),
			))
		}
		document.WriteString("\n")
	}
	if len(report.LocomotionStalls) > 0 {
		document.WriteString("## Client locomotion stalls\n\n")
		document.WriteString("| Network object | Client handle | Kind / noun | Server position | Client goal | Client object | Server-goal distance | Object-goal distance | Locomotion state | Client sources |\n")
		document.WriteString("| ---: | ---: | --- | --- | --- | --- | ---: | ---: | --- | --- |\n")
		comparisonCount := len(report.LocomotionStalls)
		if comparisonCount > 20 {
			comparisonCount = 20
		}
		for _, comparison := range report.LocomotionStalls[:comparisonCount] {
			if comparison.ClientGoal == nil || comparison.ServerGoalDistance == nil ||
				comparison.ClientGoalDistance == nil {
				continue
			}
			goal := *comparison.ClientGoal
			document.WriteString(fmt.Sprintf(
				"| %d | %d | %s | `%.3f, %.3f, %.3f` | `%.3f, %.3f, %.3f` | `%.3f, %.3f, %.3f` | %.3f | %.3f | %s | root `%d`, goal `%d` |\n",
				comparison.ObjectID, comparison.ClientObjectID,
				markdownCell(comparisonKindLabel(comparison)),
				comparison.ServerPosition[0], comparison.ServerPosition[1],
				comparison.ServerPosition[2], goal[0], goal[1], goal[2],
				comparison.ClientPosition[0], comparison.ClientPosition[1],
				comparison.ClientPosition[2], *comparison.ServerGoalDistance,
				*comparison.ClientGoalDistance,
				markdownCell(locomotionStateLabel(comparison)), comparison.ClientLine,
				comparison.ClientGoalLine,
			))
		}
		document.WriteString("\n")
	}
	if len(report.ProjectileComparisons) > 0 {
		document.WriteString("## Projectile position comparison\n\n")
		document.WriteString("| Network object | Client handle | Ability / noun | Server position | Client position | Distance | Allowance | Result | Client source |\n")
		document.WriteString("| ---: | ---: | --- | --- | --- | ---: | ---: | --- | --- |\n")
		comparisonCount := len(report.ProjectileComparisons)
		if comparisonCount > 20 {
			comparisonCount = 20
		}
		for _, comparison := range report.ProjectileComparisons[:comparisonCount] {
			result := "within allowance"
			if comparison.IsOutlier {
				result = "drifted"
			}
			document.WriteString(fmt.Sprintf(
				"| %d | %d | %s | `%.3f, %.3f, %.3f` | `%.3f, %.3f, %.3f` | %.3f | %.3f | %s | `client-memory.jsonl:%d` |\n",
				comparison.ObjectID, comparison.ClientObjectID,
				markdownCell(comparisonObjectLabel(comparison)),
				comparison.ServerPosition[0], comparison.ServerPosition[1],
				comparison.ServerPosition[2], comparison.ClientPosition[0],
				comparison.ClientPosition[1], comparison.ClientPosition[2],
				comparison.Distance, comparison.AllowedDistance, result,
				comparison.ClientLine,
			))
		}
		document.WriteString("\n")
	}
	if len(report.ObjectComparisons) > 0 {
		document.WriteString("## Boundary object comparison\n\n")
		document.WriteString("| Network object | Client handle | Kind | Distance | Allowance | Client source |\n| ---: | ---: | --- | ---: | ---: | --- |\n")
		comparisonCount := len(report.ObjectComparisons)
		if comparisonCount > 20 {
			comparisonCount = 20
		}
		for _, comparison := range report.ObjectComparisons[:comparisonCount] {
			document.WriteString(fmt.Sprintf(
				"| %d | %d | %s | %.3f | %.3f | `client-memory.jsonl:%d` |\n",
				comparison.ObjectID, comparison.ClientObjectID,
				markdownCell(comparison.Kind), comparison.Distance,
				comparison.AllowedDistance, comparison.ClientLine,
			))
		}
		document.WriteString("\n")
	}
	document.WriteString("## Next checks\n\n")
	for _, recommendation := range report.Recommendations {
		document.WriteString("- ")
		document.WriteString(markdownText(recommendation))
		document.WriteString("\n")
	}
	document.WriteString("\nThe automatic classification is evidence triage, not a gameplay correction. Raw bytes and state remain authoritative.\n")
	err := os.WriteFile(path, []byte(document.String()), 0o644)
	if err != nil {
		return fmt.Errorf("reportWrite: %w", err)
	}
	return nil
}

func writeSquadReport(document *strings.Builder, sessions []serverSessionState) {
	characterCount := 0
	occupiedCrystalCount := 0
	for _, session := range sessions {
		characterCount += len(session.Squad.Characters)
		for _, slot := range session.CrystalInventory.Slots {
			if slot.IsOccupied {
				occupiedCrystalCount++
			}
		}
	}
	if characterCount == 0 && occupiedCrystalCount == 0 {
		return
	}
	document.WriteString("## Squad resources and catalyst inventory\n\n")
	if characterCount != 0 {
		document.WriteString("| User | Character | Health | Mana | Available | Deployed | Squad gates |\n")
		document.WriteString("| ---: | ---: | --- | --- | --- | --- | --- |\n")
		for _, session := range sessions {
			gates := fmt.Sprintf(
				"deploy cooldown %d ms; game over %t; restart reserved %t",
				session.Squad.DeployCooldownRemainingMS,
				session.Squad.IsGameOver, session.Squad.IsRestartReserved,
			)
			for _, character := range session.Squad.Characters {
				document.WriteString(fmt.Sprintf(
					"| %d | %d | %.2f / %.2f | %.2f / %.2f | %t | %t | %s |\n",
					session.UserID, character.Index, character.HitPoint,
					character.MaximumHitPoint, character.ManaPoint,
					character.MaximumManaPoint, character.IsAvailable,
					character.IsDeployed, markdownCell(gates),
				))
			}
		}
		document.WriteString("\n")
	}
	if occupiedCrystalCount == 0 {
		return
	}
	document.WriteString("| User | Slot | Catalyst | Type / level / rarity | Diagonal links |\n")
	document.WriteString("| ---: | ---: | --- | --- | --- |\n")
	for _, session := range sessions {
		for _, slot := range session.CrystalInventory.Slots {
			if !slot.IsOccupied {
				continue
			}
			document.WriteString(fmt.Sprintf(
				"| %d | %d | %s (`%d`) | %d / %d / %d | %t |\n",
				session.UserID, slot.Index, markdownCell(slot.NounName),
				slot.NounAsset, slot.CrystalType, slot.CrystalLevel,
				slot.Rarity, session.CrystalInventory.IsDiagonalUnlocked,
			))
		}
	}
	document.WriteString("\n")
}

func writeAbilityRuntimeReport(document *strings.Builder, sessions []serverSessionState) {
	runtimeCount := 0
	for _, session := range sessions {
		runtimeCount += len(session.AbilityRuntimes)
	}
	if runtimeCount == 0 {
		return
	}
	document.WriteString("## Active ability-owned runtime state\n\n")
	document.WriteString("| User | Runtime | Name / ability | Object / owner | Remaining | Triggered |\n")
	document.WriteString("| ---: | --- | --- | --- | ---: | --- |\n")
	writtenCount := 0
	for _, session := range sessions {
		for _, runtime := range session.AbilityRuntimes {
			if writtenCount >= 60 {
				break
			}
			identity := runtime.Name
			if runtime.AbilityID != 0 {
				identity = fmt.Sprintf("%s / %d", identity, runtime.AbilityID)
			}
			document.WriteString(fmt.Sprintf(
				"| %d | %s | %s | %d / %d | %d ms | %t |\n",
				session.UserID, markdownCell(runtime.Kind), markdownCell(identity),
				runtime.ObjectID, runtime.OwnerObjectID, runtime.RemainingMS,
				runtime.IsTriggered,
			))
			writtenCount++
		}
		if writtenCount >= 60 {
			break
		}
	}
	if runtimeCount > writtenCount {
		document.WriteString(fmt.Sprintf(
			"\n%d additional active runtimes are available in `analysis.json`.\n",
			runtimeCount-writtenCount,
		))
	}
	document.WriteString("\n")
}

func writeZoneRuntimeReport(document *strings.Builder, sessions []serverSessionState) {
	if len(sessions) == 0 {
		return
	}
	document.WriteString("## Zone, encounter, and scheduled-world state\n\n")
	document.WriteString("| User | Zone / clock / completion | Members | Boss | Security route |\n")
	document.WriteString("| ---: | --- | --- | --- | --- |\n")
	for _, session := range sessions {
		boss := "unavailable"
		if session.Boss != nil {
			boss = fmt.Sprintf(
				"phase %d; leader %d HP %.2f; live %s; plans %d; second requested/admitted %t/%t; beam-out %t/%t",
				session.Boss.Phase, session.Boss.LeaderObjectID,
				session.Boss.LeaderHitPoint,
				uint32Summary(session.Boss.LiveObjectIDs), session.Boss.PlanCount,
				session.Boss.IsSecondWaveRequested,
				session.Boss.IsSecondWaveAdmitted,
				session.Boss.IsBeamOutReserved,
				session.Boss.IsBeamOutCommitted,
			)
		}
		security := "unavailable"
		if session.Security != nil {
			security = fmt.Sprintf(
				"route %d; objects %v; presented %v", session.Security.RouteIndex,
				session.Security.ObjectIDs, session.Security.Presented,
			)
		}
		document.WriteString(fmt.Sprintf(
			"| %d | %s difficulty %d chain %d; %d ms / `%d`; population %t; restored %t; objectives complete %t; cleared groups %s | %s | %s | %s |\n",
			session.UserID, markdownCell(session.ZoneLevel), session.ZoneDifficulty,
			session.ZoneChainLevelIndex, session.ZoneElapsedMS,
			session.ZoneCompletionID, session.IsPopulationPrimed,
			session.IsZoneRestored, session.IsObjectiveComplete,
			markdownCell(uint32Summary(session.ClearedSpawnGroupIDs)),
			markdownCell(zoneMemberSummary(session.ZoneMembers)),
			markdownCell(boss), markdownCell(security),
		))
	}
	document.WriteString("\n")
	writeDirectorPublicationReport(document, sessions)
	writeEncounterReport(document, sessions)
	writeTimelineTaskReport(document, sessions)
	writeInteractableReport(document, sessions)
}

func writeDirectorPublicationReport(
	document *strings.Builder, sessions []serverSessionState,
) {
	publicationCount := 0
	for _, session := range sessions {
		publicationCount += len(session.DirectorPublications)
	}
	if publicationCount == 0 {
		return
	}
	document.WriteString("| User | Director event | Marker set | Trigger / source | Callback | Listeners |\n")
	document.WriteString("| ---: | --- | --- | --- | --- | ---: |\n")
	writtenCount := 0
	for _, session := range sessions {
		for _, publication := range session.DirectorPublications {
			if writtenCount >= 80 {
				break
			}
			trigger := fmt.Sprintf(
				"ordinal %d marker %d source %d", publication.TriggerOrdinal,
				publication.TriggerMarkerID, publication.SourceObjectID,
			)
			document.WriteString(fmt.Sprintf(
				"| %d | %s / %s | %s (%d) | %s | %s | %d |\n",
				session.UserID, markdownCell(publication.Kind),
				markdownCell(publication.EventName),
				markdownCell(publication.MarkerSetName),
				publication.MarkerSetOrdinal, markdownCell(trigger),
				markdownCell(publication.CallbackName), publication.ListenerCount,
			))
			writtenCount++
		}
		if writtenCount >= 80 {
			break
		}
	}
	if publicationCount > writtenCount {
		document.WriteString(fmt.Sprintf(
			"\n%d additional director publications are available in `analysis.json`.\n",
			publicationCount-writtenCount,
		))
	}
	document.WriteString("\n")
}

func writeEncounterReport(document *strings.Builder, sessions []serverSessionState) {
	recordCount := 0
	for _, session := range sessions {
		recordCount += len(session.EncounterStages) + len(session.Hordes)
	}
	if recordCount == 0 {
		return
	}
	document.WriteString("| User | Kind / key | Phase / stage | Live objects | Publication / gate / completion |\n")
	document.WriteString("| ---: | --- | --- | --- | --- |\n")
	for _, session := range sessions {
		for _, encounter := range session.EncounterStages {
			document.WriteString(fmt.Sprintf(
				"| %d | encounter / %s | %d; terminal %v | — | published %t |\n",
				session.UserID, markdownCell(encounter.Key), encounter.Stage,
				encounter.TerminalStages, encounter.IsPublished,
			))
		}
		for _, horde := range session.Hordes {
			document.WriteString(fmt.Sprintf(
				"| %d | horde / %s | phase %d; wave %d | %s | gate %t; event %s |\n",
				session.UserID, markdownCell(horde.MarkerSetName), horde.Phase,
				horde.WaveOrdinal, markdownCell(uint32Summary(horde.LiveObjectIDs)),
				horde.IsGateActive, markdownCell(horde.CompletionEvent),
			))
		}
	}
	document.WriteString("\n")
}

func writeTimelineTaskReport(document *strings.Builder, sessions []serverSessionState) {
	taskCount := 0
	for _, session := range sessions {
		taskCount += len(session.TimelineTasks)
	}
	if taskCount == 0 {
		return
	}
	document.WriteString("| User | Timeline task | Due at | Remaining |\n")
	document.WriteString("| ---: | --- | --- | ---: |\n")
	for _, session := range sessions {
		for _, task := range session.TimelineTasks {
			document.WriteString(fmt.Sprintf(
				"| %d | %s | `%s` | %d ms |\n", session.UserID,
				markdownCell(task.Key), task.DueAt.UTC().Format(time.RFC3339Nano),
				task.RemainingMS,
			))
		}
	}
	document.WriteString("\n")
}

func writeInteractableReport(document *strings.Builder, sessions []serverSessionState) {
	interactableCount := 0
	for _, session := range sessions {
		interactableCount += len(session.Interactables)
	}
	if interactableCount == 0 {
		return
	}
	document.WriteString("| User | Interactable | Uses / limit | Admission |\n")
	document.WriteString("| ---: | ---: | --- | --- |\n")
	writtenCount := 0
	for _, session := range sessions {
		for _, interactable := range session.Interactables {
			if writtenCount >= 60 {
				break
			}
			admission := "available"
			if interactable.Limit >= 0 && interactable.Count >= interactable.Limit {
				admission = "exhausted"
			}
			document.WriteString(fmt.Sprintf(
				"| %d | %d | %d / %d | %s |\n", session.UserID,
				interactable.ObjectID, interactable.Count, interactable.Limit, admission,
			))
			writtenCount++
		}
		if writtenCount >= 60 {
			break
		}
	}
	if interactableCount > writtenCount {
		document.WriteString(fmt.Sprintf(
			"\n%d additional interactables are available in `analysis.json`.\n",
			interactableCount-writtenCount,
		))
	}
	document.WriteString("\n")
}

func zoneMemberSummary(members []ZoneMemberState) string {
	if len(members) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(members))
	for _, member := range members {
		parts = append(parts, fmt.Sprintf(
			"user %d gen %d slot %d abilities %d connected %t replay %t",
			member.UserID, member.PeerGeneration, member.PlayerSlot,
			member.AbilityCount, member.IsConnected, member.IsReplay,
		))
	}
	return strings.Join(parts, "; ")
}

func uint32Summary(numbers []uint32) string {
	if len(numbers) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(numbers))
	for _, number := range numbers {
		parts = append(parts, fmt.Sprint(number))
	}
	return strings.Join(parts, ", ")
}

func writeCooldownReport(document *strings.Builder, sessions []serverSessionState) {
	cooldownCount := 0
	for _, session := range sessions {
		cooldownCount += len(session.Cooldowns)
	}
	if cooldownCount == 0 {
		return
	}
	document.WriteString("## Ability cooldown reservations\n\n")
	document.WriteString("| User | Key | Ability | Remaining | Revision | Scope |\n")
	document.WriteString("| ---: | ---: | ---: | ---: | ---: | --- |\n")
	writtenCount := 0
	for _, session := range sessions {
		for _, cooldown := range session.Cooldowns {
			if writtenCount >= 40 {
				break
			}
			scope := "system"
			if cooldown.IsHeroAbility {
				scope = "hero ability"
			}
			document.WriteString(fmt.Sprintf(
				"| %d | %d | %d | %d ms | %d | %s |\n",
				session.UserID, cooldown.Key, cooldown.AbilityID,
				cooldown.RemainingMS, cooldown.Revision, scope,
			))
			writtenCount++
		}
		if writtenCount >= 40 {
			break
		}
	}
	if cooldownCount > writtenCount {
		document.WriteString(fmt.Sprintf(
			"\n%d additional retained cooldown entries are available in `analysis.json`.\n",
			cooldownCount-writtenCount,
		))
	}
	document.WriteString("\n")
}

func writeDeathReport(document *strings.Builder, sessions []serverSessionState) {
	deathCount := 0
	for _, session := range sessions {
		deathCount += len(session.Deaths)
	}
	if deathCount == 0 {
		return
	}
	document.WriteString("## Retained enemy death lifecycles\n\n")
	document.WriteString("| User | Object / source | Animation | Elapsed | Remaining deadlines | Pending tasks | Applied lifecycle state |\n")
	document.WriteString("| ---: | --- | --- | ---: | --- | ---: | --- |\n")
	writtenCount := 0
	for _, session := range sessions {
		for _, death := range session.Deaths {
			if writtenCount >= 40 {
				break
			}
			flags := fmt.Sprintf(
				"target cleared %t; immobilized %t; locomotion stopped %t; fading %t; delete %t; physics collision %t; navigation collision %t",
				death.IsTargetCleared, death.IsImmobilized,
				death.IsLocomotionStopped, death.IsCorpseFading,
				death.IsMarkedForDeletion, death.IsPhysicsCollisionEnabled,
				death.IsNavigationCollisionEnabled,
			)
			document.WriteString(fmt.Sprintf(
				"| %d | %d / %d | %s | %d ms | %s | %d | %s |\n",
				session.UserID, death.ObjectID, death.SourceObjectID,
				markdownCell(death.AnimationName), death.ElapsedMS,
				markdownCell(durationSummary(death.RemainingDeadlinesMS)),
				death.PendingTaskCount, markdownCell(flags),
			))
			writtenCount++
		}
		if writtenCount >= 40 {
			break
		}
	}
	if deathCount > writtenCount {
		document.WriteString(fmt.Sprintf(
			"\n%d additional death lifecycles are available in `analysis.json`.\n",
			deathCount-writtenCount,
		))
	}
	document.WriteString("\n")
}

func writeObjectiveReport(document *strings.Builder, sessions []serverSessionState) {
	objectiveCount := 0
	for _, session := range sessions {
		objectiveCount += len(session.Objectives)
	}
	if objectiveCount == 0 {
		return
	}
	document.WriteString("## Objective replication state\n\n")
	document.WriteString("| User | Objective | States | Tokens |\n")
	document.WriteString("| ---: | ---: | --- | --- |\n")
	writtenCount := 0
	for _, session := range sessions {
		for _, objective := range session.Objectives {
			if writtenCount >= 40 {
				break
			}
			document.WriteString(fmt.Sprintf(
				"| %d | %d | `%v` | `%v` |\n", session.UserID,
				objective.ObjectiveID, objective.States, objective.Tokens,
			))
			writtenCount++
		}
		if writtenCount >= 40 {
			break
		}
	}
	if objectiveCount > writtenCount {
		document.WriteString(fmt.Sprintf(
			"\n%d additional objectives are available in `analysis.json`.\n",
			objectiveCount-writtenCount,
		))
	}
	document.WriteString("\n")
}

func scheduleSummary(schedules []ActionScheduleState) string {
	if len(schedules) == 0 {
		return "none"
	}
	labels := make([]string, 0, len(schedules))
	for _, schedule := range schedules {
		labels = append(labels, fmt.Sprintf(
			"%s:%d (run %t, cleaner %t)", schedule.Kind, schedule.ObjectID,
			schedule.IsRunAttached, schedule.IsCleanerAttached,
		))
	}
	return boundedStrings(labels, 12)
}

func randomSummaryText(randoms []randomSummary) string {
	if len(randoms) == 0 {
		return "none"
	}
	labels := make([]string, 0, len(randoms))
	for _, random := range randoms {
		digest := random.StateSHA256
		if len(digest) > 12 {
			digest = digest[:12]
		}
		labels = append(labels, fmt.Sprintf(
			"%s draw %d index %d words %d sha256 %s", random.Kind,
			random.DrawCount, random.Index, random.WordCount, digest,
		))
	}
	return strings.Join(labels, "; ")
}

func boundedStrings(parts []string, limit int) string {
	if len(parts) == 0 {
		return "none"
	}
	if limit <= 0 || len(parts) <= limit {
		return strings.Join(parts, "; ")
	}
	return strings.Join(parts[:limit], "; ") + fmt.Sprintf(
		"; +%d more", len(parts)-limit,
	)
}

func durationSummary(durationsMS []int64) string {
	if len(durationsMS) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(durationsMS))
	for _, durationMS := range durationsMS {
		parts = append(parts, fmt.Sprintf("%d ms", durationMS))
	}
	return strings.Join(parts, ", ")
}

func locomotionStateLabel(comparison objectComparison) string {
	states := make([]string, 0, 4)
	if comparison.ClientLocomotionFlags != nil {
		states = append(states, fmt.Sprintf("flags `0x%08x`", *comparison.ClientLocomotionFlags))
	}
	if comparison.ClientPartialGoal != nil {
		partialGoal := *comparison.ClientPartialGoal
		states = append(states, fmt.Sprintf(
			"partial `%.3f, %.3f, %.3f`", partialGoal[0], partialGoal[1], partialGoal[2],
		))
	}
	if comparison.ClientTargetObjectID != 0 || comparison.ClientTargetHandle != 0 ||
		comparison.ServerTargetObjectID != 0 {
		states = append(states, fmt.Sprintf(
			"targets server `%d`, client network `%d`, handle `%d`",
			comparison.ServerTargetObjectID, comparison.ClientTargetObjectID,
			comparison.ClientTargetHandle,
		))
	}
	if comparison.ClientDesiredStop != nil {
		states = append(states, fmt.Sprintf("stop `%.3f`", *comparison.ClientDesiredStop))
	}
	if len(states) == 0 {
		return "unavailable"
	}
	return strings.Join(states, "; ")
}

func markdownText(raw string) string {
	text := strings.ReplaceAll(raw, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.TrimSpace(text)
}

func markdownCell(raw string) string {
	return strings.ReplaceAll(markdownText(raw), "|", "\\|")
}

func comparisonObjectLabel(comparison objectComparison) string {
	if comparison.AbilityName == "" {
		return comparison.NounName
	}
	if comparison.NounName == "" {
		return comparison.AbilityName
	}
	return comparison.AbilityName + " / " + comparison.NounName
}

func comparisonKindLabel(comparison objectComparison) string {
	label := comparisonObjectLabel(comparison)
	if label == "" {
		return comparison.Kind
	}
	return comparison.Kind + " / " + label
}
