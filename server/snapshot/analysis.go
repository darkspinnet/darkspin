package snapshot

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

const (
	driftBaseDistance      = 1.5
	driftDistancePerSecond = 30.0
	maximumFindingEvidence = 8
)

type analysisReport struct {
	FormatVersion         uint32               `json:"format_version"`
	SnapshotID            string               `json:"snapshot_id"`
	CreatedAt             time.Time            `json:"created_at"`
	Trigger               string               `json:"trigger"`
	Context               string               `json:"context"`
	LikelyCause           string               `json:"likely_cause"`
	Confidence            string               `json:"confidence"`
	Summary               string               `json:"summary"`
	Metrics               analysisMetrics      `json:"metrics"`
	Findings              []analysisFinding    `json:"findings"`
	ClientBoundaryStates  []clientStateSample  `json:"client_boundary_states,omitempty"`
	ServerSessions        []serverSessionState `json:"server_sessions,omitempty"`
	ServerNPCStates       []ObjectState        `json:"server_npc_states,omitempty"`
	LocomotionStalls      []objectComparison   `json:"locomotion_stalls,omitempty"`
	ProjectileComparisons []objectComparison   `json:"projectile_comparisons,omitempty"`
	ObjectComparisons     []objectComparison   `json:"object_comparisons,omitempty"`
	Recommendations       []string             `json:"recommendations"`
}

// serverSessionState keeps deterministic and command-admission context in the
// derived analysis without repeating the complete random word arrays.
type serverSessionState struct {
	Remote               string                     `json:"remote"`
	UserID               uint64                     `json:"user_id"`
	GameID               uint32                     `json:"game_id"`
	Stage                string                     `json:"stage"`
	DeployedObjectID     uint32                     `json:"deployed_object_id"`
	PlayerControl        PlayerControlState         `json:"player_control"`
	Cooldowns            []CooldownState            `json:"cooldowns,omitempty"`
	ActionSchedules      []ActionScheduleState      `json:"action_schedules,omitempty"`
	Deaths               []DeathState               `json:"deaths,omitempty"`
	Objectives           []ObjectiveState           `json:"objectives,omitempty"`
	TimelineKeys         []string                   `json:"timeline_keys,omitempty"`
	TimelineTasks        []TimelineTaskState        `json:"timeline_tasks,omitempty"`
	Randoms              []randomSummary            `json:"randoms,omitempty"`
	Squad                SquadState                 `json:"squad"`
	CrystalInventory     CrystalInventoryState      `json:"crystal_inventory"`
	AbilityRuntimes      []AbilityRuntimeState      `json:"ability_runtimes,omitempty"`
	ZoneElapsedMS        int64                      `json:"zone_elapsed_ms,omitempty"`
	ZoneCompletionID     uint64                     `json:"zone_completion_id,omitempty"`
	ZoneLevel            string                     `json:"zone_level,omitempty"`
	ZoneDifficulty       uint32                     `json:"zone_difficulty,omitempty"`
	ZoneChainLevelIndex  uint32                     `json:"zone_chain_level_index,omitempty"`
	ZoneMemberLimit      uint16                     `json:"zone_member_limit,omitempty"`
	ClearedSpawnGroupIDs []uint32                   `json:"cleared_spawn_group_ids,omitempty"`
	IsPopulationPrimed   bool                       `json:"is_population_primed"`
	IsZoneRestored       bool                       `json:"is_zone_restored"`
	IsObjectiveComplete  bool                       `json:"is_objective_complete"`
	ZoneMembers          []ZoneMemberState          `json:"zone_members,omitempty"`
	DirectorPublications []DirectorPublicationState `json:"director_publications,omitempty"`
	EncounterStages      []EncounterStageState      `json:"encounter_stages,omitempty"`
	Hordes               []HordeState               `json:"hordes,omitempty"`
	Boss                 *BossState                 `json:"boss,omitempty"`
	Security             *SecurityState             `json:"security,omitempty"`
	Interactables        []InteractableState        `json:"interactables,omitempty"`
	PendingPacketCount   int                        `json:"pending_packet_count"`
	IsPendingOverflow    bool                       `json:"is_pending_overflow"`
}

type randomSummary struct {
	Kind        string `json:"kind"`
	WordCount   int    `json:"word_count"`
	Index       uint32 `json:"index"`
	DrawCount   uint64 `json:"draw_count"`
	StateSHA256 string `json:"state_sha256"`
}

type analysisMetrics struct {
	TrafficEventCount              int     `json:"traffic_event_count"`
	InboundDatagramCount           int     `json:"inbound_datagram_count"`
	OutboundDatagramCount          int     `json:"outbound_datagram_count"`
	RetransmitCount                int     `json:"retransmit_count"`
	ACKDatagramCount               int     `json:"ack_datagram_count"`
	NACKDatagramCount              int     `json:"nack_datagram_count"`
	DatagramGapCount               uint64  `json:"datagram_gap_count"`
	DatagramOutOfOrderCount        int     `json:"datagram_out_of_order_count"`
	DatagramDecodeErrorCount       int     `json:"datagram_decode_error_count"`
	ApplicationEmitCount           int     `json:"application_emit_count"`
	ApplicationDeliverCount        int     `json:"application_deliver_count"`
	ClientEventCount               int     `json:"client_event_count"`
	ClientBoundaryStateCount       int     `json:"client_boundary_state_count"`
	ClientApplicationCount         int     `json:"client_application_count"`
	ServerClientMatchCount         int     `json:"server_client_match_count"`
	ServerClientMissingCount       int     `json:"server_client_missing_count"`
	MaximumClientDelayMS           float64 `json:"maximum_client_delay_ms,omitempty"`
	PendingServerBatchCount        int     `json:"pending_server_batch_count"`
	PendingServerPacketCount       int     `json:"pending_server_packet_count"`
	PendingServerPayloadCount      int     `json:"pending_server_payload_count"`
	PendingServerPacketByteCount   int     `json:"pending_server_packet_byte_count"`
	PendingServerCriticalCount     int     `json:"pending_server_critical_count"`
	PendingServerOverflowCount     int     `json:"pending_server_overflow_count"`
	ServerObjectCount              int     `json:"server_object_count"`
	ClientObjectCount              int     `json:"client_object_count"`
	MappedClientObjectCount        int     `json:"mapped_client_object_count"`
	ComparedObjectCount            int     `json:"compared_object_count"`
	DriftedObjectCount             int     `json:"drifted_object_count"`
	ClientLocomotionStallCount     int     `json:"client_locomotion_stall_count"`
	ServerProjectileCount          int     `json:"server_projectile_count"`
	ComparedProjectileCount        int     `json:"compared_projectile_count"`
	DriftedProjectileCount         int     `json:"drifted_projectile_count"`
	MissingClientProjectileCount   int     `json:"missing_client_projectile_count"`
	OrderRegressionCount           int     `json:"order_regression_count"`
	LifecycleAnomalyCount          int     `json:"lifecycle_anomaly_count"`
	IncompleteClientApplyCount     int     `json:"incomplete_client_apply_count"`
	MalformedClientLineCount       int     `json:"malformed_client_line_count"`
	DroppedEventCount              uint64  `json:"dropped_event_count"`
	ClientRingLineCount            uint64  `json:"client_ring_line_count,omitempty"`
	ClientRingByteCount            uint64  `json:"client_ring_byte_count,omitempty"`
	ClientCapacityDroppedLineCount uint64  `json:"client_capacity_dropped_line_count,omitempty"`
	ClientCapacityDroppedByteCount uint64  `json:"client_capacity_dropped_byte_count,omitempty"`
	IsClientRingTruncated          bool    `json:"is_client_ring_truncated,omitempty"`
}

