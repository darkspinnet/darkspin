package security

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

const (
	RouteCount             = 7
	TriggerRadius          = float32(2)
	DefaultFootprintRadius = float32(0.8)
	ContactRadius          = TriggerRadius + DefaultFootprintRadius
	ThreatRadius           = float32(20)
	ActivationRadius       = float32(12)
)

var firstSource = game.Vec3{X: -141.3413, Y: 83.7317, Z: 0.0340}
var firstDestination = game.Vec3{X: 555.5919, Y: -31.4069, Z: -0.4774}
var bossSource = game.Vec3{X: 184.2345, Y: 558.9578, Z: 10.2027}
var bossDestination = game.Vec3{X: 928.1883, Y: 668.9045, Z: 0.0880}

var route = []Teleport{
	{Source: firstSource, Destination: firstDestination},
	{Source: game.Vec3{X: 556.1375, Y: -38.2726, Z: 0.0175}, Destination: game.Vec3{X: -143.8270, Y: 77.3935, Z: -0.1422}},
	{Source: game.Vec3{X: 601.7911, Y: 42.2988, Z: 33.0519}, Destination: game.Vec3{X: -621.0203, Y: 650.6170, Z: 0.6042}},
	{Source: game.Vec3{X: -625.0099, Y: 655.0704, Z: 0.1358}, Destination: game.Vec3{X: 597.2678, Y: 44.5016, Z: 33.0999}},
	{Source: game.Vec3{X: -543.5760, Y: 546.1642, Z: 0.1581}, Destination: game.Vec3{X: 191.1970, Y: 736.2937, Z: 0.0443}},
	{Source: game.Vec3{X: 191.8558, Y: 742.7648, Z: 0.0523}, Destination: game.Vec3{X: -541.5687, Y: 553.5535, Z: 0.1342}},
	{Source: bossSource, Destination: bossDestination, IsBoss: true},
}

type Teleport struct {
	Source      game.Vec3
	Destination game.Vec3
	IsBoss      bool
}

type Quaternion struct {
	X float32
	Y float32
	Z float32
	W float32
}

type StatePublication struct {
	ObjectID    uint32
	Position    game.Vec3
	EffectNames []string
}

type TeleportPublication struct {
	ObjectID         uint32
	Source           game.Vec3
	Destination      game.Vec3
	Orientation      Quaternion
	Timestamp        uint64
	ActiveEffectName string
}

type Threat struct {
	Position    game.Vec3
	Footprint   float32
	IsDefeated  bool
	IsFixture   bool
	IsInvisible bool
}

func CompileTeleport(
	modifier sim.LuaBytecode, teleport Teleport,
) (sim.Program, error) {
	destination := sim.Position{
		X: teleport.Destination.X,
		Y: teleport.Destination.Y,
		Z: teleport.Destination.Z,
	}
	program, err := sim.CompileLuaModifier(sim.LuaModifierInput{
		Root: modifier, Role: zoneteleport.EntrantRole, Destination: destination,
		Callback: "Activate",
	})
	if err != nil {
		return sim.Program{}, fmt.Errorf("teleportCompile: %w", err)
	}
	return program, nil
}

// Routes returns every recovered traversal edge for a level.
// Levels without an implemented route return no edges.
func Routes(levelName string) []Teleport {
	if !strings.EqualFold(levelName, game.InitialChainLevel) {
		return nil
	}
	routes := make([]Teleport, len(route))
	copy(routes, route)
	return routes
}

func Route(routeIndex int) (Teleport, bool) {
	if routeIndex < 0 || routeIndex >= len(route) {
		return Teleport{}, false
	}
	return route[routeIndex], true
}

func CountRoutes() int {
	return len(route)
}

func PlanObjectIDs(firstObjectID uint32) ([RouteCount]uint32, uint32, error) {
	objectID := [RouteCount]uint32{}
	if firstObjectID == 0 ||
		firstObjectID+uint32(len(objectID)) > zoneobject.ProjectileIDStart {
		return objectID, firstObjectID, errors.New("security object IDs invalid")
	}
	nextObjectID := firstObjectID
	for index := range objectID {
		objectID[index] = nextObjectID
		nextObjectID++
	}
	return objectID, nextObjectID, nil
}

func PlanFirstTeleport(
	previous, current game.Vec3, threats []Threat, isUsed bool,
) (Teleport, bool, error) {
	if isUsed {
		return Teleport{}, false, nil
	}
	return PlanTeleport(previous, current, threats, 0)
}

