package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type replayEvent struct {
	Sequence                uint64                      `json:"sequence"`
	OccurredAt              string                      `json:"occurred_at,omitempty"`
	OffsetMS                *float64                    `json:"offset_ms,omitempty"`
	TimeBasis               string                      `json:"time_basis"`
	Source                  string                      `json:"source"`
	Kind                    string                      `json:"kind"`
	Protocol                string                      `json:"protocol,omitempty"`
	Direction               raknet.ObservationDirection `json:"direction,omitempty"`
	Stage                   raknet.ObservationStage     `json:"stage,omitempty"`
	Remote                  string                      `json:"remote,omitempty"`
	TraceID                 uint64                      `json:"trace_id,omitempty"`
	PeerGeneration          uint64                      `json:"peer_generation,omitempty"`
	RakNetEventSequence     uint64                      `json:"raknet_event_sequence,omitempty"`
	RakNetLine              int                         `json:"raknet_line,omitempty"`
	PartRakNetLines         []int                       `json:"part_raknet_lines,omitempty"`
	ClientFile              string                      `json:"client_file,omitempty"`
	ClientLine              int                         `json:"client_line,omitempty"`
	ClientTimeMS            uint64                      `json:"client_time_ms,omitempty"`
	ClientThreadID          uint32                      `json:"client_thread_id,omitempty"`
	FrameSequence           uint64                      `json:"frame_sequence,omitempty"`
	FrameDeltaBits          uint32                      `json:"frame_delta_bits,omitempty"`
	FrameTimeMS             uint64                      `json:"frame_time_ms,omitempty"`
	Phase                   string                      `json:"phase,omitempty"`
	Call                    string                      `json:"call,omitempty"`
	Request                 string                      `json:"request,omitempty"`
	ServerTimeUnixNano      int64                       `json:"server_time_unix_nano,omitempty"`
	ClientStateValue        *uint32                     `json:"client_state_value,omitempty"`
	ClientBufferMS          uint64                      `json:"client_buffer_ms,omitempty"`
	ClientRingLineCount     uint64                      `json:"client_ring_line_count,omitempty"`
	ClientRingByteCount     uint64                      `json:"client_ring_byte_count,omitempty"`
	ClientOldestTimeMS      uint64                      `json:"client_oldest_time_ms,omitempty"`
	ClientNewestTimeMS      uint64                      `json:"client_newest_time_ms,omitempty"`
	ClientDroppedLineCount  uint64                      `json:"client_capacity_dropped_line_count,omitempty"`
	ClientDroppedByteCount  uint64                      `json:"client_capacity_dropped_byte_count,omitempty"`
	ClientLastDroppedTimeMS uint64                      `json:"client_capacity_last_dropped_time_ms,omitempty"`
	DatagramSequence        uint32                      `json:"datagram_sequence,omitempty"`
	Reliability             raknet.Reliability          `json:"reliability,omitempty"`
	MessageIndex            uint32                      `json:"message_index,omitempty"`
	OrderIndex              uint32                      `json:"order_index,omitempty"`
	OrderChannel            uint8                       `json:"order_channel,omitempty"`
	PacketID                uint8                       `json:"packet_id,omitempty"`
	PacketName              string                      `json:"packet_name,omitempty"`
	ObjectID                uint32                      `json:"object_id,omitempty"`
	ClientObjectID          uint32                      `json:"client_object_id,omitempty"`
	TargetObjectID          uint32                      `json:"target_object_id,omitempty"`
	ClientTargetObjectID    uint32                      `json:"client_target_object_id,omitempty"`
	PayloadSize             int                         `json:"payload_size,omitempty"`
	PayloadSHA256           string                      `json:"payload_sha256,omitempty"`
	IsReassembled           bool                        `json:"is_reassembled,omitempty"`
	SplitCount              uint32                      `json:"split_count,omitempty"`
	PositionBits            []uint32                    `json:"position_bits,omitempty"`
	GoalBits                []uint32                    `json:"goal_bits,omitempty"`
	PartialGoalBits         []uint32                    `json:"partial_goal_bits,omitempty"`
	LocomotionFlags         *uint32                     `json:"locomotion_flags,omitempty"`
	DesiredStopBits         *uint32                     `json:"desired_stop_bits,omitempty"`
	sortTime                time.Time
	ordinal                 uint64
}

