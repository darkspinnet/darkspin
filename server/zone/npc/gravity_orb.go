package npc

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

type GravityOrbProfile struct {
	Cast                   ActionProfile
	NounName               string
	BaseOrbCount           uint32
	AddedOrbPerPlayer      uint32
	MinimumSummonDistance  float32
	MaximumSummonDistance  float32
	MinimumSeparation      float32
	SummoningArc           float32
	Radius                 float32
	PlayerPullDistance     float32
	ProjectilePullDistance float32
	ActivationDelay        time.Duration
	InstabilityDelay       time.Duration
	Lifetime               time.Duration
	CooldownModifyPeriod   time.Duration
	CooldownModifyAmount   time.Duration
	LifecyclePollPeriod    time.Duration
	StartupEffectName      string
	StableEffectName       string
	UnstableEffectName     string
	FizzleEffectName       string
}

func ZelemGravityOrbProfile(nounName string) (GravityOrbProfile, bool) {
	playerPullDistance := float32(0)
	projectilePullDistance := float32(0)
	lifetime := time.Duration(0)
	switch strings.ToLower(nounName) {
	case "zelemboss.noun":
		playerPullDistance = 2
		projectilePullDistance = 200
		lifetime = 14 * time.Second
	case "zelemboss_2.noun":
		playerPullDistance = 2.5
		projectilePullDistance = 250
		lifetime = 12 * time.Second
	case "zelemboss_3.noun":
		playerPullDistance = 3
		projectilePullDistance = 300
		lifetime = 10 * time.Second
	default:
		return GravityOrbProfile{}, false
	}
	return GravityOrbProfile{
		Cast: ActionProfile{
			Family: ActionRetainedArea, AbilityName: "SummonGravityOrb",
			AnimationName: "zlm_boss_sp_attack1",
			HitDelay:      2200 * time.Millisecond, ReleaseDelay: 1500 * time.Millisecond,
			Cooldown: 10 * time.Second, Range: 50, MovementSpeed: 5,
		},
		NounName:     "ZelemGravityOrb.Noun",
		BaseOrbCount: 1, AddedOrbPerPlayer: 1,
		MinimumSummonDistance: 6, MaximumSummonDistance: 12,
		MinimumSeparation: 1.5, SummoningArc: math.Pi * 0.5,
		Radius: 8, PlayerPullDistance: playerPullDistance,
		ProjectilePullDistance: projectilePullDistance,
		ActivationDelay:        time.Second, InstabilityDelay: 10 * time.Second,
		Lifetime: lifetime, CooldownModifyPeriod: time.Second,
		CooldownModifyAmount: -time.Second, LifecyclePollPeriod: 500 * time.Millisecond,
		StartupEffectName:  "gravity_orb_startup.ServerEventDef",
		StableEffectName:   "gravity_orb_stable.ServerEventDef",
		UnstableEffectName: "gravity_orb_unstable.ServerEventDef",
		FizzleEffectName:   "gravity_orb_fizzle.ServerEventDef",
	}, true
}

func GravityOrbPushCooldown(
	cooldown time.Duration, orbElapsed time.Duration, orbCount uint32,
	profile GravityOrbProfile,
) time.Duration {
	if cooldown <= 0 || orbElapsed < 0 || orbCount == 0 ||
		profile.CooldownModifyPeriod <= 0 ||
		profile.CooldownModifyAmount >= 0 || profile.LifecyclePollPeriod <= 0 {
		return cooldown
	}
	firstTick := profile.ActivationDelay + profile.LifecyclePollPeriod
	lastTick := profile.InstabilityDelay + profile.LifecyclePollPeriod
	for firstTick <= orbElapsed {
		firstTick += profile.CooldownModifyPeriod
	}
	remaining := cooldown
	readyDelay := time.Duration(0)
	cursor := orbElapsed
	reduction := -profile.CooldownModifyAmount * time.Duration(orbCount)
	for tick := firstTick; tick <= lastTick; tick += profile.CooldownModifyPeriod {
		wait := tick - cursor
		if remaining <= wait {
			return readyDelay + remaining
		}
		remaining -= wait
		readyDelay += wait
		remaining -= reduction
		if remaining <= 0 {
			return readyDelay
		}
		cursor = tick
	}
	return readyDelay + remaining
}

func PlanGravityOrbPositions(
	source game.Vec3, target game.Vec3, count uint32,
	profile GravityOrbProfile,
) ([]game.Vec3, error) {
	if count == 0 || profile.MinimumSummonDistance <= 0 ||
		profile.MaximumSummonDistance < profile.MinimumSummonDistance ||
		profile.SummoningArc <= 0 || profile.MinimumSeparation <= 0 {
		return nil, errors.New("gravity orb placement invalid")
	}
	direction := math.Atan2(
		float64(target.Y-source.Y), float64(target.X-source.X),
	)
	positions := make([]game.Vec3, 0, count)
	for index := uint32(0); index < count; index++ {
		fraction := float64(0.5)
		if count > 1 {
			fraction = float64(index) / float64(count-1)
		}
		angle := direction - float64(profile.SummoningArc)*0.5 +
			float64(profile.SummoningArc)*fraction
		radius := profile.MinimumSummonDistance
		if index%2 == 1 {
			radius = profile.MaximumSummonDistance
		}
		position := game.Vec3{
			X: source.X + float32(math.Cos(angle))*radius,
			Y: source.Y + float32(math.Sin(angle))*radius,
			Z: source.Z,
		}
		for _, previous := range positions {
			deltaX := position.X - previous.X
			deltaY := position.Y - previous.Y
			if float32(math.Hypot(float64(deltaX), float64(deltaY))) <
				profile.MinimumSeparation {
				return nil, errors.New("gravity orb placement overlaps")
			}
		}
		positions = append(positions, position)
	}
	return positions, nil
}
