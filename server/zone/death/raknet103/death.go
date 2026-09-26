package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const enemyDeathPhase sim.Phase = "enemyDeath"
const enemyDeathActorRole sim.Role = "deathActor"
const enemyDeathTargetRole sim.Role = "deathTarget"

var ordinaryEnemyDeathDeadlines = []time.Duration{10 * time.Second, 15 * time.Second}
var criticalEnemyDeathDeadlines = []time.Duration{100 * time.Millisecond, 3100 * time.Millisecond}

const fixtureDeathDeleteDelay = time.Second

type Run struct {
	session   *sim.Session
	behavior  *sim.DeathBehavior
	outbox    *enemyDeathOutbox
	deadlines []time.Duration
	cancel    raknet.CancelSchedule
}

type Target struct {
	ObjectID               uint32
	Position               sim.Position
	OrdinaryDeathAnimation string
	IsAnimationSuppressed  bool
	CorpseFadeDelay        time.Duration
	GraphicsState          uint32
	ExplosionEffectName    string
	CreatureType           uint32
	IsCreatureTypeKnown    bool
	IsFixture              bool
	IsBoss                 bool
	IsRemnantRetained      bool
	IsCollisionRetained    bool
	DeleteDelay            time.Duration
}

type EffectPool interface {
	Allocate(uint32) (uint8, bool)
	Release(uint32, uint8) bool
	ReleaseObject(uint32)
}

type State struct {
	IsTargetCleared              bool
	IsImmobilized                bool
	IsLocomotionStopped          bool
	IsCorpseFading               bool
	IsMarkedForDeletion          bool
	IsPhysicsCollisionEnabled    bool
	IsNavigationCollisionEnabled bool
}

type Snapshot struct {
	SourceObjectID   uint32
	Target           Target
	Elapsed          time.Duration
	Deadlines        []time.Duration
	PendingTaskCount int
	State            State
}

type enemyDeathOutbox struct {
	actorObjectID                uint32
	target                       Target
	sourceTime                   uint64
	effectPool                   EffectPool
	effectSlot                   uint8
	isEffectSet                  bool
	isTargetCleared              bool
	isImmobilized                bool
	isLocomotionStopped          bool
	isCorpseFading               bool
	isMarkedForDeletion          bool
	isPhysicsCollisionEnabled    bool
	isNavigationCollisionEnabled bool
	packets                      [][]byte
	projections                  []zonenpc.DeathEvent
}

