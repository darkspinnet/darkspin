package snapshot

import (
	"fmt"
	"math"
	"strings"
)

type clientControlHistory struct {
	Status               string                `json:"status"`
	SampleCount          int                   `json:"sample_count"`
	InputEventCount      int                   `json:"input_event_count"`
	TruncatedSampleCount int                   `json:"truncated_sample_count"`
	SpanMS               uint64                `json:"span_ms"`
	MaximumGapMS         uint64                `json:"maximum_gap_ms"`
	BoundaryAgeMS        *uint64               `json:"boundary_age_ms,omitempty"`
	Samples              []clientControlSample `json:"samples,omitempty"`
	Inputs               []clientControlSample `json:"inputs,omitempty"`
}

type clientControlSample struct {
	ClientTimeMS           uint64            `json:"client_time_ms"`
	OffsetMS               *float64          `json:"offset_ms,omitempty"`
	FrameSequence          uint64            `json:"frame_sequence,omitempty"`
	ClientLine             int               `json:"client_line"`
	States                 map[string]uint32 `json:"states"`
	IsTruncated            bool              `json:"is_truncated,omitempty"`
	CameraCenterSourceGap  *float64          `json:"camera_center_source_gap,omitempty"`
	CameraSourceObjectGap  *float64          `json:"camera_source_object_gap,omitempty"`
	CameraCenterDesiredGap *float64          `json:"camera_center_desired_gap,omitempty"`
}

// Keep the native samples, including missing fields, rather than interpreting
// a correct follow target or a recovered boundary as proof of synchronization.
func analyzeClientControlHistory(events []replayEvent) clientControlHistory {
	history := clientControlHistory{Status: "unavailable"}
	var lastTimeMS uint64
	for _, event := range events {
		if event.Source != "client" {
			continue
		}
		if event.Kind == "snapshot_boundary" && history.SampleCount > 0 {
			ageMS := uint64(uint32(event.ClientTimeMS - lastTimeMS))
			history.BoundaryAgeMS = &ageMS
		}
		if event.Kind != "control_history" && event.Kind != "control_input" {
			continue
		}
		sample := clientControlSample{
			ClientTimeMS: event.ClientTimeMS, OffsetMS: event.OffsetMS,
			FrameSequence: event.FrameSequence, ClientLine: event.ClientLine,
			States: event.ClientStates, IsTruncated: event.IsClientStateTruncated,
		}
		if event.Kind == "control_input" {
			history.Inputs = append(history.Inputs, sample)
			history.InputEventCount++
			continue
		}
		sample.CameraCenterSourceGap = controlVectorGap(sample.States, "camera_center", "camera_source")
		sample.CameraSourceObjectGap = controlVectorGap(sample.States, "camera_source", "camera_object")
		sample.CameraCenterDesiredGap = controlVectorGap(sample.States, "camera_center", "camera_desired")
		if history.SampleCount > 0 {
			gapMS := uint64(uint32(event.ClientTimeMS - lastTimeMS))
			history.SpanMS += gapMS
			if gapMS > history.MaximumGapMS {
				history.MaximumGapMS = gapMS
			}
		}
		lastTimeMS = event.ClientTimeMS
		if sample.IsTruncated {
			history.TruncatedSampleCount++
		}
		history.Samples = append(history.Samples, sample)
		history.SampleCount++
	}
	if history.SampleCount > 0 {
		history.Status = "sampled"
	}
	return history
}

func controlVectorGap(states map[string]uint32, first, second string) *float64 {
	var squaredDistance float64
	for _, axis := range []string{"x", "y", "z"} {
		firstBits, isFirstPresent := states[first+"_"+axis+"_bits"]
		secondBits, isSecondPresent := states[second+"_"+axis+"_bits"]
		if !isFirstPresent || !isSecondPresent {
			return nil
		}
		delta := float64(math.Float32frombits(firstBits)) - float64(math.Float32frombits(secondBits))
		if math.IsNaN(delta) || math.IsInf(delta, 0) {
			return nil
		}
		squaredDistance += delta * delta
	}
	distance := math.Sqrt(squaredDistance)
	return &distance
}

