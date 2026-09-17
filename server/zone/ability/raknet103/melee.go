package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	simraknet103 "github.com/darkspinnet/darkspin/server/sim/raknet103"
)

const timedMeleePhase sim.Phase = "timedMelee"
const timedMeleeActorRole sim.Role = "meleeActor"
const timedMeleeTargetRole sim.Role = "meleeTarget"

type MeleeInput struct {
	Ability                sim.AbilityDefinition
	ActorObjectID          uint32
	TargetObjectID         uint32
	ActorPosition          sim.Position
	TargetPosition         sim.Position
	TargetFacing           sim.Position
	Damage                 float32
	TargetHitPoint         float32
	TargetManaPoint        float32
	ActorTeam              uint8
	SourceTime             uint64
	HitDelays              []time.Duration
	IsUnreliableStopNeeded bool
}

type MeleeRun struct {
	simulator       *sim.Simulator
	session         *sim.Session
	behavior        *sim.TimedMeleeBehavior
	outbox          *timedMeleeOutbox
	cancel          raknet.CancelSchedule
	hitDeadline     time.Duration
	releaseDeadline time.Duration
}

type timedMeleeOutbox struct {
	*simraknet103.PacketOutbox
	abilityName            string
	actorObjectID          uint32
	actorPosition          sim.Position
	isUnreliableStopNeeded bool
	cooldown               time.Duration
}

type timedMeleeRoleResolver map[sim.Role]simraknet103.Binding

func (r timedMeleeRoleResolver) ResolveRole(_ context.Context, role sim.Role) (simraknet103.Binding, error) {
	binding, isFound := r[role]
	if !isFound {
		return simraknet103.Binding{}, fmt.Errorf("roleMissing: %s", role)
	}
	return binding, nil
}

func NewMeleeRun(input MeleeInput) (*MeleeRun, [][]byte, error) {
	if input.ActorObjectID == 0 || input.TargetObjectID == 0 || input.Damage <= 0 || input.TargetHitPoint < 0 {
		return nil, nil, errors.New("invalid Poison melee input")
	}
	resolver := timedMeleeRoleResolver{
		timedMeleeActorRole: {
			ObjectID: input.ActorObjectID, Position: input.ActorPosition, Team: input.ActorTeam,
		},
		timedMeleeTargetRole: {
			ObjectID: input.TargetObjectID, Position: input.TargetPosition, ManaPoints: input.TargetManaPoint,
		},
	}
	encoder, err := simraknet103.NewEncoder(resolver, input.SourceTime)
	if err != nil {
		return nil, nil, fmt.Errorf("encoderCreate: %w", err)
	}
	packetOutbox, err := simraknet103.NewPacketOutbox(encoder)
	if err != nil {
		return nil, nil, fmt.Errorf("outboxCreate: %w", err)
	}
	outbox := &timedMeleeOutbox{
		PacketOutbox:           packetOutbox,
		abilityName:            input.Ability.Name,
		actorObjectID:          input.ActorObjectID,
		actorPosition:          input.ActorPosition,
		isUnreliableStopNeeded: input.IsUnreliableStopNeeded,
		cooldown:               input.Ability.Cooldown,
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: timedMeleePhase,
		ActiveRoles: []sim.Role{
			timedMeleeActorRole,
			timedMeleeTargetRole,
		},
	}}, timedMeleePhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Locomotion: outbox, Cooldown: outbox, Damage: outbox, Presentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	simulator := director.Simulator()
	scope := simulator.Scope(timedMeleeActorRole)
	behavior, err := sim.StartTimedMelee(simulator, scope, sim.TimedMeleeInput{
		ActorRole: timedMeleeActorRole, TargetRole: timedMeleeTargetRole,
		AbilityName: input.Ability.Name, AnimationName: input.Ability.AnimationName,
		HitEffectName:  input.Ability.HitEffectName,
		TargetPosition: input.TargetPosition, TargetFacing: input.TargetFacing,
		HitDelay: input.Ability.HitDelay, ReleaseDelay: input.Ability.ReleaseDelay,
		HitDelays: input.HitDelays,
		Cooldown:  input.Ability.Cooldown, Damage: input.Damage, TargetHitPoint: input.TargetHitPoint,
		IsTargetValidAtHit: true,
		Provenance:         input.Ability.Provenance,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("behaviorStart: %w", err)
	}
	run := &MeleeRun{
		simulator: simulator, session: session, behavior: behavior, outbox: outbox,
		hitDeadline: input.Ability.HitDelay, releaseDeadline: input.Ability.ReleaseDelay,
	}
	err = session.DispatchPending(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("immediateDispatch: %w", err)
	}
	packets := outbox.Drain()
	if len(packets) == 0 {
		return nil, nil, errors.New("immediate packets missing")
	}
	return run, packets, nil
}