type analysisFinding struct {
	ID       string             `json:"id"`
	Severity string             `json:"severity"`
	Category string             `json:"category"`
	Title    string             `json:"title"`
	Detail   string             `json:"detail"`
	ObjectID uint32             `json:"object_id,omitempty"`
	Evidence []analysisEvidence `json:"evidence,omitempty"`
}

type analysisEvidence struct {
	Source      string `json:"source"`
	Line        int    `json:"line,omitempty"`
	Sequence    uint64 `json:"sequence,omitempty"`
	OccurredAt  string `json:"occurred_at,omitempty"`
	PacketName  string `json:"packet_name,omitempty"`
	ObjectID    uint32 `json:"object_id,omitempty"`
	Description string `json:"description"`
}

type clientStateSample struct {
	Kind          string `json:"kind"`
	Value         uint32 `json:"value"`
	ClientLine    int    `json:"client_line"`
	ClientTimeMS  uint64 `json:"client_time_ms"`
	ThreadID      uint32 `json:"thread_id,omitempty"`
	FrameSequence uint64 `json:"frame_sequence,omitempty"`
}

type objectComparison struct {
	ObjectID       uint32 `json:"object_id"`
	ClientObjectID uint32 `json:"client_object_id,omitempty"`
	Kind           string `json:"kind,omitempty"`
	NounName       string `json:"noun_name,omitempty"`
	AbilityName    string `json:"ability_name,omitempty"`

	ServerPosition            [3]float32  `json:"server_position"`
	ClientPosition            [3]float32  `json:"client_position"`
	ClientGoal                *[3]float32 `json:"client_goal,omitempty"`
	ClientPartialGoal         *[3]float32 `json:"client_partial_goal,omitempty"`
	Distance                  float64     `json:"distance"`
	AllowedDistance           float64     `json:"allowed_distance"`
	ServerGoalDistance        *float64    `json:"server_goal_distance,omitempty"`
	ClientGoalDistance        *float64    `json:"client_goal_distance,omitempty"`
	SampleDeltaMS             float64     `json:"sample_delta_ms"`
	IsOutlier                 bool        `json:"is_outlier"`
	IsClientLocomotionStalled bool        `json:"is_client_locomotion_stalled,omitempty"`
	ServerTargetObjectID      uint32      `json:"server_target_object_id,omitempty"`
	ClientTargetObjectID      uint32      `json:"client_target_object_id,omitempty"`
	ClientTargetHandle        uint32      `json:"client_target_handle,omitempty"`
	ClientLocomotionFlags     *uint32     `json:"client_locomotion_flags,omitempty"`
	ClientDesiredStop         *float32    `json:"client_desired_stop,omitempty"`
	ClientLine                int         `json:"client_line"`
	ClientGoalLine            int         `json:"client_goal_line,omitempty"`
}

type sequenceCursor struct {
	sequence uint32
}

type lifecycleCursor struct {
	isCreated     bool
	isMoveStarted bool
	isDeleted     bool
}

type clientPosition struct {
	clientObjectID uint32
	position       [3]float32
	line           int
}

type clientLocomotionGoal struct {
	position             [3]float32
	partialGoal          *[3]float32
	targetObjectID       uint32
	clientTargetObjectID uint32
	locomotionFlags      *uint32
	desiredStop          *float32
	line                 int
}

func analyzeSnapshot(
	id string, req dumpRequest, state StateFrame, events []trafficEvent,
	timeline replayTimeline, capture clientCapture, droppedEventCount uint64,
	createdAt time.Time,
) analysisReport {
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	report := analysisReport{
		FormatVersion: 9, SnapshotID: id, CreatedAt: createdAt.UTC(),
		Trigger: req.Trigger, Context: req.Context,
		Findings: make([]analysisFinding, 0),
		Metrics: analysisMetrics{
			TrafficEventCount: len(events), ClientEventCount: timeline.ClientEventCount,
			MalformedClientLineCount:       timeline.ClientMalformedLineCount,
			DroppedEventCount:              droppedEventCount,
			ClientRingLineCount:            timeline.ClientRingLineCount,
			ClientRingByteCount:            timeline.ClientRingByteCount,
			ClientCapacityDroppedLineCount: timeline.ClientCapacityDroppedLineCount,
			ClientCapacityDroppedByteCount: timeline.ClientCapacityDroppedByteCount,
			IsClientRingTruncated:          timeline.IsClientRingTruncated,
		},
	}
	analyzeTransport(&report, events)
	analyzeTimeline(&report, timeline.Events)
	analyzeClientBoundaryStates(&report, timeline.Events)
	analyzePendingServerPackets(&report, state)
	analyzeServerSessions(&report, state)
	analyzeObjectState(
		&report, state, timeline.Events, capture.RequestedAt,
		capture.IsCaptured && timeline.IsClientAligned,
	)
	analyzeCompleteness(&report, capture, timeline)
	classifyAnalysis(&report)
	return report
}

