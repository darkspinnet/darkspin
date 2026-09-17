package game

import (
	"math"
	"sync"
	"time"
)

type Quaternion struct{ X, Y, Z, W float32 }
type MovementType uint8
type StealthType uint8
type CorpseState uint32

const (
	CorpseAlive CorpseState = iota
	CorpseUnknown
	CorpseVaporized
)

type CombatantData struct {
	CorpseState        CorpseState
	HitPoints          float32
	ManaPoints         float32
	IsCorpseFadingAway bool
}

type InteractableData struct {
	Ability                uint32
	TimesUsed, UsesAllowed int32
}

func (d InteractableData) HasUsesLeft() bool { return d.UsesAllowed < 0 || d.TimesUsed < d.UsesAllowed }

type LootData struct {
	ID, InstanceID                                                uint64
	RigblockAsset, SuffixAsset, PrefixAsset, SecondaryPrefixAsset uint32
	CrystalLevel, ItemLevel, Rarity                               int32
	DNAAmount                                                     float32
}

type AgentBlackboard struct {
	TargetID, NumAttackers   uint32
	StealthType              StealthType
	IsInCombat, IsTargetable bool
}

type Modifier struct {
	ID         uint32
	Timestamp  time.Time
	Duration   time.Duration
	StackCount uint8
	IsBound    bool
}

type EffectList struct {
	mu            sync.Mutex
	slotsByEffect map[uint32]uint8
	openSlots     []uint8
	next          uint8
}

func (e *EffectList) Add(effect uint32) uint8 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.slotsByEffect == nil {
		e.slotsByEffect = make(map[uint32]uint8)
	}
	if index, isFound := e.slotsByEffect[effect]; isFound {
		return index
	}
	var index uint8
	if len(e.openSlots) > 0 {
		index = e.openSlots[len(e.openSlots)-1]
		e.openSlots = e.openSlots[:len(e.openSlots)-1]
	} else {
		index = e.next
		e.next++
	}
	e.slotsByEffect[effect] = index
	return index
}

func (e *EffectList) Remove(effect uint32) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	index, isFound := e.slotsByEffect[effect]
	if !isFound {
		return false
	}
	delete(e.slotsByEffect, effect)
	e.openSlots = append(e.openSlots, index)
	return true
}

type LobParameters struct {
	UpDirection, PlaneDirection                           Vec3
	BounceNumber                                          int32
	UpQuadratic, UpLinear, PlaneLinear, BounceRestitution float32
	IsGroundCollisionOnly, AreCreatureBouncesStopped      bool
}

type ProjectileParameters struct {
	Direction                                                                                                         Vec3
	JinkInfo                                                                                                          uint32
	Speed, Acceleration, Range, SpinRate, HomingDelay, TurnRate, TurnAcceleration, Eccentricity, CombatantSweepHeight float32
	Flags                                                                                                             uint8
	IsPiercing, IsGroundCollisionIgnored, IsCreatureCollisionIgnored                                                  bool
}

type Locomotion struct {
	Projectile                                                                       ProjectileParameters
	Lob                                                                              LobParameters
	GoalPosition, PartialGoalPosition, Facing, ExternalLinearVelocity, ExternalForce Vec3
	TargetPosition, ExpectedGeoCollision, InitialDirection, Offset                   Vec3
	GoalFlags, TargetID                                                              uint32
	AllowedStopDistance, DesiredStopDistance                                         float32
	IsCreatureCollisionDetected, IsCollisionDetected                                 bool
}

func (l *Locomotion) SetGoalPosition(position Vec3, distance float32) {
	l.GoalPosition = position
	l.DesiredStopDistance = distance
	l.GoalFlags |= 1
}
func (l *Locomotion) Stop() {
	l.GoalFlags = 0x20
	l.ExternalLinearVelocity = Vec3{}
	l.ExternalForce = Vec3{}
}
func (l *Locomotion) ClearTarget() { l.TargetID = 0; l.GoalFlags &^= 0x40 }
func (l *Locomotion) ApplyExternalVelocity(value Vec3) {
	l.ExternalLinearVelocity = value
	l.GoalFlags |= 0x08
}

// Object is the authoritative replicated gameplay object.
type Object struct {
	mu                                                   sync.RWMutex
	ID, NounID                                           uint32
	AssetID                                              uint64
	Position, LinearVelocity, AngularVelocity, Extent    Vec3
	Orientation                                          Quaternion
	Scale                                                float32
	Team, PlayerIndex                                    uint8
	MovementType                                         MovementType
	OwnerID, InputSyncStamp, InteractableState, MarkerID uint32
	IsVisible, IsCollisionEnabled, IsPlayerControlled    bool
	IsMarkedForDeletion, IsDirty                         bool
	Attributes                                           *Attributes
	Combatant                                            *CombatantData
	Interactable                                         *InteractableData
	Loot                                                 *LootData
	Blackboard                                           *AgentBlackboard
	Locomotion                                           *Locomotion
	Effects                                              EffectList
	Modifiers                                            map[uint32]*Modifier
}