func NewRun(
	target Target, actorObjectID uint32, damage float32, isCritical bool, sourceTime uint64,
	effectPool EffectPool,
) (*Run, [][]byte, error) {
	if target.ObjectID == 0 || actorObjectID == 0 || damage <= 0 || effectPool == nil {
		return nil, nil, errors.New("invalid enemy death input")
	}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: enemyDeathPhase, ActiveRoles: []sim.Role{enemyDeathActorRole, enemyDeathTargetRole},
	}}, enemyDeathPhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	outbox := &enemyDeathOutbox{
		actorObjectID: actorObjectID, target: target, sourceTime: sourceTime, effectPool: effectPool,
		isPhysicsCollisionEnabled: true, isNavigationCollisionEnabled: true,
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Locomotion: outbox, Attribute: outbox, Damage: outbox, Death: outbox,
		Graphics: outbox, Physics: outbox, Presentation: outbox,
		PositionedPresentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	scope := director.Simulator().Scope(enemyDeathTargetRole)
	isFastCriticalDeath := isCritical && !target.IsFixture && !target.IsBoss
	deleteDelay := target.DeleteDelay
	if target.IsFixture && deleteDelay == 0 {
		deleteDelay = fixtureDeathDeleteDelay
	}
	// Explicit detonation/fixture deletion owns its complete deadline sequence.
	// The critical settle branch would otherwise wait beyond our sole deadline.
	isFastCriticalDeath = isFastCriticalDeath && deleteDelay == 0
	behavior, err := sim.StartDeathBehavior(director.Simulator(), scope, sim.DeathBehaviorInput{
		ActorRole: enemyDeathActorRole, TargetRole: enemyDeathTargetRole,
		Damage: damage, AnimationName: DeathAnimation(target.OrdinaryDeathAnimation),
		CorpseFadeDelay:      target.CorpseFadeDelay,
		GraphicsState:        target.GraphicsState,
		PositionedEffectName: target.ExplosionEffectName,
		Position:             target.Position,
		FadeEffectName:       FadeEffect(target.CreatureType, target.IsCreatureTypeKnown),
		IsCritical:           isCritical, IsFastCriticalDeath: isFastCriticalDeath,
		IsBoss:              target.IsBoss,
		IsRemnantRetained:   target.IsRemnantRetained,
		IsCollisionRetained: target.IsCollisionRetained,
		DeleteDelay:         deleteDelay,
		DamageProvenance: sim.Provenance{
			FunctionName: "sub_9E4D90", Confidence: sim.ConfidenceNative,
		},
		BehaviorProvenance: sim.Provenance{
			FunctionName: "Behavior_Death", Confidence: sim.ConfidenceBytecode,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("behaviorStart: %w", err)
	}
	err = session.DispatchPending(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("immediateDispatch: %w", err)
	}
	deadlines := ordinaryEnemyDeathDeadlines
	if deleteDelay > 0 {
		deadlines = []time.Duration{deleteDelay}
	} else if isFastCriticalDeath {
		deadlines = criticalEnemyDeathDeadlines
	} else if target.CorpseFadeDelay > ordinaryEnemyDeathDeadlines[0] {
		deadlines = []time.Duration{
			target.CorpseFadeDelay,
			target.CorpseFadeDelay +
				(ordinaryEnemyDeathDeadlines[1] - ordinaryEnemyDeathDeadlines[0]),
		}
	}
	run := &Run{
		session: session, behavior: behavior, outbox: outbox,
		deadlines: append([]time.Duration(nil), deadlines...),
	}
	return run, outbox.drain(), nil
}

func (r *Run) Deadlines() []time.Duration {
	if r == nil {
		return nil
	}
	return append([]time.Duration(nil), r.deadlines...)
}

func (r *Run) IsFinalDeadline(deadline time.Duration) bool {
	return r != nil && len(r.deadlines) != 0 && deadline == r.deadlines[len(r.deadlines)-1]
}

func DeathAnimation(animationName string) string {
	if animationName != "" {
		return animationName
	}
	return "gen_death_melee"
}

func FadeEffect(creatureType uint32, isCreatureTypeKnown bool) string {
	if !isCreatureTypeKnown {
		return "fadeaway_necro.ServerEventDef"
	}
	switch creatureType {
	case 0:
		return "fadeaway_cyber.ServerEventDef"
	case 1:
		return "fadeaway_spacetime.ServerEventDef"
	case 2:
		return "fadeaway_bio.ServerEventDef"
	case 3:
		return "fadeaway_plasma.ServerEventDef"
	default:
		return "fadeaway_necro.ServerEventDef"
	}
}

func (r *Run) PendingTaskCount() int {
	if r == nil || r.session == nil {
		return 0
	}
	return r.session.Director().Simulator().PendingTaskCount()
}

func (r *Run) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil enemy death run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	now := r.session.Director().Simulator().Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.drain(), nil
}

func (r *Run) AdvanceDeath(
	ctx context.Context, deadline time.Duration,
) ([]zonenpc.DeathEvent, error) {
	_, err := r.Advance(ctx, deadline)
	if err != nil {
		return nil, fmt.Errorf("deathAdvance: %w", err)
	}
	return r.outbox.drainProjection(), nil
}

func (r *Run) DrainProjection() []zonenpc.DeathEvent {
	if r == nil || r.outbox == nil {
		return nil
	}
	return r.outbox.drainProjection()
}

func (r *Run) State() State {
	if r == nil || r.outbox == nil {
		return State{}
	}
	return State{
		IsTargetCleared:              r.outbox.isTargetCleared,
		IsImmobilized:                r.outbox.isImmobilized,
		IsLocomotionStopped:          r.outbox.isLocomotionStopped,
		IsCorpseFading:               r.outbox.isCorpseFading,
		IsMarkedForDeletion:          r.outbox.isMarkedForDeletion,
		IsPhysicsCollisionEnabled:    r.outbox.isPhysicsCollisionEnabled,
		IsNavigationCollisionEnabled: r.outbox.isNavigationCollisionEnabled,
	}
}

func (r *Run) Snapshot() Snapshot {
	if r == nil || r.session == nil || r.outbox == nil {
		return Snapshot{}
	}
	return Snapshot{
		SourceObjectID:   r.outbox.actorObjectID,
		Target:           r.outbox.target,
		Elapsed:          r.session.Director().Simulator().Now(),
		Deadlines:        append([]time.Duration(nil), r.deadlines...),
		PendingTaskCount: r.PendingTaskCount(),
		State:            r.State(),
	}
}

func (r *Run) StopDeath() {
	r.Stop()
}

func (r *Run) Revive(ctx context.Context) ([][]byte, bool, error) {
	if r == nil || r.session == nil || r.behavior == nil || r.outbox == nil {
		return nil, false, errors.New("nil enemy death run")
	}
	if ctx == nil {
		return nil, false, errors.New("nil context")
	}
	isRevived, err := r.behavior.Revive()
	if err != nil {
		return nil, false, fmt.Errorf("behaviorRevive: %w", err)
	}
	if !isRevived {
		return nil, false, nil
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("cleanupDispatch: %w", err)
	}
	return r.outbox.drain(), true, nil
}

func (r *Run) ReviveDeath(
	ctx context.Context,
) ([]zonenpc.DeathEvent, bool, error) {
	if r == nil || r.session == nil || r.behavior == nil || r.outbox == nil {
		return nil, false, errors.New("nil enemy death run")
	}
	if ctx == nil {
		return nil, false, errors.New("nil context")
	}
	isRevived, err := r.behavior.Revive()
	if err != nil {
		return nil, false, fmt.Errorf("behaviorRevive: %w", err)
	}
	if !isRevived {
		return nil, false, nil
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("cleanupDispatch: %w", err)
	}
	return r.outbox.drainProjection(), true, nil
}

func (r *Run) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.behavior != nil {
		err := r.behavior.Cancel()
		if err == nil && r.session != nil {
			_ = r.session.DispatchPending(context.Background())
		}
	}
	if r.session != nil {
		r.session.Director().Simulator().Stop()
	}
}