func analyzeServerSessions(report *analysisReport, state StateFrame) {
	for _, session := range state.Sessions {
		if !isServerSessionReplayCaptured(session) {
			continue
		}
		randoms := make([]randomSummary, 0, len(session.Randoms))
		for _, random := range session.Randoms {
			randoms = append(randoms, randomSummary{
				Kind: random.Kind, WordCount: len(random.Words), Index: random.Index,
				DrawCount: random.DrawCount, StateSHA256: random.StateSHA256,
			})
		}
		report.ServerSessions = append(report.ServerSessions, serverSessionState{
			Remote: session.Remote, UserID: session.UserID, GameID: session.GameID,
			Stage: session.Stage, DeployedObjectID: session.DeployedObjectID,
			PlayerControl: session.PlayerControl, Cooldowns: session.Cooldowns,
			ActionSchedules: session.ActionSchedules, Deaths: session.Deaths,
			Objectives: session.Objectives, TimelineKeys: session.TimelineKeys,
			TimelineTasks: session.TimelineTasks, Randoms: randoms,
			Squad: session.Squad, CrystalInventory: session.CrystalInventory,
			AbilityRuntimes:  session.AbilityRuntimes,
			ZoneElapsedMS:    session.ZoneElapsedMS,
			ZoneCompletionID: session.ZoneCompletionID,
			ZoneLevel:        session.ZoneLevel, ZoneDifficulty: session.ZoneDifficulty,
			ZoneChainLevelIndex:  session.ZoneChainLevelIndex,
			ZoneMemberLimit:      session.ZoneMemberLimit,
			ClearedSpawnGroupIDs: session.ClearedSpawnGroupIDs,
			IsPopulationPrimed:   session.IsPopulationPrimed,
			IsZoneRestored:       session.IsZoneRestored,
			IsObjectiveComplete:  session.IsObjectiveComplete,
			ZoneMembers:          session.ZoneMembers, EncounterStages: session.EncounterStages,
			DirectorPublications: session.DirectorPublications,
			Hordes:               session.Hordes, Boss: session.Boss, Security: session.Security,
			Interactables:      session.Interactables,
			PendingPacketCount: session.PendingPacketCount,
			IsPendingOverflow:  session.IsPendingOverflow,
		})
	}
}

func isServerSessionReplayCaptured(session SessionState) bool {
	control := session.PlayerControl
	return !control.MotionStartedAt.IsZero() || control.MotionRevision != 0 ||
		control.AbilityReleaseRevision != 0 || control.BasicSequence.Revision != 0 ||
		len(session.Cooldowns) != 0 || len(session.ActionSchedules) != 0 ||
		len(session.Deaths) != 0 || len(session.Objectives) != 0 ||
		len(session.TimelineKeys) != 0 || len(session.Randoms) != 0 ||
		len(session.Squad.Characters) != 0 || len(session.AbilityRuntimes) != 0 ||
		len(session.ZoneMembers) != 0 || len(session.EncounterStages) != 0 ||
		len(session.DirectorPublications) != 0 ||
		len(session.Hordes) != 0 || session.Boss != nil ||
		len(session.Interactables) != 0
}

func analyzeClientBoundaryStates(report *analysisReport, events []replayEvent) {
	isCollecting := false
	states := make([]clientStateSample, 0)
	for _, event := range events {
		if event.Source != "client" {
			continue
		}
		if event.Kind == "action_snapshot_boundary" {
			isCollecting = true
			states = states[:0]
			continue
		}
		if !isCollecting {
			continue
		}
		if event.Kind == "snapshot_boundary" {
			break
		}
		if event.ClientStateValue == nil {
			continue
		}
		states = append(states, clientStateSample{
			Kind: event.Kind, Value: *event.ClientStateValue,
			ClientLine: event.ClientLine, ClientTimeMS: event.ClientTimeMS,
			ThreadID: event.ClientThreadID, FrameSequence: event.FrameSequence,
		})
	}
	report.ClientBoundaryStates = states
	report.Metrics.ClientBoundaryStateCount = len(states)
}

func analyzePendingServerPackets(report *analysisReport, state StateFrame) {
	overflowEvidence := make([]analysisEvidence, 0, maximumFindingEvidence)
	for _, session := range state.Sessions {
		batchIDs := make(map[uint64]struct{})
		report.Metrics.PendingServerPacketCount += session.PendingPacketCount
		for _, packet := range session.PendingPackets {
			report.Metrics.PendingServerPayloadCount++
			report.Metrics.PendingServerPacketByteCount += packet.PayloadSize
			batchIDs[packet.BatchID] = struct{}{}
			if packet.IsCritical {
				report.Metrics.PendingServerCriticalCount++
			}
		}
		report.Metrics.PendingServerBatchCount += len(batchIDs)
		if !session.IsPendingOverflow {
			continue
		}
		report.Metrics.PendingServerOverflowCount++
		overflowEvidence = appendBoundedEvidence(
			overflowEvidence,
			analysisEvidence{
				Source: "server-state.json",
				Description: fmt.Sprintf(
					"session %q retained %d pending output packets after its bounded pending queue overflowed",
					session.Remote, session.PendingPacketCount,
				),
			},
		)
	}
	if report.Metrics.PendingServerOverflowCount == 0 {
		return
	}
	addFinding(report, analysisFinding{
		ID: "server-pending-output-truncated", Severity: "warning", Category: "capture",
		Title: "Server pending output history was truncated",
		Detail: fmt.Sprintf(
			"%d captured sessions had overflowed their bounded pending gameplay packet queue before the snapshot boundary.",
			report.Metrics.PendingServerOverflowCount,
		),
		Evidence: overflowEvidence,
	})
}

