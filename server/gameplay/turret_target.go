package gameplay

// Build-103 indexed Lua chunk 513, Abilities/0xCD4048E2.lua, defines
// nAbility_Turret_Laser.range={10,10}. TurretTrap's range={4,4} in chunk 328
// belongs to placement, not laser targeting. Neither value replaces aggro.
const autoTurretLaserRange = float32(10)

func autoTurretAttackRange(aggroRange, sourceFootprint, targetFootprint float32) float32 {
	radius := autoTurretLaserRange
	if aggroRange > 0 {
		radius = min(radius, aggroRange)
	}
	return radius + max(float32(0), sourceFootprint) + max(float32(0), targetFootprint)
}