func writeClientControlHistory(document *strings.Builder, history clientControlHistory) {
	document.WriteString("\n## Client input, action and camera history\n\n")
	if history.SampleCount == 0 {
		document.WriteString("Native control history is unavailable in this capture. Boundary state cannot rule out an earlier input block or camera/hero misalignment that recovered before submission.\n\n")
	} else {
		fmt.Fprintf(document,
			"%d samples over %d ms; largest sample gap %d ms; %d truncated samples; %d window input events.\n\n",
			history.SampleCount, history.SpanMS, history.MaximumGapMS,
			history.TruncatedSampleCount, history.InputEventCount,
		)
		if history.BoundaryAgeMS != nil {
			fmt.Fprintf(document, "Last sample was %d ms before the client boundary.\n\n", *history.BoundaryAgeMS)
		}
		document.WriteString("Build-103 native state is sampled on the game thread at most every 100 ms. Frame stalls and transitions between samples remain blind spots; this is not a complete input history. Full samples and input events are retained in analysis.json and timeline.jsonl.\n\n")
		document.WriteString("Camera distances are world-space observations, not fault thresholds: authored offsets, smoothing and scripted modes can produce nonzero gaps. A correct target alone does not establish alignment with the hero. Compare the same-time camera source, object position, controlled handle and server movement history.\n\n")
		document.WriteString("Latest 20 samples (missing fields are `?`):\n\n")
		document.WriteString("| Client ms / frame | Hero / camera handle | Move held / basic repeat | Pending ability active / sent / sync | Active / response / queued sync | Center→source / source→object / center→desired | Client line |\n")
		document.WriteString("| --- | --- | --- | --- | --- | --- | ---: |\n")
		for _, sample := range history.Samples[max(0, len(history.Samples)-20):] {
			fmt.Fprintf(document, "| %d / %d | %s | %s | %s | %s | %s / %s / %s | %d |\n",
				sample.ClientTimeMS, sample.FrameSequence,
				controlFields(sample.States, "combat_controlled_handle", "camera_follow_target"),
				controlFields(sample.States, "input_move_hold", "input_basic_repeat"),
				controlFields(sample.States, "pending_ability_active", "pending_ability_sent", "pending_ability_sync"),
				controlFields(sample.States, "action_active_sync", "action_response_sync", "action_queued_sync"),
				controlDistance(sample.CameraCenterSourceGap), controlDistance(sample.CameraSourceObjectGap),
				controlDistance(sample.CameraCenterDesiredGap), sample.ClientLine,
			)
		}
	}
	if history.InputEventCount == 0 {
		document.WriteString("\nNo mouse/focus edges were retained; this does not prove the player supplied no input.\n\n")
		return
	}
	document.WriteString("\nWindow events establish delivery to the game window, not consumption by gameplay. Keyboard/text input is not recorded. Latest 20 events:\n\n")
	document.WriteString("| Client ms | Windows message | Mouse button flags | Client line |\n| ---: | ---: | ---: | ---: |\n")
	for _, input := range history.Inputs[max(0, len(history.Inputs)-20):] {
		fmt.Fprintf(document, "| %d | %s | %s | %d |\n", input.ClientTimeMS,
			controlFields(input.States, "window_message"), controlFields(input.States, "mouse_buttons"), input.ClientLine)
	}
	document.WriteString("\n")
}

func controlFields(states map[string]uint32, names ...string) string {
	fields := make([]string, 0, len(names))
	for _, name := range names {
		field, isPresent := states[name]
		if !isPresent {
			fields = append(fields, "?")
			continue
		}
		fields = append(fields, fmt.Sprint(field))
	}
	return strings.Join(fields, " / ")
}

func controlDistance(distance *float64) string {
	if distance == nil {
		return "?"
	}
	return fmt.Sprintf("%.3f", *distance)
}
