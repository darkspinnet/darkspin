package snapshot

import (
	"fmt"
	"strings"
	"time"
)

func analyzeMovementHistory(report *analysisReport, req dumpRequest, events []replayEvent) {
	report.MovementSummaries = summarizeMovements(req.Movements)
	if req.TriggerState != nil && !req.TriggeredAt.IsZero() {
		delay := float64(req.TriggerState.CapturedAt.Sub(req.TriggeredAt)) / float64(time.Millisecond)
		report.TriggerStateDelayMS = &delay
		if delay > 500 {
			addFinding(report, analysisFinding{
				ID: "trigger-keyframe-delayed", Severity: "warning", Category: "capture",
				Title:  "Trigger keyframe was captured after the original observation",
				Detail: fmt.Sprintf("The authoritative trigger keyframe is %.0f ms later than the retained incident. It cannot establish the server state at the original divergence.", delay),
			})
		}
	}
	focusedObjectIDs := make(map[uint32]struct{})
	if req.ObjectID != 0 {
		focusedObjectIDs[req.ObjectID] = struct{}{}
	}
	for _, comparison := range report.ObjectComparisons {
		if comparison.IsOutlier {
			focusedObjectIDs[comparison.ObjectID] = struct{}{}
		}
	}
	for _, event := range events {
		_, isFocused := focusedObjectIDs[event.ObjectID]
		if !isFocused {
			continue
		}
		isMovementPacket := event.PacketName == "ObjectCreate" ||
			event.PacketName == "ObjectPlayerMove" || event.PacketName == "LocomotionUpdate" ||
			event.PacketName == "LocomotionUnreliable" || event.PacketName == "ObjectTeleport" ||
			event.PacketName == "ForcePhysicsUpdate" || event.PacketName == "ObjectDelete"
		if event.Kind != "locomotion_snapshot" && !isMovementPacket {
			continue
		}
		source := event.ClientFile
		line := event.ClientLine
		if event.Source == "server" {
			source, line = "raknet.jsonl", event.RakNetLine
		}
		report.MovementEvents = append(report.MovementEvents, analysisEvidence{
			Source: source, Line: line, ObjectID: event.ObjectID,
			OccurredAt: event.OccurredAt, PacketName: event.PacketName,
			Description: fmt.Sprintf("object %d: %s %s %s", event.ObjectID, event.Source, event.Kind, event.Phase),
		})
	}
	if len(report.MovementEvents) > 32 {
		report.MovementEvents = report.MovementEvents[len(report.MovementEvents)-32:]
	}
}

// A different object's mismatch is useful secondary evidence, not a cause for
// the retained trigger. Report whether that object still diverges at capture.
func classifyTriggerMovement(report *analysisReport) bool {
	if report.TriggerObjectID == 0 || !strings.Contains(report.Context, "position divergence") {
		return false
	}
	report.LikelyCause = "trigger_object_unmapped"
	report.Confidence = "low"
	report.Summary = fmt.Sprintf("The capture does not compare trigger object %d at the boundary; other object findings do not explain its divergence.", report.TriggerObjectID)
	for _, comparison := range report.ObjectComparisons {
		if comparison.ObjectID != report.TriggerObjectID {
			continue
		}
		report.LikelyCause = "trigger_position_divergence"
		report.Summary = fmt.Sprintf("Trigger object %d still differs by %.3f units (allowance %.3f). The movement mechanism remains unresolved; other object mismatches are separate findings.", comparison.ObjectID, comparison.Distance, comparison.AllowedDistance)
		if !comparison.IsOutlier {
			report.LikelyCause = "trigger_position_recovered"
			report.Summary = fmt.Sprintf("Trigger object %d is within allowance at the boundary (%.3f units versus %.3f). Use the retained onset samples to investigate the earlier divergence; unrelated offsets are not its cause.", comparison.ObjectID, comparison.Distance, comparison.AllowedDistance)
		}
		if comparison.MappingProvenance == "explicit" &&
			report.Completeness.ClientKeyframe == "complete" && report.Completeness.TimingAlignment == "complete" {
			report.Confidence = "medium"
		}
		break
	}
	for _, summary := range report.MovementSummaries {
		if summary.ObjectID != report.TriggerObjectID || summary.Pattern != "stationary_offset" {
			continue
		}
		report.Summary += " The paired history includes a stationary offset; that alone does not prove a locomotion stall."
		break
	}
	report.Recommendations = []string{
		"Use movement.json sample sequences to compare the last within-allowance position with the first outlier, then follow the cited movement packets and client before/after apply rows.",
		"Compare server position, velocity, goal and collision state with the client's root, goal, partial goal, flags and stop distance; check sample age and session/zone identity before assigning a cause.",
	}
	if report.TriggerStateDelayMS != nil && *report.TriggerStateDelayMS > 500 {
		report.Summary += " The trigger keyframe was delayed and does not represent the original observation."
		report.Recommendations = append(report.Recommendations,
			"Capture again with the updated automatic monitor to retain the authoritative onset keyframe before the archive cooldown.")
	}
	return true
}

func writeMovementAnalysis(document *strings.Builder, report analysisReport) {
	if report.TriggerObjectID != 0 {
		document.WriteString(fmt.Sprintf("\n## Trigger object\n\nObject `%d` owns the retained incident. Other object findings below are separate observations.\n", report.TriggerObjectID))
		if report.TriggerStateDelayMS != nil {
			document.WriteString(fmt.Sprintf("\nAuthoritative trigger keyframe delay: %.1f ms from the original observation.\n", *report.TriggerStateDelayMS))
		}
	}
	if len(report.MovementSummaries) > 0 {
		document.WriteString("\n## Paired movement history\n\nPositions were sampled on the same host at different times; `movement.json` records age and freshness for each pair. Travel sums exclude gaps longer than one second. Raw client line numbers refer to the original named trace, not the cropped snapshot tail.\n\n")
		document.WriteString("| Object / kind | Samples / fresh | Sequence range | First / last / maximum distance | Server / client travel | First outlier / last within allowance | Pattern |\n| --- | ---: | --- | --- | --- | --- | --- |\n")
		for _, summary := range report.MovementSummaries {
			document.WriteString(fmt.Sprintf("| %d / %s | %d / %d | %d–%d | %.3f / %.3f / %.3f | %.3f / %.3f | %d / %d | %s |\n",
				summary.ObjectID, markdownText(summary.Kind), summary.SampleCount, summary.TimedSampleCount,
				summary.FirstSequence, summary.LastSequence, summary.FirstDistance, summary.LastDistance,
				summary.MaximumDistance, summary.ServerTravel, summary.ClientTravel,
				summary.FirstOutlierSequence, summary.LastWithinAllowanceSequence, summary.Pattern))
		}
	}
	if len(report.MovementEvents) == 0 {
		return
	}
	document.WriteString("\n## Recent movement evidence for affected objects\n\n")
	for _, event := range report.MovementEvents {
		document.WriteString(fmt.Sprintf("- `%s:%d` — %s %s (%s)\n",
			event.Source, event.Line, markdownText(event.OccurredAt), markdownText(event.Description), event.PacketName))
	}
}