func analyzeTransport(report *analysisReport, events []trafficEvent) {
	cursors := make(map[string]sequenceCursor)
	gapEvidence := make([]analysisEvidence, 0, maximumFindingEvidence)
	orderEvidence := make([]analysisEvidence, 0, maximumFindingEvidence)
	for index, event := range events {
		if event.Stage == raknet.ObservationRetransmit {
			report.Metrics.RetransmitCount++
			continue
		}
		if event.Stage != raknet.ObservationDatagram || len(event.Payload) == 0 {
			continue
		}
		if event.Direction == raknet.ObservationClientToServer {
			report.Metrics.InboundDatagramCount++
		} else if event.Direction == raknet.ObservationServerToClient {
			report.Metrics.OutboundDatagramCount++
		}
		switch event.Payload[0] {
		case 0xc0:
			report.Metrics.ACKDatagramCount++
			continue
		case 0xa0:
			report.Metrics.NACKDatagramCount++
			continue
		}
		if event.Payload[0]&0x80 == 0 {
			continue
		}
		datagram, err := raknet.DecodeDatagram(event.Payload)
		if err != nil {
			report.Metrics.DatagramDecodeErrorCount++
			continue
		}
		key := string(event.Direction) + "|" + event.Remote
		currentEvidence := trafficEvidence(event, index+1, "RakNet data sequence")
		cursor, isFound := cursors[key]
		if isFound {
			delta := triadDelta(cursor.sequence, datagram.Sequence)
			if delta > 1 && delta < 0x00800000 {
				report.Metrics.DatagramGapCount += uint64(delta - 1)
				gapEvidence = appendBoundedEvidence(gapEvidence, currentEvidence)
			} else if delta >= 0x00800000 {
				report.Metrics.DatagramOutOfOrderCount++
				orderEvidence = appendBoundedEvidence(orderEvidence, currentEvidence)
			}
			if delta == 0 || delta >= 0x00800000 {
				continue
			}
		}
		cursors[key] = sequenceCursor{sequence: datagram.Sequence}
	}
	if report.Metrics.DatagramGapCount > 0 {
		addFinding(report, analysisFinding{
			ID: "transport-sequence-gaps", Severity: "info", Category: "capture",
			Title:    "RakNet datagram sequence discontinuities observed",
			Detail:   fmt.Sprintf("The bounded observer crossed %d absent data-datagram sequence numbers. This is capture-window context, not packet-loss evidence unless NACK or retransmit records corroborate it.", report.Metrics.DatagramGapCount),
			Evidence: gapEvidence,
		})
	}
	if report.Metrics.DatagramOutOfOrderCount > 0 {
		addFinding(report, analysisFinding{
			ID: "transport-out-of-order", Severity: "info", Category: "transport",
			Title:    "Late or duplicate RakNet datagrams observed",
			Detail:   fmt.Sprintf("%d data datagrams arrived behind the newest sequence for their peer and direction.", report.Metrics.DatagramOutOfOrderCount),
			Evidence: orderEvidence,
		})
	}
	if report.Metrics.RetransmitCount > 0 || report.Metrics.NACKDatagramCount > 0 {
		addFinding(report, analysisFinding{
			ID: "transport-recovery", Severity: "warning", Category: "transport",
			Title:  "RakNet loss recovery was active",
			Detail: fmt.Sprintf("The window contains %d NACK datagrams and %d server retransmissions.", report.Metrics.NACKDatagramCount, report.Metrics.RetransmitCount),
		})
	}
}

func analyzeTimeline(report *analysisReport, events []replayEvent) {
	orderIndexes := make(map[string]uint32)
	orderEvidence := make([]analysisEvidence, 0, maximumFindingEvidence)
	flowsByObjectID := make(map[uint32]lifecycleCursor)
	lifecycleFindings := make([]analysisFinding, 0)
	serverEmitsByDigest := make(map[string][]replayEvent)
	clientApplyCounts := make(map[uint32][2]int)
	for _, event := range events {
		switch event.Kind {
		case "application_emit":
			report.Metrics.ApplicationEmitCount++
			if event.Direction == raknet.ObservationServerToClient && event.PayloadSHA256 != "" {
				serverEmitsByDigest[event.PayloadSHA256] = append(
					serverEmitsByDigest[event.PayloadSHA256], event,
				)
			}
		case "application_deliver":
			report.Metrics.ApplicationDeliverCount++
		case "application_receive":
			report.Metrics.ClientApplicationCount++
			matchClientApplication(report, serverEmitsByDigest, event)
		case "locomotion_snapshot":
			if event.ObjectID == 0 {
				continue
			}
			counts := clientApplyCounts[event.ObjectID]
			if event.Phase == "before_apply" {
				counts[0]++
			} else if event.Phase == "after_apply" {
				counts[1]++
			}
			clientApplyCounts[event.ObjectID] = counts
		}
		if event.Kind != "application_emit" && event.Kind != "application_deliver" {
			continue
		}
		if isOrderedReliability(event.Reliability) {
			key := string(event.Direction) + "|" + event.Remote + fmt.Sprintf("|%d", event.OrderChannel)
			previous, isFound := orderIndexes[key]
			isForward := true
			if isFound {
				delta := triadDelta(previous, event.OrderIndex)
				if delta >= 0x00800000 {
					report.Metrics.OrderRegressionCount++
					orderEvidence = appendBoundedEvidence(orderEvidence, replayEvidence(event, "Ordering index moved backward"))
				}
				if delta == 0 || delta >= 0x00800000 {
					isForward = false
				}
			}
			if isForward {
				orderIndexes[key] = event.OrderIndex
			}
		}
		lifecycleFindings = analyzeLifecycleEvent(lifecycleFindings, flowsByObjectID, event)
	}
	if report.Metrics.ClientApplicationCount > 0 {
		for _, emits := range serverEmitsByDigest {
			for _, event := range emits {
				if event.OffsetMS != nil && *event.OffsetMS > -500 {
					continue
				}
				report.Metrics.ServerClientMissingCount++
			}
		}
	}
	if report.Metrics.OrderRegressionCount > 0 {
		addFinding(report, analysisFinding{
			ID: "application-order-regression", Severity: "critical", Category: "ordering",
			Title:    "Application ordering index regressed",
			Detail:   fmt.Sprintf("%d ordered application events moved backward within a peer channel.", report.Metrics.OrderRegressionCount),
			Evidence: orderEvidence,
		})
	}
	lifecycleIDs := make(map[string]struct{})
	for _, finding := range lifecycleFindings {
		if _, isFound := lifecycleIDs[finding.ID]; isFound {
			continue
		}
		lifecycleIDs[finding.ID] = struct{}{}
		addFinding(report, finding)
		report.Metrics.LifecycleAnomalyCount++
	}
	clientApplyObjectIDs := make([]uint32, 0, len(clientApplyCounts))
	for objectID := range clientApplyCounts {
		clientApplyObjectIDs = append(clientApplyObjectIDs, objectID)
	}
	sort.Slice(clientApplyObjectIDs, func(left int, right int) bool {
		return clientApplyObjectIDs[left] < clientApplyObjectIDs[right]
	})
	for _, objectID := range clientApplyObjectIDs {
		counts := clientApplyCounts[objectID]
		if counts[0] <= counts[1] {
			continue
		}
		report.Metrics.IncompleteClientApplyCount += counts[0] - counts[1]
		addFinding(report, analysisFinding{
			ID: fmt.Sprintf("client-apply-incomplete-%d", objectID), Severity: "warning",
			Category: "client_apply", Title: "Client movement apply did not complete",
			Detail:   fmt.Sprintf("Object %d has %d before-apply images and %d after-apply images in the capture.", objectID, counts[0], counts[1]),
			ObjectID: objectID,
		})
	}
	if report.Metrics.ServerClientMissingCount > 0 {
		addFinding(report, analysisFinding{
			ID: "client-application-missing", Severity: "warning", Category: "client_receive",
			Title:  "Server application payloads lack a client receive match",
			Detail: fmt.Sprintf("%d server payloads emitted at least 500ms before the boundary have no same-byte client application trace match. Capture boundaries and client trace drops remain possible explanations.", report.Metrics.ServerClientMissingCount),
		})
	}
}