type replayTimeline struct {
	Events                          []replayEvent
	ClientEventCount                int
	ClientMalformedLineCount        int
	ClientBoundaryTimeMS            uint64
	ClientRingLineCount             uint64
	ClientRingByteCount             uint64
	ClientCapacityDroppedLineCount  uint64
	ClientCapacityDroppedByteCount  uint64
	ClientCapacityLastDroppedTimeMS uint64
	IsClientAligned                 bool
	IsClientRingTruncated           bool
}

type clientTraceEvent struct {
	TimeMS                    uint64   `json:"time_ms"`
	ServerTimeUnixNano        int64    `json:"server_time_unix_nano"`
	Protocol                  string   `json:"protocol"`
	Kind                      string   `json:"kind"`
	Direction                 string   `json:"direction"`
	Call                      string   `json:"call"`
	Phase                     string   `json:"phase"`
	Request                   string   `json:"request"`
	ThreadID                  uint32   `json:"thread"`
	FrameSequence             uint64   `json:"frame_sequence"`
	FrameDeltaBits            uint32   `json:"frame_delta_bits"`
	FrameTimeMS               uint64   `json:"frame_time_ms"`
	MessageID                 uint8    `json:"message_id"`
	ObjectID                  uint32   `json:"object_id"`
	Size                      int      `json:"size"`
	PayloadHex                string   `json:"payload_hex"`
	ObjectHex                 string   `json:"object_hex"`
	ComponentHex              string   `json:"component_hex"`
	PositionBits              []uint32 `json:"position_bits"`
	GoalBits                  []uint32 `json:"goal_bits"`
	PartialGoalBits           []uint32 `json:"partial_goal_bits"`
	LocomotionFlags           uint32   `json:"flags"`
	TargetObjectID            uint32   `json:"target_object_id"`
	DesiredStopBits           uint32   `json:"desired_stop_bits"`
	Value                     *uint32  `json:"value"`
	RingLineCount             uint64   `json:"ring_line_count"`
	RingByteCount             uint64   `json:"ring_byte_count"`
	OldestTimeMS              uint64   `json:"oldest_time_ms"`
	NewestTimeMS              uint64   `json:"newest_time_ms"`
	CapacityDroppedLineCount  uint64   `json:"capacity_dropped_line_count"`
	CapacityDroppedByteCount  uint64   `json:"capacity_dropped_byte_count"`
	CapacityLastDroppedTimeMS uint64   `json:"capacity_last_dropped_time_ms"`
	BufferMS                  uint64   `json:"buffer_ms"`
}

type clientTraceRecord struct {
	Line  int
	Event clientTraceEvent
}

type clientObjectMapper struct {
	networkObjectIDsByHandle map[uint32]uint32
	pendingNetworkObjectID   uint32
}

type splitKey struct {
	remote         string
	peerGeneration uint64
	splitID        uint16
	splitCount     uint32
}

type splitAssembly struct {
	parts      [][]byte
	lines      []int
	isReceived []bool
	remaining  uint32
	latest     trafficEvent
	packet     raknet.EncapsulatedPacket
}

