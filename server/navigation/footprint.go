package navigation

// ActorNavigation is selected when an actor's navigation object is created.
// It remains independent of later graphics-scale and combat-radius changes.
type ActorNavigation struct {
	Mode      uint8
	Radius    float32
	PlanLayer uint8
	IsPresent bool
}

// SelectDropLayer follows sub_9EDCD0: the penultimate authored tuning row,
// or the only row when there is just one. Pickup projection is not actor-sized.
func (e *Mesh) SelectDropLayer() (uint8, bool) {
	if e == nil || len(e.tunings) == 0 {
		return 0, false
	}
	index := max(0, len(e.tunings)-2)
	layer := e.tunings[index].Ordinal
	layerInfo, isFound := e.LayerInfo(layer)
	return layer, isFound && layerInfo.PlanLayer == layer
}

// SelectFootprintLayer keeps the radius-only contract explicit at callers.
// SelectLayer owns layer ordering and the tuning-table selection policy.
func (e *Mesh) SelectFootprintLayer(radius float32, modes ...uint8) (uint8, bool) {
	mode := uint8(0)
	if len(modes) != 0 {
		mode = modes[0]
	}
	return e.SelectLayerForMode(radius, mode)
}

func (e *Mesh) ActorLayer(radius float32, actors ...ActorNavigation) (uint8, bool) {
	if len(actors) != 0 && actors[0].IsPresent {
		layer := actors[0].PlanLayer
		layerInfo, isFound := e.LayerInfo(layer)
		if isFound && layerInfo.PlanLayer != layer {
			return layer, false
		}
		return layer, isFound
	}
	if len(actors) != 0 {
		return e.SelectFootprintLayer(radius, actors[0].Mode)
	}
	return e.SelectFootprintLayer(radius)
}
