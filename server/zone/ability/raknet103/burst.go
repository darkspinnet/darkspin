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

const projectileBurstPhase sim.Phase = "projectileBurst"
const projectileBurstActorRole sim.Role = "burstActor"
const projectileBurstTargetRole sim.Role = "burstTarget"

type BurstInput struct {
	Ability               sim.AbilityDefinition
	ActorObjectID         uint32
	TargetObjectID        uint32
	ProjectileObjectIDs   []uint32
	ActorPosition         sim.Position
	TargetPosition        sim.Position
	ActorFacing           sim.Position
	FootprintRadius       float32
	Damage                float32
	TargetHitPoint        float32
	TargetManaPoint       float32
	ActorTeam             uint8
	SourceTime            uint64
	StartedAt             time.Time
	IsImpactFacingOmitted bool
}

type BurstRun struct {
	simulator           *sim.Simulator
	session             *sim.Session
	behavior            *sim.ProjectileBurstBehavior
	outbox              *projectileBurstOutbox
	projectileRoles     []sim.Role
	projectileObjectIDs []uint32
	targetObjectIDs     []uint32
	ability             sim.AbilityDefinition
	sourceObjectID      uint32
	startedAt           time.Time
	cancel              raknet.CancelSchedule
}

type projectileBurstOutbox struct {
	*simraknet103.PacketOutbox
}

type projectileBurstObjectOutbox struct {
	owner *projectileBurstOutbox
}

type projectileBurstRoleResolver map[sim.Role]simraknet103.Binding

func (r projectileBurstRoleResolver) ResolveRole(
	_ context.Context, role sim.Role,
) (simraknet103.Binding, error) {
	binding, isFound := r[role]
	if !isFound {
		return simraknet103.Binding{}, fmt.Errorf("roleBinding: %s", role)
	}
	return binding, nil
}

func NewBurstRun(input BurstInput) (*BurstRun, [][]byte, error) {
	if input.ActorObjectID == 0 || input.TargetObjectID == 0 || input.Damage <= 0 ||
		input.TargetHitPoint < 0 || len(input.ProjectileObjectIDs) == 0 ||
		len(input.ProjectileObjectIDs) != len(input.Ability.HitDelays) {
		return nil, nil, errors.New("invalid projectile burst input")
	}
	resolver := projectileBurstRoleResolver{
		projectileBurstActorRole: {
			ObjectID: input.ActorObjectID, Position: input.ActorPosition, Team: input.ActorTeam,
		},
		projectileBurstTargetRole: {
			ObjectID: input.TargetObjectID, Position: input.TargetPosition, ManaPoints: input.TargetManaPoint,
		},
	}
	projectileRoles := make([]sim.Role, len(input.ProjectileObjectIDs))
	activeRoles := []sim.Role{projectileBurstActorRole, projectileBurstTargetRole}
	for index, objectID := range input.ProjectileObjectIDs {
		if objectID == 0 {
			return nil, nil, fmt.Errorf("projectileID[%d]: zero", index)
		}
		role := sim.Role(fmt.Sprintf("burstProjectile%d", index+1))
		projectileRoles[index] = role
		activeRoles = append(activeRoles, role)
		resolver[role] = simraknet103.Binding{ObjectID: objectID, Orientation: raknet.Quaternion{W: 1}}
	}
	encoder, err := simraknet103.NewEncoder(resolver, input.SourceTime)
	if err != nil {
		return nil, nil, fmt.Errorf("encoderCreate: %w", err)
	}
	packetOutbox, err := simraknet103.NewPacketOutbox(encoder)
	if err != nil {
		return nil, nil, fmt.Errorf("outboxCreate: %w", err)
	}
	outbox := &projectileBurstOutbox{PacketOutbox: packetOutbox}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: projectileBurstPhase, ActiveRoles: activeRoles,
	}}, projectileBurstPhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Object:     projectileBurstObjectOutbox{owner: outbox},
		Locomotion: outbox, Projectile: outbox, Cooldown: outbox,
		Damage: outbox, Presentation: outbox, PositionedPresentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	simulator := director.Simulator()
	scope := simulator.Scope(projectileBurstActorRole)
	err = simulator.EmitScoped(sim.LocomotionStopIntent{
		Role: projectileBurstActorRole, Facing: input.ActorFacing,
		TargetPosition: input.TargetPosition, IsTurn: true,
	}, sim.Provenance{
		FunctionName: "AI attack admission", Confidence: sim.ConfidenceInferred,
	}, scope)
	if err != nil {
		return nil, nil, fmt.Errorf("facingEmit: %w", err)
	}
	behavior, err := sim.StartProjectileBurst(simulator, scope, sim.ProjectileBurstInput{
		ActorRole: projectileBurstActorRole, TargetRole: projectileBurstTargetRole,
		ProjectileRoles: projectileRoles, AbilityName: input.Ability.Name,
		AnimationName: input.Ability.AnimationName, ProjectileNoun: input.Ability.ProjectileNoun,
		MuzzleEffectName: input.Ability.MuzzleEffectName,
		TrailEffectName:  input.Ability.TrailEffectName,
		ImpactEffectName: input.Ability.ImpactEffectName, MissEffectName: input.Ability.MissEffectName,
		ActorPosition: input.ActorPosition, ActorFacing: input.ActorFacing,
		TargetPosition: input.TargetPosition, FootprintRadius: input.FootprintRadius,
		ShotDelays: input.Ability.HitDelays, ReleaseDelay: input.Ability.ReleaseDelay,
		Cooldown: input.Ability.Cooldown, ProjectileSpeed: input.Ability.Speed,
		ProjectileDistance: input.Ability.Distance, Damage: input.Damage,
		TargetHitPoint: input.TargetHitPoint, IsTargetValid: true,
		IsImpactFacingOmitted: input.IsImpactFacingOmitted,
		Provenance:            input.Ability.Provenance,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("behaviorStart: %w", err)
	}
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	targetObjectIDs := make([]uint32, len(input.ProjectileObjectIDs))
	for index := range targetObjectIDs {
		targetObjectIDs[index] = input.TargetObjectID
	}
	run := &BurstRun{
		simulator: simulator, session: session, behavior: behavior, outbox: outbox,
		projectileRoles:     projectileRoles,
		projectileObjectIDs: append([]uint32(nil), input.ProjectileObjectIDs...),
		targetObjectIDs:     targetObjectIDs,
		ability:             input.Ability, sourceObjectID: input.ActorObjectID,
		startedAt: startedAt,
	}
	err = session.DispatchPending(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("immediateDispatch: %w", err)
	}
	return run, outbox.Drain(), nil
}