func buildReplayTimeline(
	events []trafficEvent, clientLines [][]byte, clientFile string,
	request string, boundaryAt time.Time,
) replayTimeline {
	timeline := replayTimeline{}
	timeline.Events = appendServerReplayEvents(timeline.Events, events, boundaryAt)
	clientRecords, malformedCount := decodeClientTraceEvents(clientLines)
	timeline.ClientEventCount = len(clientRecords)
	timeline.ClientMalformedLineCount = malformedCount
	clientBoundary, clientBoundaryAt, isAligned := clientReplayBoundary(
		clientRecords, request, boundaryAt,
	)
	timeline.ClientBoundaryTimeMS = clientBoundary.TimeMS
	timeline.ClientRingLineCount = clientBoundary.RingLineCount
	timeline.ClientRingByteCount = clientBoundary.RingByteCount
	timeline.ClientCapacityDroppedLineCount = clientBoundary.CapacityDroppedLineCount
	timeline.ClientCapacityDroppedByteCount = clientBoundary.CapacityDroppedByteCount
	timeline.ClientCapacityLastDroppedTimeMS = clientBoundary.CapacityLastDroppedTimeMS
	timeline.IsClientAligned = isAligned
	if clientBoundary.CapacityLastDroppedTimeMS != 0 && clientBoundary.BufferMS != 0 &&
		clientBoundary.TimeMS >= clientBoundary.CapacityLastDroppedTimeMS {
		timeline.IsClientRingTruncated =
			clientBoundary.TimeMS-clientBoundary.CapacityLastDroppedTimeMS <= clientBoundary.BufferMS
	}
	objectMapper := clientObjectMapper{
		networkObjectIDsByHandle: make(map[uint32]uint32),
	}
	clientEventStart := len(timeline.Events)
	for index, record := range clientRecords {
		event := record.Event
		packetID, payloadObjectID := clientApplicationIdentity(event)
		networkObjectID := objectMapper.mapEvent(event, packetID, payloadObjectID)
		replay := replayEvent{
			TimeBasis: "client_monotonic", Source: "client", Kind: event.Kind,
			Protocol: event.Protocol, ClientFile: clientFile, ClientLine: record.Line,
			ClientTimeMS: event.TimeMS, ClientThreadID: event.ThreadID,
			FrameSequence: event.FrameSequence, FrameDeltaBits: event.FrameDeltaBits,
			FrameTimeMS: event.FrameTimeMS, Phase: event.Phase, Call: event.Call,
			Request: event.Request, ServerTimeUnixNano: event.ServerTimeUnixNano,
			ClientStateValue: cloneUint32(event.Value), ClientBufferMS: event.BufferMS,
			ClientRingLineCount:     event.RingLineCount,
			ClientRingByteCount:     event.RingByteCount,
			ClientOldestTimeMS:      event.OldestTimeMS,
			ClientNewestTimeMS:      event.NewestTimeMS,
			ClientDroppedLineCount:  event.CapacityDroppedLineCount,
			ClientDroppedByteCount:  event.CapacityDroppedByteCount,
			ClientLastDroppedTimeMS: event.CapacityLastDroppedTimeMS,
			PacketID:                packetID, ObjectID: networkObjectID,
			ClientObjectID: event.ObjectID, ClientTargetObjectID: event.TargetObjectID,
			PayloadSize:     clientPayloadSize(event),
			PositionBits:    append([]uint32(nil), event.PositionBits...),
			GoalBits:        append([]uint32(nil), event.GoalBits...),
			PartialGoalBits: append([]uint32(nil), event.PartialGoalBits...),
			ordinal:         uint64(len(timeline.Events) + index),
		}
		if event.Kind == "locomotion_snapshot" {
			locomotionFlags := event.LocomotionFlags
			replay.LocomotionFlags = &locomotionFlags
			desiredStopBits := event.DesiredStopBits
			replay.DesiredStopBits = &desiredStopBits
			replay.TargetObjectID = objectMapper.networkObjectID(event.TargetObjectID)
		}
		if direction := parseReplayDirection(event.Direction); direction != "" {
			replay.Direction = direction
		} else if event.Kind == "application_receive" {
			replay.Direction = raknet.ObservationServerToClient
		}
		if replay.PacketID != 0 {
			replay.PacketName = packetName(replay.PacketID)
		}
		replay.PayloadSHA256 = clientPayloadDigest(event)
		if isAligned {
			deltaMS := clientTimeDelta(event.TimeMS, clientBoundary.TimeMS)
			replay.sortTime = clientBoundaryAt.Add(time.Duration(deltaMS) * time.Millisecond)
			replay.OccurredAt = replay.sortTime.UTC().Format(time.RFC3339Nano)
			replay.TimeBasis = "client_boundary"
			offsetMS := float64(replay.sortTime.Sub(boundaryAt)) / float64(time.Millisecond)
			replay.OffsetMS = &offsetMS
		}
		timeline.Events = append(timeline.Events, replay)
	}
	for index := clientEventStart; index < len(timeline.Events); index++ {
		event := &timeline.Events[index]
		if event.ClientTargetObjectID == 0 || event.TargetObjectID != 0 {
			continue
		}
		event.TargetObjectID = objectMapper.networkObjectID(event.ClientTargetObjectID)
	}
	sort.SliceStable(timeline.Events, func(left int, right int) bool {
		leftTime := timeline.Events[left].sortTime
		rightTime := timeline.Events[right].sortTime
		if leftTime.IsZero() != rightTime.IsZero() {
			return !leftTime.IsZero()
		}
		if !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		return timeline.Events[left].ordinal < timeline.Events[right].ordinal
	})
	for index := range timeline.Events {
		timeline.Events[index].Sequence = uint64(index + 1)
	}
	return timeline
}