func (r *Run) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *Run) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (o *enemyDeathOutbox) Damage(_ context.Context, _ sim.EventMeta, intent sim.DamageIntent) error {
	if intent.ActorRole != enemyDeathActorRole || intent.TargetRole != enemyDeathTargetRole ||
		!intent.IsKilling || intent.DeltaHealth <= 0 {
		return fmt.Errorf("damageIntent: %#v", intent)
	}
	flags := uint16(0x0005)
	if intent.IsCritical {
		flags |= 0x0008
	}
	packet, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, DeltaHealth: intent.DeltaHealth, TargetID: o.target.ObjectID,
		SourceID: o.actorObjectID, IntegerHPChange: intent.IntegerHitPointChange,
	})
	if err != nil {
		return fmt.Errorf("damageMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind:           zonenpc.DeathDamage,
		SourceObjectID: o.actorObjectID, TargetObjectID: o.target.ObjectID,
		Damage: intent.DeltaHealth, IntegerHitPoint: intent.IntegerHitPointChange,
		Position: game.Vec3{
			X: o.target.Position.X,
			Y: o.target.Position.Y,
			Z: o.target.Position.Z,
		},
		IsCritical: intent.IsCritical,
	})
	return nil
}

func (o *enemyDeathOutbox) SetHitPoints(_ context.Context, _ sim.EventMeta, intent sim.HitPointIntent) error {
	if intent.Role != enemyDeathTargetRole || intent.HitPoints != 0 {
		return fmt.Errorf("hitPointIntent: %#v", intent)
	}
	packet, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: o.target.ObjectID, HitPoints: intent.HitPoints, IsHitPointChanged: true,
	})
	if err != nil {
		return fmt.Errorf("hitPointMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathHitPoint, TargetObjectID: o.target.ObjectID,
		HitPoint: intent.HitPoints,
	})
	return nil
}

func (o *enemyDeathOutbox) Animate(_ context.Context, meta sim.EventMeta, intent sim.AnimationIntent) error {
	if o.target.IsAnimationSuppressed {
		return nil
	}
	if intent.Role != enemyDeathTargetRole || intent.AnimationName == "" {
		return fmt.Errorf("animationIntent: %#v", intent)
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: o.target.ObjectID, State: util.HashID(intent.AnimationName),
		Timestamp: o.sourceTime + uint64(meta.At/time.Millisecond), Scale: 1,
	})
	if err != nil {
		return fmt.Errorf("animationMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathAnimation, TargetObjectID: o.target.ObjectID,
		AnimationName: intent.AnimationName,
		Timestamp:     o.sourceTime + uint64(meta.At/time.Millisecond),
	})
	return nil
}

func (o *enemyDeathOutbox) SetGraphicsState(
	_ context.Context, meta sim.EventMeta, intent sim.GraphicsStateIntent,
) error {
	if intent.Role != enemyDeathTargetRole || intent.State == 0 {
		return fmt.Errorf("graphicsIntent: %#v", intent)
	}
	packet, err := raknet.MarshalApplication(raknet.SetObjectGFXStateMessage{
		ObjectID: o.target.ObjectID, State: intent.State,
		Timestamp: o.sourceTime + uint64(meta.At/time.Millisecond),
	})
	if err != nil {
		return fmt.Errorf("graphicsMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathGraphics, TargetObjectID: o.target.ObjectID,
		GraphicsState: intent.State,
		Timestamp:     o.sourceTime + uint64(meta.At/time.Millisecond),
	})
	return nil
}