func NewObject(id, noun uint32) *Object {
	return &Object{ID: id, NounID: noun, Scale: 1, IsVisible: true, IsCollisionEnabled: true, IsDirty: true, Attributes: NewAttributes(), Modifiers: make(map[uint32]*Modifier)}
}

func (o *Object) BoundingBox() BoundingBox {
	o.mu.RLock()
	box := BoundingBox{Center: o.Position, Extent: o.Extent.Scale(o.Scale)}
	o.mu.RUnlock()
	return box
}
func (o *Object) CenterPoint() Vec3 {
	o.mu.RLock()
	value := o.Position
	value.Y += o.Extent.Y * o.Scale
	o.mu.RUnlock()
	return value
}
func (o *Object) FootprintRadius() float32 {
	o.mu.RLock()
	value := max(o.Extent.X, o.Extent.Z) * o.Scale
	o.mu.RUnlock()
	return value
}
func (o *Object) SetPosition(position Vec3) {
	o.mu.Lock()
	o.Position = position
	o.IsDirty = true
	o.mu.Unlock()
}

func (o *Object) SetHealth(value float32) {
	o.mu.Lock()
	if o.Combatant == nil {
		o.Combatant = &CombatantData{}
	}
	maximum := o.Attributes.Value(AttributeMaxHealth)
	o.Combatant.HitPoints = min(max(value, 0), maximum)
	o.IsDirty = true
	o.mu.Unlock()
}
func (o *Object) Health() float32 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.Combatant == nil {
		return 0
	}
	return o.Combatant.HitPoints
}
func (o *Object) SetMana(value float32) {
	o.mu.Lock()
	if o.Combatant == nil {
		o.Combatant = &CombatantData{}
	}
	maximum := o.Attributes.Value(AttributeMaxMana)
	o.Combatant.ManaPoints = min(max(value, 0), maximum)
	o.IsDirty = true
	o.mu.Unlock()
}

// ObjectManager owns IDs, spatial queries, and deferred deletion.
type ObjectManager struct {
	mu      sync.RWMutex
	nextID  uint32
	objects map[uint32]*Object
	openIDs []uint32
}

func NewObjectManager() *ObjectManager {
	return &ObjectManager{nextID: 1, objects: make(map[uint32]*Object)}
}
func (m *ObjectManager) Create(noun uint32) *Object {
	m.mu.Lock()
	var id uint32
	if len(m.openIDs) > 0 {
		id = m.openIDs[len(m.openIDs)-1]
		m.openIDs = m.openIDs[:len(m.openIDs)-1]
	} else {
		id = m.nextID
		m.nextID++
	}
	object := NewObject(id, noun)
	m.objects[id] = object
	m.mu.Unlock()
	return object
}
func (m *ObjectManager) Get(id uint32) *Object {
	m.mu.RLock()
	object := m.objects[id]
	m.mu.RUnlock()
	return object
}
func (m *ObjectManager) Delete(id uint32) {
	m.mu.Lock()
	if _, isFound := m.objects[id]; isFound {
		delete(m.objects, id)
		m.openIDs = append(m.openIDs, id)
	}
	m.mu.Unlock()
}
func (m *ObjectManager) Objects() []*Object {
	m.mu.RLock()
	values := make([]*Object, 0, len(m.objects))
	for _, object := range m.objects {
		values = append(values, object)
	}
	m.mu.RUnlock()
	return values
}
func (m *ObjectManager) InRegion(region BoundingBox) []*Object {
	objects := m.Objects()
	result := make([]*Object, 0)
	for _, object := range objects {
		if region.IntersectsBox(object.BoundingBox()) {
			result = append(result, object)
		}
	}
	return result
}
func (m *ObjectManager) InRadius(region BoundingSphere) []*Object {
	objects := m.Objects()
	result := make([]*Object, 0)
	for _, object := range objects {
		if region.IntersectsBox(object.BoundingBox()) {
			result = append(result, object)
		}
	}
	return result
}
func (m *ObjectManager) Update(delta time.Duration) {
	seconds := float32(delta.Seconds())
	for _, object := range m.Objects() {
		object.mu.Lock()
		if object.Locomotion != nil {
			velocity := object.LinearVelocity.Add(object.Locomotion.ExternalLinearVelocity)
			object.Position = object.Position.Add(velocity.Scale(seconds))
			if object.Locomotion.GoalFlags&1 != 0 {
				deltaPosition := object.Locomotion.GoalPosition.Sub(object.Position)
				if deltaPosition.Length() <= object.Locomotion.DesiredStopDistance {
					object.Locomotion.Stop()
				}
			}
		}
		object.mu.Unlock()
	}
}

func distance(a, b Vec3) float32 { return float32(math.Sqrt(float64(a.Sub(b).LengthSquared()))) }