func (e *clientObjectMapper) networkObjectID(clientObjectID uint32) uint32 {
	if e == nil || clientObjectID == 0 {
		return 0
	}
	return e.networkObjectIDsByHandle[clientObjectID]
}

func cloneUint32(number *uint32) *uint32 {
	if number == nil {
		return nil
	}
	copy := *number
	return &copy
}

func (e *clientObjectMapper) mapEvent(
	event clientTraceEvent, packetID uint8, payloadObjectID uint32,
) uint32 {
	if event.Kind == "application_receive" {
		if isClientObjectMappingPacket(packetID) {
			e.pendingNetworkObjectID = payloadObjectID
		}
		return payloadObjectID
	}
	if event.ObjectID == 0 {
		return 0
	}
	networkObjectID := e.networkObjectIDsByHandle[event.ObjectID]
	if event.Kind != "locomotion_snapshot" ||
		(event.Phase != "before_apply" && event.Phase != "after_apply") {
		return networkObjectID
	}
	if e.pendingNetworkObjectID != 0 {
		networkObjectID = e.pendingNetworkObjectID
		e.networkObjectIDsByHandle[event.ObjectID] = networkObjectID
	}
	if event.Phase == "after_apply" {
		e.pendingNetworkObjectID = 0
	}
	return networkObjectID
}

func clientApplicationIdentity(event clientTraceEvent) (uint8, uint32) {
	if event.Kind != "application_receive" || event.PayloadHex == "" {
		return event.MessageID, 0
	}
	payload, err := hex.DecodeString(event.PayloadHex)
	if err != nil || len(payload) == 0 {
		return event.MessageID, 0
	}
	packetID, objectID := applicationIdentity(payload)
	if packetID == 0 {
		packetID = event.MessageID
	}
	return packetID, objectID
}

func isClientObjectMappingPacket(packetID uint8) bool {
	switch raknet.PacketID(packetID) {
	case raknet.ObjectPlayerMove, raknet.LocomotionUpdate, raknet.LocomotionUnreliable:
		return true
	default:
		return false
	}
}