func analyzeLifecycleEvent(
	findings []analysisFinding, flowsByObjectID map[uint32]lifecycleCursor,
	event replayEvent,
) []analysisFinding {
	if event.Direction != raknet.ObservationServerToClient || event.ObjectID == 0 {
		return findings
	}
	flow := flowsByObjectID[event.ObjectID]
	switch raknet.PacketID(event.PacketID) {
	case raknet.ObjectCreate:
		flow = lifecycleCursor{isCreated: true}
	case raknet.ObjectPlayerMove:
		if flow.isDeleted {
			findings = append(findings, lifecycleFinding(event, "movement-after-delete", "Movement intent followed object deletion"))
		}
		flow.isMoveStarted = true
	case raknet.LocomotionUnreliable:
		if flow.isCreated && !flow.isMoveStarted && !flow.isDeleted {
			findings = append(findings, lifecycleFinding(event, "locomotion-before-intent", "Locomotion update preceded movement intent"))
		} else if flow.isDeleted {
			findings = append(findings, lifecycleFinding(event, "locomotion-after-delete", "Locomotion update followed object deletion"))
		}
	case raknet.ObjectDelete:
		if flow.isDeleted {
			findings = append(findings, lifecycleFinding(event, "duplicate-delete", "Object was deleted more than once"))
		}
		flow.isDeleted = true
	}
	flowsByObjectID[event.ObjectID] = flow
	return findings
}

