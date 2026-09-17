package game

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/darkspinnet/darkspin/server/util"
)

type TriggerVolumeShape uint32

const (
	TriggerSphere TriggerVolumeShape = iota
	TriggerBox
	TriggerCapsule
)

type TriggerActivationType uint32

const (
	TriggerEachObject TriggerActivationType = iota
	TriggerEachPlayer
	TriggerAnyObject
	TriggerAllPlayers
)

type vectorXML struct {
	X float32 `xml:"x"`
	Y float32 `xml:"y"`
	Z float32 `xml:"z"`
}

func (v vectorXML) value() Vec3 { return Vec3{X: v.X, Y: v.Y, Z: v.Z} }

type TriggerVolumeData struct {
	Shape                   TriggerVolumeShape    `xml:"shape"`
	Activation              TriggerActivationType `xml:"triggerActivationType"`
	Offset                  vectorXML             `xml:"offset"`
	OnEnter                 string                `xml:"onEnter"`
	OnExit                  string                `xml:"onExit"`
	OnStay                  string                `xml:"onStay"`
	LuaOnEnter              string                `xml:"luaCallbackOnEnter"`
	LuaOnExit               string                `xml:"luaCallbackOnExit"`
	LuaOnStay               string                `xml:"luaCallbackOnStay"`
	TimeToActivate          float32               `xml:"timeToActivate"`
	SphereRadius            float32               `xml:"sphereRadius"`
	BoxWidth                float32               `xml:"boxWidth"`
	BoxHeight               float32               `xml:"boxHeight"`
	BoxLength               float32               `xml:"boxLength"`
	CapsuleRadius           float32               `xml:"capsuleRadius"`
	CapsuleHeight           float32               `xml:"capsuleHeight"`
	AreObjectDimensionsUsed bool                  `xml:"useGameObjectDimensions"`
	IsKinematic             bool                  `xml:"isKinematic"`
	IsTimerPersistent       bool                  `xml:"persistentTimer"`
	IsOnceOnly              bool                  `xml:"triggerOnceOnly"`
}

func (t TriggerVolumeData) BoundingBox() BoundingBox {
	extent := Vec3{}
	switch t.Shape {
	case TriggerSphere:
		extent = Vec3{t.SphereRadius, t.SphereRadius, t.SphereRadius}
	case TriggerBox:
		extent = Vec3{t.BoxWidth, t.BoxHeight, t.BoxLength}
	case TriggerCapsule:
		extent = Vec3{t.CapsuleRadius, t.CapsuleHeight, t.CapsuleRadius}
	}
	return BoundingBox{Center: t.Offset.value(), Extent: extent}
}

type TeleporterData struct {
	DestinationMarkerID       uint32             `xml:"destinationMarkerId"`
	IsTriggerCreationDeferred bool               `xml:"deferTriggerCreation"`
	Trigger                   *TriggerVolumeData `xml:"triggerVolume"`
}
type MarkerInteractableData struct {
	Ability        string `xml:"interactableAbility"`
	StartEvent     string `xml:"startInteractEvent"`
	EndEvent       string `xml:"endInteractEvent"`
	OptionalEvent  string `xml:"optionalInteractEvent"`
	UsesAllowed    int32  `xml:"numUsesAllowed"`
	ChallengeValue int32  `xml:"challengeValue"`
}
type markerComponentsXML struct {
	Teleporter   *TeleporterData         `xml:"teleporter"`
	Interactable *MarkerInteractableData `xml:"interactable"`
}

type Marker struct {
	Name               string              `xml:"markerName"`
	ID                 uint32              `xml:"markerId"`
	NounName           string              `xml:"nounDef"`
	PositionXML        vectorXML           `xml:"pos"`
	RotationXML        vectorXML           `xml:"rotDegrees"`
	Scale              float32             `xml:"scale"`
	IsVisible          bool                `xml:"visible"`
	IsCollisionEnabled bool                `xml:"createWithCollision"`
	AssetID            uint64              `xml:"assetOverrideId"`
	TargetID           uint32              `xml:"targetMarkerId"`
	Components         markerComponentsXML `xml:"componentData"`
}