func (o *enemyDeathOutbox) ApplyPositionedEffect(
	_ context.Context, _ sim.EventMeta, intent sim.PositionedEffectIntent,
) error {
	if intent.EffectName == "" {
		return errors.New("positioned effect empty")
	}
	packet, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: util.HashID(intent.EffectName),
		Position: raknet.Vector3{
			X: intent.Position.X, Y: intent.Position.Y, Z: intent.Position.Z,
		},
		Facing: raknet.Vector3{
			X: intent.Facing.X, Y: intent.Facing.Y, Z: intent.Facing.Z,
		},
	})
	if err != nil {
		return fmt.Errorf("positionedEffectMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind:           zonenpc.DeathPositionedEffect,
		TargetObjectID: o.target.ObjectID,
		EffectName:     intent.EffectName,
		Position: game.Vec3{
			X: intent.Position.X, Y: intent.Position.Y, Z: intent.Position.Z,
		},
	})
	return nil
}

func (o *enemyDeathOutbox) ApplyEffect(_ context.Context, _ sim.EventMeta, intent sim.EffectIntent) error {
	if intent.Role != enemyDeathTargetRole || intent.EffectName == "" {
		return fmt.Errorf("effectIntent: %#v", intent)
	}
	if intent.IsStopped {
		if !o.isEffectSet || !o.effectPool.Release(o.target.ObjectID, o.effectSlot) {
			return nil
		}
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: o.effectSlot + 1, IsRemovalRequested: true, ObjectID: o.target.ObjectID,
		})
		if err != nil {
			return fmt.Errorf("effectRemoveMarshal: %w", err)
		}
		o.isEffectSet = false
		o.packets = append(o.packets, packet)
		o.projections = append(o.projections, zonenpc.DeathEvent{
			Kind: zonenpc.DeathEffect, TargetObjectID: o.target.ObjectID,
			EffectSlot: o.effectSlot + 1, IsEffectStopped: true,
		})
		return nil
	}
	slot, isAllocated := o.effectPool.Allocate(o.target.ObjectID)
	if !isAllocated {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: slot + 1, IsForceAttached: true, Asset: util.HashID(intent.EffectName),
		ObjectID: o.target.ObjectID,
	})
	if err != nil {
		o.effectPool.Release(o.target.ObjectID, slot)
		return fmt.Errorf("effectMarshal: %w", err)
	}
	o.effectSlot = slot
	o.isEffectSet = true
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathEffect, TargetObjectID: o.target.ObjectID,
		EffectName: intent.EffectName, EffectSlot: slot + 1,
	})
	return nil
}

func (o *enemyDeathOutbox) ResetAnimation(
	_ context.Context, meta sim.EventMeta, intent sim.AnimationResetIntent,
) error {
	if intent.Role != enemyDeathTargetRole {
		return fmt.Errorf("animationResetIntent: %#v", intent)
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: o.target.ObjectID, Timestamp: o.sourceTime + uint64(meta.At/time.Millisecond), Scale: 1,
	})
	if err != nil {
		return fmt.Errorf("animationResetMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathAnimation, TargetObjectID: o.target.ObjectID,
		Timestamp: o.sourceTime + uint64(meta.At/time.Millisecond),
	})
	return nil
}

func (*enemyDeathOutbox) SetVisibility(context.Context, sim.EventMeta, sim.VisibilityIntent) error {
	return errors.New("visibility unsupported")
}

func (*enemyDeathOutbox) StartCinematic(context.Context, sim.EventMeta, sim.CinematicIntent) error {
	return errors.New("cinematic unsupported")
}

func (*enemyDeathOutbox) ShowDialogue(context.Context, sim.EventMeta, sim.DialogueIntent) error {
	return errors.New("dialogue unsupported")
}

func (*enemyDeathOutbox) Notify(context.Context, sim.EventMeta, sim.ClientEventIntent) error {
	return errors.New("client event unsupported")
}