func analyzeObjectState(
	report *analysisReport, state StateFrame, events []replayEvent, boundaryAt time.Time,
	isClientBoundaryCaptured bool,
) {
	serverObjectsByID := make(map[uint32]ObjectState)
	for _, session := range state.Sessions {
		for _, object := range session.Objects {
			if object.ObjectID == 0 {
				continue
			}
			serverObjectsByID[object.ObjectID] = object
		}
	}
	report.Metrics.ServerObjectCount = len(serverObjectsByID)
	serverObjectIDs := make([]uint32, 0, len(serverObjectsByID))
	for objectID := range serverObjectsByID {
		serverObjectIDs = append(serverObjectIDs, objectID)
	}
	sort.Slice(serverObjectIDs, func(left int, right int) bool {
		return serverObjectIDs[left] < serverObjectIDs[right]
	})
	for _, objectID := range serverObjectIDs {
		object := serverObjectsByID[objectID]
		if object.Kind == "npc" {
			report.ServerNPCStates = append(report.ServerNPCStates, object)
		}
		if object.Kind == "projectile" {
			report.Metrics.ServerProjectileCount++
		}
	}
	clientPositionsByID := make(map[uint32]clientPosition)
	clientGoalsByID := make(map[uint32]clientLocomotionGoal)
	clientObjectIDs := make(map[uint32]struct{})
	for _, event := range events {
		if event.Source != "client" {
			continue
		}
		if event.Kind == "locomotion_snapshot" && event.Phase == "snapshot_boundary" &&
			event.ObjectID != 0 && len(event.GoalBits) == 3 {
			goal, isValid := decodePosition(event.GoalBits)
			if isValid {
				locomotionGoal := clientLocomotionGoal{
					position: goal, targetObjectID: event.TargetObjectID,
					clientTargetObjectID: event.ClientTargetObjectID,
					locomotionFlags:      event.LocomotionFlags, line: event.ClientLine,
				}
				if len(event.PartialGoalBits) == 3 {
					partialGoal, isPartialGoalValid := decodePosition(event.PartialGoalBits)
					if isPartialGoalValid {
						locomotionGoal.partialGoal = &partialGoal
					}
				}
				if event.DesiredStopBits != nil {
					desiredStop := math.Float32frombits(*event.DesiredStopBits)
					if !math.IsNaN(float64(desiredStop)) &&
						!math.IsInf(float64(desiredStop), 0) {
						locomotionGoal.desiredStop = &desiredStop
					}
				}
				clientGoalsByID[event.ObjectID] = locomotionGoal
			}
		}
		if event.Kind != "object_memory" || len(event.PositionBits) != 3 {
			continue
		}
		position, isValid := decodePosition(event.PositionBits)
		if !isValid {
			continue
		}
		if event.ClientObjectID != 0 {
			clientObjectIDs[event.ClientObjectID] = struct{}{}
		}
		if event.ObjectID == 0 {
			continue
		}
		clientPositionsByID[event.ObjectID] = clientPosition{
			clientObjectID: event.ClientObjectID,
			position:       position,
			line:           event.ClientLine,
		}
	}
	sampleDeltaMS := float64(0)
	if !state.CapturedAt.IsZero() && !boundaryAt.IsZero() {
		sampleDeltaMS = float64(boundaryAt.Sub(state.CapturedAt)) / float64(time.Millisecond)
	}
	report.Metrics.ClientObjectCount = len(clientObjectIDs)
	report.Metrics.MappedClientObjectCount = len(clientPositionsByID)
	if isClientBoundaryCaptured {
		missingProjectileEvidence := make([]analysisEvidence, 0, maximumFindingEvidence)
		for _, objectID := range serverObjectIDs {
			object := serverObjectsByID[objectID]
			if object.Kind != "projectile" {
				continue
			}
			if _, isFound := clientPositionsByID[objectID]; isFound {
				continue
			}
			if sampleDeltaMS > 0 && object.RemainingDurationMS > 0 &&
				float64(object.RemainingDurationMS) <= sampleDeltaMS+100 {
				continue
			}
			report.Metrics.MissingClientProjectileCount++
			missingProjectileEvidence = appendBoundedEvidence(
				missingProjectileEvidence,
				analysisEvidence{
					Source: "server-state.json", ObjectID: objectID,
					Description: fmt.Sprintf(
						"active server projectile %q from ability %q has no network-associated client boundary object",
						object.NounName, object.AbilityName,
					),
				},
			)
		}
		if report.Metrics.MissingClientProjectileCount > 0 {
			addFinding(report, analysisFinding{
				ID: "client-projectile-missing", Severity: "critical",
				Category: "state_divergence",
				Title:    "Active server projectiles were absent from client memory",
				Detail: fmt.Sprintf(
					"%d authoritative active projectiles had no RakNet-identified client object at the aligned boundary.",
					report.Metrics.MissingClientProjectileCount,
				),
				Evidence: missingProjectileEvidence,
			})
		}
	}
	comparedNonProjectileCount := 0
	for objectID, client := range clientPositionsByID {
		serverObject, isFound := serverObjectsByID[objectID]
		if !isFound {
			continue
		}
		distance := positionDistance(serverObject.Position, client.position)
		movementSpeed := driftDistancePerSecond
		if float64(serverObject.Speed) > movementSpeed {
			movementSpeed = float64(serverObject.Speed)
		}
		allowedDistance := driftBaseDistance +
			math.Abs(sampleDeltaMS)/1000*movementSpeed
		isClientOrbitPresented := isClientOrbitPresentedNPC(serverObject.NounName)
		comparison := objectComparison{
			ObjectID: objectID, ClientObjectID: client.clientObjectID,
			Kind: serverObject.Kind, NounName: serverObject.NounName,
			AbilityName:    serverObject.AbilityName,
			ServerPosition: serverObject.Position, ClientPosition: client.position,
			ServerTargetObjectID: serverObject.TargetObjectID,
			Distance:             distance, AllowedDistance: allowedDistance,
			SampleDeltaMS: sampleDeltaMS,
			IsOutlier:     distance > allowedDistance && !isClientOrbitPresented,
			ClientLine:    client.line,
		}
		goal, isGoalFound := clientGoalsByID[objectID]
		if isGoalFound {
			clientGoal := goal.position
			serverGoalDistance := positionDistance(serverObject.Position, clientGoal)
			clientGoalDistance := positionDistance(client.position, clientGoal)
			comparison.ClientGoal = &clientGoal
			comparison.ClientPartialGoal = goal.partialGoal
			comparison.ServerGoalDistance = &serverGoalDistance
			comparison.ClientGoalDistance = &clientGoalDistance
			comparison.ClientTargetObjectID = goal.targetObjectID
			comparison.ClientTargetHandle = goal.clientTargetObjectID
			comparison.ClientLocomotionFlags = goal.locomotionFlags
			comparison.ClientDesiredStop = goal.desiredStop
			comparison.ClientGoalLine = goal.line
			comparison.IsClientLocomotionStalled = comparison.IsOutlier &&
				serverGoalDistance <= allowedDistance &&
				clientGoalDistance > allowedDistance
		}
		report.ObjectComparisons = append(report.ObjectComparisons, comparison)
		report.Metrics.ComparedObjectCount++
		if serverObject.Kind == "projectile" {
			report.Metrics.ComparedProjectileCount++
			report.ProjectileComparisons = append(report.ProjectileComparisons, comparison)
		} else {
			comparedNonProjectileCount++
		}
		if comparison.IsOutlier {
			report.Metrics.DriftedObjectCount++
			if serverObject.Kind == "projectile" {
				report.Metrics.DriftedProjectileCount++
			}
		}
		if comparison.IsClientLocomotionStalled {
			report.Metrics.ClientLocomotionStallCount++
			report.LocomotionStalls = append(report.LocomotionStalls, comparison)
		}
	}
	sortObjectComparisons(report.ObjectComparisons)
	sortObjectComparisons(report.ProjectileComparisons)
	sortObjectComparisons(report.LocomotionStalls)
	if report.Metrics.ClientLocomotionStallCount > 0 {
		evidence := make([]analysisEvidence, 0, maximumFindingEvidence)
		for _, comparison := range report.LocomotionStalls {
			evidence = appendBoundedEvidence(
				evidence, locomotionStallEvidence(comparison),
			)
		}
		addFinding(report, analysisFinding{
			ID: "client-locomotion-stalled", Severity: "critical",
			Category: "client_locomotion",
			Title:    "Client locomotion stalled",
			Detail: fmt.Sprintf(
				"%d mapped objects have an authoritative server position aligned with the client locomotion goal while the client object root remains elsewhere.",
				report.Metrics.ClientLocomotionStallCount,
			),
			Evidence: evidence,
		})
	}
	if report.Metrics.DriftedProjectileCount > 0 {
		evidence := make([]analysisEvidence, 0, maximumFindingEvidence)
		for _, comparison := range report.ProjectileComparisons {
			if !comparison.IsOutlier {
				continue
			}
			evidence = appendBoundedEvidence(evidence, comparisonPositionEvidence(comparison))
		}
		addFinding(report, analysisFinding{
			ID: "projectile-position-drift", Severity: "critical",
			Category: "state_divergence",
			Title:    "Client and server projectile positions diverged",
			Detail: fmt.Sprintf(
				"%d of %d RakNet-identified projectiles exceed the sampling-time-adjusted position allowance.",
				report.Metrics.DriftedProjectileCount,
				report.Metrics.ComparedProjectileCount,
			),
			Evidence: evidence,
		})
	}
	driftedNonStalledObjectCount := 0
	for _, comparison := range report.ObjectComparisons {
		if comparison.IsOutlier && comparison.Kind != "projectile" &&
			!comparison.IsClientLocomotionStalled {
			driftedNonStalledObjectCount++
		}
	}
	if driftedNonStalledObjectCount > 0 {
		evidence := make([]analysisEvidence, 0, maximumFindingEvidence)
		for _, comparison := range report.ObjectComparisons {
			if !comparison.IsOutlier || comparison.Kind == "projectile" ||
				comparison.IsClientLocomotionStalled {
				continue
			}
			evidence = appendBoundedEvidence(evidence, comparisonPositionEvidence(comparison))
		}
		addFinding(report, analysisFinding{
			ID: "state-position-drift", Severity: "critical", Category: "state_divergence",
			Title: "Client and server object positions diverged",
			Detail: fmt.Sprintf(
				"%d of %d matched non-projectile boundary objects exceed the sampling-time-adjusted position allowance.",
				driftedNonStalledObjectCount, comparedNonProjectileCount,
			),
			Evidence: evidence,
		})
	}
}