func appendServerReplayEvents(
	timeline []replayEvent, events []trafficEvent, boundaryAt time.Time,
) []replayEvent {
	assemblies := make(map[splitKey]*splitAssembly)
	for index, event := range events {
		kind := "datagram"
		if event.Stage == raknet.ObservationRetransmit {
			kind = "retransmit"
		} else if event.Stage == raknet.ObservationApplication {
			kind = "application_deliver"
		}
		replay := serverReplayEvent(event, boundaryAt, index+1, kind, event.Payload)
		timeline = append(timeline, replay)
		if event.Direction != raknet.ObservationServerToClient ||
			event.Stage != raknet.ObservationDatagram || len(event.Payload) == 0 ||
			event.Payload[0]&0x80 == 0 || event.Payload[0] == 0xa0 ||
			event.Payload[0] == 0xc0 {
			continue
		}
		datagram, err := raknet.DecodeDatagram(event.Payload)
		if err != nil {
			continue
		}
		for _, packet := range datagram.Packets {
			if len(packet.Payload) == 0 {
				continue
			}
			if !packet.IsSplit {
				application := serverReplayEvent(
					event, boundaryAt, index+1, "application_emit", packet.Payload,
				)
				applyPacketMetadata(&application, packet)
				timeline = append(timeline, application)
				continue
			}
			key := splitKey{
				remote: event.Remote, peerGeneration: event.PeerGeneration,
				splitID: packet.SplitID, splitCount: packet.SplitCount,
			}
			assembly := assemblies[key]
			if assembly == nil && packet.SplitCount > 0 && packet.SplitCount <= 4096 {
				assembly = &splitAssembly{
					parts: make([][]byte, packet.SplitCount), lines: make([]int, packet.SplitCount),
					isReceived: make([]bool, packet.SplitCount), remaining: packet.SplitCount,
					packet: packet,
				}
				assemblies[key] = assembly
			}
			if assembly == nil || packet.SplitIndex >= uint32(len(assembly.parts)) ||
				assembly.isReceived[packet.SplitIndex] {
				continue
			}
			assembly.parts[packet.SplitIndex] = append([]byte(nil), packet.Payload...)
			assembly.lines[packet.SplitIndex] = index + 1
			assembly.isReceived[packet.SplitIndex] = true
			assembly.remaining--
			assembly.latest = event
			if assembly.remaining != 0 {
				continue
			}
			payload := joinSplitParts(assembly.parts)
			application := serverReplayEvent(
				assembly.latest, boundaryAt, index+1, "application_emit", payload,
			)
			applyPacketMetadata(&application, assembly.packet)
			application.PacketID, application.ObjectID = applicationIdentity(payload)
			application.PacketName = packetName(application.PacketID)
			application.IsReassembled = true
			application.SplitCount = packet.SplitCount
			application.PartRakNetLines = append([]int(nil), assembly.lines...)
			timeline = append(timeline, application)
			delete(assemblies, key)
		}
	}
	return timeline
}

func serverReplayEvent(
	event trafficEvent, boundaryAt time.Time, line int, kind string, payload []byte,
) replayEvent {
	packetID := event.PacketID
	objectID := event.ObjectID
	if kind == "application_emit" || kind == "application_deliver" {
		packetID, objectID = applicationIdentity(payload)
	}
	offsetMS := float64(event.CapturedAt.Sub(boundaryAt)) / float64(time.Millisecond)
	replay := replayEvent{
		OccurredAt: event.CapturedAt.UTC().Format(time.RFC3339Nano), OffsetMS: &offsetMS,
		TimeBasis: "server_utc", Source: "server", Kind: kind,
		Protocol: "raknet", Direction: event.Direction, Stage: event.Stage,
		Remote: event.Remote, TraceID: event.TraceID, PeerGeneration: event.PeerGeneration,
		RakNetEventSequence: event.Sequence, RakNetLine: line,
		DatagramSequence: event.DatagramSequence, Reliability: event.Reliability,
		MessageIndex: event.MessageIndex, OrderIndex: event.OrderIndex,
		OrderChannel: event.OrderChannel, PacketID: packetID,
		PacketName: packetName(packetID), ObjectID: objectID,
		PayloadSize: len(payload), PayloadSHA256: payloadDigest(payload),
		sortTime: event.CapturedAt, ordinal: event.Sequence * 2,
	}
	return replay
}