func (m Marker) NounID() uint32 { return util.HashID(m.NounName) }
func (m Marker) Position() Vec3 { return m.PositionXML.value() }
func (m Marker) Rotation() Vec3 { return m.RotationXML.value() }

type markerListXML struct {
	Entries []*Marker `xml:"entry"`
}
type markerSetXML struct {
	XMLName xml.Name      `xml:"markerset"`
	Markers markerListXML `xml:"markers"`
}
type Markerset struct {
	Name          string
	Markers       []*Marker
	markersByNoun map[uint32][]*Marker
}

func LoadMarkerset(path, name string) (*Markerset, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("markersetRead[%q]: %w", path, err)
	}
	var document markerSetXML
	err = unmarshalAssetXML(contents, &document)
	if err != nil {
		return nil, fmt.Errorf("markersetDecode: %w", err)
	}
	value := &Markerset{Name: name, Markers: document.Markers.Entries, markersByNoun: make(map[uint32][]*Marker)}
	for _, marker := range value.Markers {
		value.markersByNoun[marker.NounID()] = append(value.markersByNoun[marker.NounID()], marker)
	}
	return value, nil
}
func (m *Markerset) ByNoun(noun uint32) []*Marker {
	return append([]*Marker(nil), m.markersByNoun[noun]...)
}

type DirectorClass struct {
	NounName     string `xml:"mpNoun"`
	MinimumLevel int32  `xml:"minDifficulty"`
	MaximumLevel int32  `xml:"maxDifficulty"`
	IsHordeLegal bool   `xml:"hordeLegal"`
}
type directorListXML struct {
	Entries []DirectorClass `xml:"entry"`
}
type LevelConfig struct {
	Minions  directorListXML `xml:"minion"`
	Specials directorListXML `xml:"special"`
	Bosses   directorListXML `xml:"boss"`
	Agents   directorListXML `xml:"agent"`
	Captains directorListXML `xml:"captain"`
}
type levelMarkersetReference struct {
	Asset string `xml:"markersetAsset"`
}
type levelMarkersetList struct {
	Entries []levelMarkersetReference `xml:"entry"`
}
type levelXML struct {
	XMLName         xml.Name           `xml:"level"`
	Markersets      levelMarkersetList `xml:"markersets"`
	Config          LevelConfig        `xml:"levelConfig"`
	FirstTimeConfig LevelConfig        `xml:"firstTimeConfig"`
	PlanetConfig    string             `xml:"planetConfig"`
}

type Level struct {
	mu                                    sync.RWMutex
	Markersets                            map[uint32]*Markerset
	Config, FirstTimeConfig, PlanetConfig LevelConfig
}

func NewLevel() *Level { return &Level{Markersets: make(map[uint32]*Markerset)} }

func (l *Level) Load(dataRoot, levelName string) error {
	levelPath := filepath.Join(dataRoot, "level", levelName+".level.xml")
	contents, err := os.ReadFile(levelPath)
	if err != nil {
		return fmt.Errorf("levelRead[%q]: %w", levelPath, err)
	}
	var document levelXML
	err = unmarshalAssetXML(contents, &document)
	if err != nil {
		return fmt.Errorf("levelDecode: %w", err)
	}
	sets := make(map[uint32]*Markerset)
	for _, reference := range document.Markersets.Entries {
		if reference.Asset == "" {
			continue
		}
		set, loadErr := LoadMarkerset(filepath.Join(dataRoot, "markerset", reference.Asset+".xml"), reference.Asset)
		if loadErr != nil {
			return fmt.Errorf("markersetLoad[%q]: %w", reference.Asset, loadErr)
		}
		sets[util.HashID(reference.Asset)] = set
	}
	l.mu.Lock()
	l.Markersets = sets
	l.Config = document.Config
	l.FirstTimeConfig = document.FirstTimeConfig
	l.mu.Unlock()
	return nil
}
func (l *Level) Markerset(name string) *Markerset {
	l.mu.RLock()
	value := l.Markersets[util.HashID(name)]
	l.mu.RUnlock()
	return value
}