func sortObjectComparisons(comparisons []objectComparison) {
	sort.Slice(comparisons, func(left int, right int) bool {
		if comparisons[left].Distance == comparisons[right].Distance {
			return comparisons[left].ObjectID < comparisons[right].ObjectID
		}
		return comparisons[left].Distance > comparisons[right].Distance
	})
}

func comparisonPositionEvidence(comparison objectComparison) analysisEvidence {
	return analysisEvidence{
		Source: "client-memory.jsonl", Line: comparison.ClientLine,
		ObjectID: comparison.ObjectID,
		Description: fmt.Sprintf(
			"server (%.3f, %.3f, %.3f), client (%.3f, %.3f, %.3f), distance %.3f exceeds time-adjusted allowance %.3f; client handle %d",
			comparison.ServerPosition[0], comparison.ServerPosition[1],
			comparison.ServerPosition[2], comparison.ClientPosition[0],
			comparison.ClientPosition[1], comparison.ClientPosition[2],
			comparison.Distance, comparison.AllowedDistance, comparison.ClientObjectID,
		),
	}
}

func locomotionStallEvidence(comparison objectComparison) analysisEvidence {
	if comparison.ClientGoal == nil || comparison.ServerGoalDistance == nil ||
		comparison.ClientGoalDistance == nil {
		return comparisonPositionEvidence(comparison)
	}
	goal := *comparison.ClientGoal
	locomotionContext := locomotionComparisonContext(comparison)
	return analysisEvidence{
		Source: "client-memory.jsonl", Line: comparison.ClientLine,
		ObjectID: comparison.ObjectID,
		Description: fmt.Sprintf(
			"server position (%.3f, %.3f, %.3f) equals client goal (%.3f, %.3f, %.3f) within %.3f, but client object remains at (%.3f, %.3f, %.3f), %.3f from its goal and beyond allowance %.3f; client handle %d, goal source line %d%s",
			comparison.ServerPosition[0], comparison.ServerPosition[1],
			comparison.ServerPosition[2], goal[0], goal[1], goal[2],
			*comparison.ServerGoalDistance, comparison.ClientPosition[0],
			comparison.ClientPosition[1], comparison.ClientPosition[2],
			*comparison.ClientGoalDistance, comparison.AllowedDistance,
			comparison.ClientObjectID, comparison.ClientGoalLine, locomotionContext,
		),
	}
}

func locomotionComparisonContext(comparison objectComparison) string {
	context := ""
	if comparison.ClientLocomotionFlags != nil {
		context += fmt.Sprintf(", flags 0x%08x", *comparison.ClientLocomotionFlags)
	}
	if comparison.ClientPartialGoal != nil {
		partialGoal := *comparison.ClientPartialGoal
		context += fmt.Sprintf(
			", partial goal (%.3f, %.3f, %.3f)",
			partialGoal[0], partialGoal[1], partialGoal[2],
		)
	}
	if comparison.ClientTargetObjectID != 0 || comparison.ClientTargetHandle != 0 ||
		comparison.ServerTargetObjectID != 0 {
		context += fmt.Sprintf(
			", targets server %d/client network %d/client handle %d",
			comparison.ServerTargetObjectID, comparison.ClientTargetObjectID,
			comparison.ClientTargetHandle,
		)
	}
	if comparison.ClientDesiredStop != nil {
		context += fmt.Sprintf(", desired stop %.3f", *comparison.ClientDesiredStop)
	}
	return context
}

func analyzeCompleteness(report *analysisReport, capture clientCapture, timeline replayTimeline) {
	if report.Metrics.DroppedEventCount > 0 {
		addFinding(report, analysisFinding{
			ID: "server-ring-truncated", Severity: "warning", Category: "capture",
			Title:  "Server rolling history was truncated",
			Detail: fmt.Sprintf("%d events inside the requested window were evicted by the server ring's memory bound before this dump.", report.Metrics.DroppedEventCount),
		})
	}
	if !capture.IsCaptured {
		addFinding(report, analysisFinding{
			ID: "client-memory-unavailable", Severity: "warning", Category: "capture",
			Title:  "Client in-memory boundary dump was unavailable",
			Detail: "The report uses the persistent Fang trace tail and cannot make a complete client object-state comparison.",
		})
	} else if !timeline.IsClientAligned {
		addFinding(report, analysisFinding{
			ID: "client-clock-unaligned", Severity: "warning", Category: "capture",
			Title:  "Client boundary could not be aligned to server UTC",
			Detail: "The requested client dump exists, but its matching boundary marker or server timestamp is missing.",
		})
	}
	if timeline.ClientMalformedLineCount > 0 {
		addFinding(report, analysisFinding{
			ID: "client-lines-malformed", Severity: "warning", Category: "capture",
			Title:  "Malformed client trace lines were skipped",
			Detail: fmt.Sprintf("%d client lines could not be decoded into the replay timeline.", timeline.ClientMalformedLineCount),
		})
	}
	if timeline.IsClientRingTruncated {
		addFinding(report, analysisFinding{
			ID: "client-ring-truncated", Severity: "warning", Category: "capture",
			Title: "Client rolling history was truncated by its memory limit",
			Detail: fmt.Sprintf(
				"The Fang ring reached its capacity during the requested window; cumulative capture losses since collection began are %d lines and %d bytes.",
				timeline.ClientCapacityDroppedLineCount,
				timeline.ClientCapacityDroppedByteCount,
			),
		})
	}
}