func (r *MeleeRun) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.simulator == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil timed melee run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	now := r.simulator.Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.Drain(), nil
}

func (r *MeleeRun) PrepareHit(
	isTargetValid bool, targetHitPoint float32, damage float32, isCritical bool,
	targetPosition sim.Position, targetFacing sim.Position,
) error {
	if r == nil || r.behavior == nil {
		return errors.New("nil timed melee run")
	}
	err := r.behavior.PrepareHit(isTargetValid, targetHitPoint, damage, isCritical, targetPosition, targetFacing)
	if err != nil {
		return fmt.Errorf("behaviorPrepare: %w", err)
	}
	return nil
}

func (r *MeleeRun) PrepareHitAt(
	hitIndex int, isTargetValid bool, targetHitPoint float32, damage float32, isCritical bool,
	targetPosition sim.Position, targetFacing sim.Position,
) error {
	if r == nil || r.behavior == nil {
		return errors.New("nil timed melee run")
	}
	err := r.behavior.PrepareHitAt(
		hitIndex, isTargetValid, targetHitPoint, damage, isCritical, targetPosition, targetFacing,
	)
	if err != nil {
		return fmt.Errorf("behaviorPrepare[%d]: %w", hitIndex, err)
	}
	return nil
}

func (r *MeleeRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.behavior != nil {
		r.behavior.Cancel()
	}
	if r.simulator != nil {
		r.simulator.InvalidateRole(timedMeleeActorRole)
		r.simulator.Stop()
	}
}

func (r *MeleeRun) HitDeadline() time.Duration {
	if r == nil {
		return 0
	}
	return r.hitDeadline
}

func (r *MeleeRun) ReleaseDeadline() time.Duration {
	if r == nil {
		return 0
	}
	return r.releaseDeadline
}

func (r *MeleeRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *MeleeRun) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (r *MeleeRun) IsCancelSet() bool {
	return r != nil && r.cancel != nil
}

func (o *timedMeleeOutbox) Stop(
	ctx context.Context, meta sim.EventMeta, intent sim.LocomotionStopIntent,
) error {
	if intent.Role != timedMeleeActorRole {
		return fmt.Errorf("locomotionIntent: %#v", intent)
	}
	err := o.Encode(ctx, meta, intent)
	if err != nil {
		return fmt.Errorf("stopEncode: %w", err)
	}
	if !o.isUnreliableStopNeeded {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.LocomotionUnreliableMessage{
		ObjectID: o.actorObjectID,
		GoalPosition: raknet.Vector3{
			X: o.actorPosition.X, Y: o.actorPosition.Y, Z: o.actorPosition.Z,
		},
	})
	if err != nil {
		return fmt.Errorf("stopUnreliableMarshal: %w", err)
	}
	o.Append(packet)
	return nil
}

func (o *timedMeleeOutbox) Start(
	_ context.Context, _ sim.EventMeta, intent sim.CooldownIntent,
) error {
	if intent.Role != timedMeleeActorRole || intent.AbilityName != o.abilityName ||
		intent.Duration != o.cooldown {
		return fmt.Errorf("cooldownIntent: %#v", intent)
	}
	return nil
}

func (o *timedMeleeOutbox) Damage(
	ctx context.Context, meta sim.EventMeta, intent sim.DamageIntent,
) error {
	if intent.ActorRole != timedMeleeActorRole || intent.TargetRole != timedMeleeTargetRole {
		return fmt.Errorf("damageIntent: %#v", intent)
	}
	return o.Encode(ctx, meta, intent)
}

func (o *timedMeleeOutbox) SetHitPoints(
	ctx context.Context, meta sim.EventMeta, intent sim.HitPointIntent,
) error {
	if intent.Role != timedMeleeTargetRole {
		return fmt.Errorf("hitPointIntent: %#v", intent)
	}
	return o.Encode(ctx, meta, intent)
}
