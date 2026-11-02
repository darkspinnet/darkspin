package game

// AttributeMirrorTuning contains the build-103 base derivation and limit inputs.
// The mirror selector itself is fixed by native code, not configurable policy.
type AttributeMirrorTuning struct {
	GlobalBaseMaximums          [115]float32
	GlobalMaximums              [115]float32
	HealthPerStrength           float32
	ManaPerMind                 float32
	PhysicalDefensePerDexterity float32
	EnergyDefensePerMind        float32
	CriticalPerDexterity        float32
	IsKnown                     bool
}