func (o *enemyDeathOutbox) Stop(_ context.Context, _ sim.EventMeta, intent sim.LocomotionStopIntent) error {
	if intent.Role != enemyDeathTargetRole {
		return fmt.Errorf("locomotionIntent: %#v", intent)
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: o.target.ObjectID, GoalFlags: 0x20,
		GoalPosition: raknet.Vector3{
			X: o.target.Position.X, Y: o.target.Position.Y, Z: o.target.Position.Z,
		},
	})
	if err != nil {
		return fmt.Errorf("locomotionMarshal: %w", err)
	}
	o.isLocomotionStopped = true
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathMovementStop, TargetObjectID: o.target.ObjectID,
		Position: game.Vec3{
			X: o.target.Position.X, Y: o.target.Position.Y, Z: o.target.Position.Z,
		},
	})
	return nil
}

func (o *enemyDeathOutbox) ApplyModifier(
	_ context.Context, _ sim.EventMeta, intent sim.AttributeModifierIntent,
) error {
	if intent.Role != enemyDeathTargetRole || intent.AttributeKind != sim.AttributeImmobilized ||
		(intent.Amount != 1 && intent.Amount != -1) {
		return fmt.Errorf("attributeIntent: %#v", intent)
	}
	o.isImmobilized = intent.Amount > 0
	return nil
}

func (o *enemyDeathOutbox) ApplyDeathState(
	_ context.Context, _ sim.EventMeta, intent sim.DeathStateIntent,
) error {
	if intent.Role != enemyDeathTargetRole {
		return fmt.Errorf("deathIntent: %#v", intent)
	}
	switch intent.Action {
	case sim.DeathTargetCleared:
		packet, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
			ObjectID: o.target.ObjectID, IsTargetable: false,
		})
		if err != nil {
			return fmt.Errorf("targetClearMarshal: %w", err)
		}
		o.isTargetCleared = true
		o.packets = append(o.packets, packet)
		o.projections = append(o.projections, zonenpc.DeathEvent{
			Kind: zonenpc.DeathTargetable, TargetObjectID: o.target.ObjectID,
			IsTargetable: false,
		})
		return nil
	case sim.DeathTargetRestored:
		packet, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
			ObjectID: o.target.ObjectID, IsTargetable: true,
		})
		if err != nil {
			return fmt.Errorf("targetRestoreMarshal: %w", err)
		}
		o.isTargetCleared = false
		o.packets = append(o.packets, packet)
		o.projections = append(o.projections, zonenpc.DeathEvent{
			Kind: zonenpc.DeathTargetable, TargetObjectID: o.target.ObjectID,
			IsTargetable: true,
		})
		return nil
	case sim.DeathCorpseFading:
		o.isCorpseFading = true
		return nil
	case sim.DeathCorpseRestored:
		o.isCorpseFading = false
		return nil
	case sim.DeathWaitingForRevival:
		return nil
	case sim.DeathMarkedForDeletion:
		o.isMarkedForDeletion = true
	default:
		return fmt.Errorf("deathAction: %s", intent.Action)
	}
	o.effectPool.ReleaseObject(o.target.ObjectID)
	o.isEffectSet = false
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{o.target.ObjectID}})
	if err != nil {
		return fmt.Errorf("deleteMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathDelete, TargetObjectID: o.target.ObjectID,
	})
	return nil
}

func (o *enemyDeathOutbox) SetPhysicsState(
	_ context.Context, _ sim.EventMeta, intent sim.PhysicsStateIntent,
) error {
	if intent.Role != enemyDeathTargetRole {
		return fmt.Errorf("physicsIntent: %#v", intent)
	}
	if intent.CollisionKind == sim.CollisionStateNavigation {
		o.isNavigationCollisionEnabled = intent.IsCollisionEnabled
		return nil
	}
	if intent.CollisionKind != sim.CollisionStatePhysics {
		return fmt.Errorf("physicsKind: %s", intent.CollisionKind)
	}
	o.isPhysicsCollisionEnabled = intent.IsCollisionEnabled
	packet, err := raknet.MarshalApplication(raknet.ObjectCollisionUpdateMessage{
		ObjectID: o.target.ObjectID, IsCollisionEnabled: intent.IsCollisionEnabled,
	})
	if err != nil {
		return fmt.Errorf("collisionMarshal: %w", err)
	}
	o.packets = append(o.packets, packet)
	o.projections = append(o.projections, zonenpc.DeathEvent{
		Kind: zonenpc.DeathCollision, TargetObjectID: o.target.ObjectID,
		IsCollisionEnabled: intent.IsCollisionEnabled,
	})
	return nil
}

func (o *enemyDeathOutbox) drain() [][]byte {
	packets := o.packets
	o.packets = nil
	return packets
}

func (o *enemyDeathOutbox) drainProjection() []zonenpc.DeathEvent {
	if o == nil {
		return nil
	}
	projections := o.projections
	o.projections = nil
	return projections
}