func (r *BurstRun) PrepareLaunch(
	index int, actorPosition sim.Position, targetPosition sim.Position,
	actorFacing sim.Position, targetObjectID uint32, isTargetValid bool,
) error {
	if r == nil || r.behavior == nil {
		return errors.New("nil projectile burst run")
	}
	err := r.behavior.PrepareLaunch(
		index, actorPosition, targetPosition, actorFacing, isTargetValid,
	)
	if err != nil {
		return fmt.Errorf("behaviorPrepare[%d]: %w", index, err)
	}
	r.targetObjectIDs[index] = targetObjectID
	return nil
}

func (r *BurstRun) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.simulator == nil || r.session == nil || r.outbox == nil || ctx == nil {
		return nil, errors.New("invalid projectile burst run")
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

func (r *BurstRun) ResetActorAnimation(ctx context.Context) ([][]byte, error) {
	if r == nil || r.session == nil || r.behavior == nil || r.outbox == nil || ctx == nil {
		return nil, errors.New("invalid projectile burst animation reset")
	}
	err := r.behavior.ResetActorAnimation()
	if err != nil {
		return nil, fmt.Errorf("behaviorReset: %w", err)
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return nil, fmt.Errorf("resetDispatch: %w", err)
	}
	return r.outbox.Drain(), nil
}

func (r *BurstRun) AdvanceClock(deadline time.Duration) error {
	if r == nil || r.simulator == nil {
		return errors.New("nil projectile burst run")
	}
	err := r.simulator.AdvanceTo(deadline)
	if err != nil {
		return fmt.Errorf("clockAdvance: %w", err)
	}
	return nil
}

func (r *BurstRun) Definition() sim.AbilityDefinition {
	if r == nil {
		return sim.AbilityDefinition{}
	}
	return r.ability
}

func (r *BurstRun) Now() time.Duration {
	if r == nil || r.simulator == nil {
		return 0
	}
	return r.simulator.Now()
}

// Snapshots projects every active retained burst projectile at one wall-clock
// boundary without advancing the simulator.
func (r *BurstRun) Snapshots(now time.Time) []ProjectileSnapshot {
	if r == nil || r.behavior == nil || now.IsZero() || now.Before(r.startedAt) {
		return nil
	}
	shots := r.behavior.Snapshots(now.Sub(r.startedAt))
	snapshots := make([]ProjectileSnapshot, 0, len(shots))
	for _, shot := range shots {
		if !shot.IsActive || shot.Index < 0 || shot.Index >= len(r.projectileObjectIDs) {
			continue
		}
		remainingDuration := time.Duration(0)
		if r.ability.Speed > 0 && shot.RemainingDistance > 0 {
			remainingDuration = time.Duration(
				float64(shot.RemainingDistance) / float64(r.ability.Speed) *
					float64(time.Second),
			)
		}
		targetObjectID := uint32(0)
		if shot.IsTargetValid {
			targetObjectID = r.targetObjectIDs[shot.Index]
		}
		snapshots = append(snapshots, ProjectileSnapshot{
			ObjectID:       r.projectileObjectIDs[shot.Index],
			SourceObjectID: r.sourceObjectID, TargetObjectID: targetObjectID,
			AbilityName: r.ability.Name, ProjectileNoun: r.ability.ProjectileNoun,
			Position: shot.Position, TargetPosition: shot.TargetPosition,
			Direction: shot.Direction,
			Speed:     r.ability.Speed, RemainingDistance: shot.RemainingDistance,
			RemainingFlightDuration: remainingDuration, IsActive: true,
		})
	}
	return snapshots
}

func (r *BurstRun) ResolveCollision(
	ctx context.Context, index int, deadline time.Duration, isDirectHit bool,
	isTargetValid bool, targetHitPoint float32, damage float32, isCritical bool,
	impactPosition sim.Position, facing sim.Position,
) ([][]byte, error) {
	if r == nil || r.simulator == nil || r.session == nil || r.behavior == nil || r.outbox == nil || ctx == nil {
		return nil, errors.New("invalid projectile burst collision")
	}
	now := r.simulator.Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	err = r.behavior.ResolveCollision(
		index, isDirectHit, isTargetValid, targetHitPoint, damage, isCritical, impactPosition, facing,
	)
	if err != nil {
		return nil, fmt.Errorf("collisionResolve[%d]: %w", index, err)
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return nil, fmt.Errorf("collisionDispatch[%d]: %w", index, err)
	}
	return r.outbox.Drain(), nil
}

func (o projectileBurstObjectOutbox) Spawn(
	_ context.Context, _ sim.EventMeta, intent sim.SpawnIntent,
) error {
	return fmt.Errorf("spawnUnexpected: %#v", intent)
}

func (o projectileBurstObjectOutbox) Despawn(
	ctx context.Context, meta sim.EventMeta, intent sim.DespawnIntent,
) error {
	if o.owner == nil {
		return errors.New("burst outbox missing")
	}
	return o.owner.Encode(ctx, meta, intent)
}

func (o projectileBurstObjectOutbox) Move(
	_ context.Context, _ sim.EventMeta, intent sim.MovementIntent,
) error {
	return fmt.Errorf("movementUnexpected: %#v", intent)
}

func (o projectileBurstObjectOutbox) Teleport(
	_ context.Context, _ sim.EventMeta, intent sim.TeleportIntent,
) error {
	return fmt.Errorf("teleportUnexpected: %#v", intent)
}

func (o projectileBurstObjectOutbox) CreateTriggerVolume(
	_ context.Context, _ sim.EventMeta, intent sim.TriggerVolumeIntent,
) error {
	return fmt.Errorf("triggerUnexpected: %#v", intent)
}

func (o projectileBurstObjectOutbox) DestroyTriggerVolume(
	_ context.Context, _ sim.EventMeta, intent sim.DestroyTriggerVolumeIntent,
) error {
	return fmt.Errorf("triggerDestroyUnexpected: %#v", intent)
}

func (o *projectileBurstOutbox) Stop(
	ctx context.Context, meta sim.EventMeta, intent sim.LocomotionStopIntent,
) error {
	if intent.Role != projectileBurstActorRole {
		return fmt.Errorf("locomotionIntent: %#v", intent)
	}
	return o.Encode(ctx, meta, intent)
}

func (o *projectileBurstOutbox) Start(
	ctx context.Context, meta sim.EventMeta, intent sim.CooldownIntent,
) error {
	if intent.Role != projectileBurstActorRole {
		return fmt.Errorf("cooldownIntent: %#v", intent)
	}
	return o.Encode(ctx, meta, intent)
}

func (o *projectileBurstOutbox) Launch(
	ctx context.Context, meta sim.EventMeta, intent sim.ProjectileLaunchIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *projectileBurstOutbox) Move(
	ctx context.Context, meta sim.EventMeta, intent sim.ProjectileMotionIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *projectileBurstOutbox) Impact(
	ctx context.Context, meta sim.EventMeta, intent sim.ProjectileImpactIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *projectileBurstOutbox) Damage(
	ctx context.Context, meta sim.EventMeta, intent sim.DamageIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *projectileBurstOutbox) SetHitPoints(
	ctx context.Context, meta sim.EventMeta, intent sim.HitPointIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (r *BurstRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.behavior != nil {
		_ = r.behavior.Cancel()
	}
	if r.simulator != nil {
		for _, role := range r.projectileRoles {
			r.simulator.InvalidateRole(role)
		}
		r.simulator.Stop()
	}
}

func (r *BurstRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}