func PlanTeleport(
	previous, current game.Vec3, threats []Threat, routeIndex int,
) (Teleport, bool, error) {
	if !zonepopulation.IsFinitePosition(previous) ||
		!zonepopulation.IsFinitePosition(current) {
		return Teleport{}, false, errors.New("security position invalid")
	}
	teleport, isFound := Route(routeIndex)
	if !isFound {
		return Teleport{}, false, nil
	}
	contactDistance := segmentDistanceSquared(previous, current, teleport.Source)
	if contactDistance > ContactRadius*ContactRadius {
		return Teleport{}, false, nil
	}
	if HasThreat(teleport, threats) {
		return Teleport{}, false, nil
	}
	return teleport, true, nil
}

func PlanActivation(
	previous, current game.Vec3, threats []Threat, routeIndex int,
) (Teleport, bool, error) {
	if !zonepopulation.IsFinitePosition(previous) ||
		!zonepopulation.IsFinitePosition(current) {
		return Teleport{}, false, errors.New("security activation position invalid")
	}
	teleport, isFound := Route(routeIndex)
	if !isFound {
		return Teleport{}, false, nil
	}
	contactDistance := segmentDistanceSquared(previous, current, teleport.Source)
	if contactDistance > ActivationRadius*ActivationRadius {
		return Teleport{}, false, nil
	}
	if HasThreat(teleport, threats) {
		return Teleport{}, false, nil
	}
	return teleport, true, nil
}

func HasThreat(teleport Teleport, threats []Threat) bool {
	for _, threat := range threats {
		if threat.IsDefeated || threat.IsFixture {
			continue
		}
		deltaX := threat.Position.X - teleport.Source.X
		deltaY := threat.Position.Y - teleport.Source.Y
		deltaZ := threat.Position.Z - teleport.Source.Z
		threatRadius := ThreatRadius + max(float32(0), threat.Footprint)
		if deltaX*deltaX+deltaY*deltaY+deltaZ*deltaZ < threatRadius*threatRadius {
			return true
		}
	}
	return false
}

func PublishState(
	objectID uint32, teleport Teleport, isActive bool, isPowerUp bool,
) (StatePublication, error) {
	if objectID == 0 || !zonepopulation.IsFinitePosition(teleport.Source) {
		return StatePublication{}, errors.New("security activation invalid")
	}
	effectName := effectPrefix(teleport) + "_inactive.ServerEventDef"
	if isActive {
		effectName = effectPrefix(teleport) + ".ServerEventDef"
	}
	effectNames := make([]string, 0, 2)
	if isPowerUp {
		effectNames = append(
			effectNames, effectPrefix(teleport)+"_powerup.ServerEventDef",
		)
	}
	effectNames = append(effectNames, effectName)
	return StatePublication{
		ObjectID: objectID, Position: teleport.Source, EffectNames: effectNames,
	}, nil
}

func PublishInitialState(objectID [RouteCount]uint32) ([]StatePublication, error) {
	if objectID == ([RouteCount]uint32{}) {
		return nil, nil
	}
	if len(route) != len(objectID) {
		return nil, errors.New("security route size invalid")
	}
	publications := make([]StatePublication, 0, len(objectID))
	for index, currentObjectID := range objectID {
		publication, err := PublishState(
			currentObjectID, route[index], false, false,
		)
		if err != nil {
			return nil, fmt.Errorf("securityInitial[%d]: %w", index, err)
		}
		publications = append(publications, publication)
	}
	return publications, nil
}

func PublishTeleport(
	objectID uint32, teleport Teleport, orientation Quaternion, timestamp uint64,
) (TeleportPublication, error) {
	if objectID == 0 || !zonepopulation.IsFinitePosition(teleport.Source) ||
		!zonepopulation.IsFinitePosition(teleport.Destination) {
		return TeleportPublication{}, errors.New("security teleport invalid")
	}
	return TeleportPublication{
		ObjectID: objectID, Source: teleport.Source, Destination: teleport.Destination,
		Orientation: orientation, Timestamp: timestamp,
		ActiveEffectName: effectPrefix(teleport) + ".ServerEventDef",
	}, nil
}

func effectPrefix(teleport Teleport) string {
	if teleport.IsBoss {
		return "zelem_boss_teleporter"
	}
	return "zelem_teleporter"
}

func segmentDistanceSquared(start game.Vec3, end game.Vec3, point game.Vec3) float32 {
	delta := game.Vec3{X: end.X - start.X, Y: end.Y - start.Y, Z: end.Z - start.Z}
	lengthSquared := delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z
	if lengthSquared == 0 {
		x := start.X - point.X
		y := start.Y - point.Y
		z := start.Z - point.Z
		return x*x + y*y + z*z
	}
	projection := ((point.X-start.X)*delta.X + (point.Y-start.Y)*delta.Y +
		(point.Z-start.Z)*delta.Z) / lengthSquared
	projection = min(float32(1), max(float32(0), projection))
	x := start.X + projection*delta.X - point.X
	y := start.Y + projection*delta.Y - point.Y
	z := start.Z + projection*delta.Z - point.Z
	return x*x + y*y + z*z
}