func classifyAnalysis(report *analysisReport) {
	cause := "not_isolated"
	confidence := "low"
	summary := "The capture did not isolate one dominant desync mechanism."
	if report.Metrics.ClientLocomotionStallCount > 0 {
		cause = "client_locomotion_stalled"
		confidence = "high"
		summary = "The server position matches the client locomotion goal, but the client object root remains elsewhere."
	} else if report.Metrics.OrderRegressionCount > 0 || report.Metrics.LifecycleAnomalyCount > 0 {
		cause = "server_packet_ordering"
		confidence = "high"
		summary = "Application ordering or object lifecycle evidence is the strongest root-cause signal."
	} else if report.Metrics.DriftedObjectCount > 0 ||
		report.Metrics.MissingClientProjectileCount > 0 {
		cause = "client_server_state_divergence"
		confidence = "high"
		summary = "Aligned client object memory disagrees with authoritative active-object presence or positions."
	} else if report.Metrics.IncompleteClientApplyCount > 0 {
		cause = "client_application_path"
		confidence = "medium"
		summary = "Movement entered the client apply path without a corresponding post-apply image."
	} else if report.Metrics.NACKDatagramCount > 0 || report.Metrics.RetransmitCount > 0 {
		cause = "transport_loss_or_retransmission"
		confidence = "medium"
		summary = "RakNet NACK or retransmission traffic is the strongest signal in this window."
	} else if report.Metrics.ServerClientMissingCount > 0 {
		cause = "client_receive_gap"
		confidence = "medium"
		summary = "Some server-emitted application bytes have no matching client receive observation."
	}
	report.LikelyCause = cause
	report.Confidence = confidence
	report.Summary = summary
	report.Recommendations = recommendationsForCause(cause)
}

func recommendationsForCause(cause string) []string {
	switch cause {
	case "client_locomotion_stalled":
		return []string{"Inspect the cited object-memory and boundary locomotion rows to identify which client field stopped advancing after the last correct movement apply.", "Compare the mapped object's final ObjectPlayerMove, LocomotionUpdate, LocomotionUnreliable, and teleport events without changing server authority."}
	case "server_packet_ordering":
		return []string{"Inspect the cited timeline events and their order channel before changing gameplay state logic.", "Verify create, movement-intent, locomotion, teleport, and delete emission order for the affected object."}
	case "client_server_state_divergence":
		return []string{"Compare missing projectiles and the highest-distance object rows with their create, movement, teleport, and delete payloads in timeline.jsonl.", "Use the client boundary and before/after locomotion images to identify the first field or lifecycle event that diverges from the server keyframe."}
	case "client_application_path":
		return []string{"Inspect the final before_apply image and nearby application_receive event for the affected object.", "Check for a missing object or locomotion component resolution event immediately after packet construction."}
	case "transport_loss_or_retransmission":
		return []string{"Inspect NACK, retransmit, and missing sequence evidence by peer and direction.", "Confirm whether ordered movement traffic was delayed behind a recovered reliable message."}
	case "client_receive_gap":
		return []string{"Compare unmatched server payload hashes with Fang socket datagrams and application_receive lines.", "Check whether the missing payload falls at a capture boundary before treating it as packet loss."}
	default:
		return []string{"Reproduce with auto mode near the affected object and use the trigger context to narrow the next bundle.", "Inspect the timeline around teleports, projectile creation, forced physics, and modifier transitions."}
	}
}

func matchClientApplication(
	report *analysisReport, serverEmitsByDigest map[string][]replayEvent, client replayEvent,
) {
	if client.PayloadSHA256 == "" {
		return
	}
	emits := serverEmitsByDigest[client.PayloadSHA256]
	if len(emits) == 0 {
		return
	}
	server := emits[0]
	if len(emits) == 1 {
		delete(serverEmitsByDigest, client.PayloadSHA256)
	} else {
		serverEmitsByDigest[client.PayloadSHA256] = emits[1:]
	}
	report.Metrics.ServerClientMatchCount++
	if server.OffsetMS == nil || client.OffsetMS == nil {
		return
	}
	delayMS := *client.OffsetMS - *server.OffsetMS
	if delayMS > report.Metrics.MaximumClientDelayMS {
		report.Metrics.MaximumClientDelayMS = delayMS
	}
}

func lifecycleFinding(event replayEvent, suffix string, title string) analysisFinding {
	return analysisFinding{
		ID:       fmt.Sprintf("object-%d-%s", event.ObjectID, suffix),
		Severity: "critical", Category: "lifecycle", Title: title,
		Detail:   fmt.Sprintf("Object %d received %s at replay event %d.", event.ObjectID, event.PacketName, event.Sequence),
		ObjectID: event.ObjectID,
		Evidence: []analysisEvidence{replayEvidence(event, title)},
	}
}

func trafficEvidence(event trafficEvent, line int, description string) analysisEvidence {
	return analysisEvidence{
		Source: "raknet.jsonl", Line: line, Sequence: event.Sequence,
		OccurredAt: event.CapturedAt.UTC().Format(time.RFC3339Nano),
		PacketName: packetName(event.PacketID), ObjectID: event.ObjectID,
		Description: description,
	}
}

func replayEvidence(event replayEvent, description string) analysisEvidence {
	source := "timeline.jsonl"
	return analysisEvidence{
		Source: source, Line: int(event.Sequence), Sequence: event.Sequence,
		OccurredAt: event.OccurredAt, PacketName: event.PacketName,
		ObjectID: event.ObjectID, Description: description,
	}
}

func appendBoundedEvidence(
	evidence []analysisEvidence, current analysisEvidence,
) []analysisEvidence {
	if len(evidence) >= maximumFindingEvidence {
		return evidence
	}
	return append(evidence, current)
}

func addFinding(report *analysisReport, finding analysisFinding) {
	report.Findings = append(report.Findings, finding)
}

func triadDelta(previous uint32, current uint32) uint32 {
	return (current - previous) & 0x00ffffff
}

func isOrderedReliability(reliability raknet.Reliability) bool {
	return reliability == raknet.UnreliableSequenced ||
		reliability == raknet.ReliableOrdered ||
		reliability == raknet.ReliableSequenced ||
		reliability == raknet.ReliableOrderedWithACKReceipt
}

func decodePosition(bits []uint32) ([3]float32, bool) {
	position := [3]float32{
		math.Float32frombits(bits[0]),
		math.Float32frombits(bits[1]),
		math.Float32frombits(bits[2]),
	}
	for _, coordinate := range position {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return [3]float32{}, false
		}
	}
	return position, true
}

func positionDistance(left [3]float32, right [3]float32) float64 {
	x := float64(right[0] - left[0])
	y := float64(right[1] - left[1])
	z := float64(right[2] - left[2])
	return math.Sqrt(x*x + y*y + z*z)
}