func applyPacketMetadata(event *replayEvent, packet raknet.EncapsulatedPacket) {
	event.Reliability = packet.Reliability
	event.MessageIndex = packet.MessageIndex
	event.OrderIndex = packet.OrderIndex
	event.OrderChannel = packet.OrderChannel
	event.PacketID, event.ObjectID = applicationIdentity(packet.Payload)
	event.PacketName = packetName(event.PacketID)
}

func decodeClientTraceEvents(lines [][]byte) ([]clientTraceRecord, int) {
	records := make([]clientTraceRecord, 0, len(lines))
	malformedCount := 0
	for index, line := range lines {
		event := clientTraceEvent{}
		err := json.Unmarshal(line, &event)
		if err != nil || event.TimeMS == 0 || event.Kind == "" {
			malformedCount++
			continue
		}
		records = append(records, clientTraceRecord{Line: index + 1, Event: event})
	}
	return records, malformedCount
}

func clientReplayBoundary(
	records []clientTraceRecord, request string, fallback time.Time,
) (clientTraceEvent, time.Time, bool) {
	for index := len(records) - 1; index >= 0; index-- {
		event := records[index].Event
		if event.Kind != "snapshot_boundary" || event.Request != request {
			continue
		}
		boundaryAt := fallback
		if event.ServerTimeUnixNano > 0 {
			boundaryAt = time.Unix(0, event.ServerTimeUnixNano).UTC()
		}
		return event, boundaryAt, true
	}
	return clientTraceEvent{}, time.Time{}, false
}

func clientTimeDelta(current uint64, boundary uint64) int64 {
	if current >= boundary {
		return int64(current - boundary)
	}
	return -int64(boundary - current)
}

func parseReplayDirection(raw string) raknet.ObservationDirection {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(raknet.ObservationClientToServer):
		return raknet.ObservationClientToServer
	case string(raknet.ObservationServerToClient):
		return raknet.ObservationServerToClient
	default:
		return ""
	}
}

func clientPayloadDigest(event clientTraceEvent) string {
	for _, encoded := range []string{event.PayloadHex, event.ObjectHex, event.ComponentHex} {
		if encoded == "" {
			continue
		}
		payload, err := hex.DecodeString(encoded)
		if err != nil {
			return ""
		}
		return payloadDigest(payload)
	}
	return ""
}

func clientPayloadSize(event clientTraceEvent) int {
	if event.Size > 0 {
		return event.Size
	}
	for _, encoded := range []string{event.PayloadHex, event.ObjectHex, event.ComponentHex} {
		if len(encoded)%2 == 0 && encoded != "" {
			return len(encoded) / 2
		}
	}
	return 0
}

func payloadDigest(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func packetName(packetID uint8) string {
	contract, isFound := raknet.LookupApplicationPacketContract(raknet.PacketID(packetID))
	if !isFound {
		return ""
	}
	return contract.Name
}

func joinSplitParts(parts [][]byte) []byte {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	payload := make([]byte, 0, total)
	for _, part := range parts {
		payload = append(payload, part...)
	}
	return payload
}

func writeReplayTimeline(path string, events []replayEvent) error {
	w, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("timelineCreate: %w", err)
	}
	encoder := json.NewEncoder(w)
	for index, event := range events {
		err = encoder.Encode(event)
		if err != nil {
			closeErr := w.Close()
			if closeErr != nil {
				return fmt.Errorf("timelineEncode[%d]: %w; close: %v", index, err, closeErr)
			}
			return fmt.Errorf("timelineEncode[%d]: %w", index, err)
		}
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("timelineClose: %w", err)
	}
	return nil
}
