package creation

import "math"

// Shift the whole authored palette together so its color relationships survive
// while companions can use distinctly different paint, including tinted neutrals.
func (e *appearanceVariation) palette(colors [3]float64) [3]float64 {
	red, green, blue := colors[0], colors[1], colors[2]
	brightest := max(red, green, blue)
	darkest := min(red, green, blue)
	chroma := brightest - darkest
	hue, saturation := 0.0, 0.0
	if brightest > 0 {
		saturation = chroma / brightest
	}
	if chroma > 0 {
		switch brightest {
		case red:
			hue = (green - blue) / chroma
		case green:
			hue = (blue-red)/chroma + 2
		default:
			hue = (red-green)/chroma + 4
		}
		hue /= 6
	}
	hue = math.Mod(hue+e.hueShift+1, 1)
	saturation = min(1, max(0.20, saturation*(0.80+e.random.Float64()*0.45)))
	brightness := min(1, max(0.18, brightest*(0.75+e.random.Float64()*0.45)))
	chroma = brightness * saturation
	secondary := chroma * (1 - math.Abs(math.Mod(hue*6, 2)-1))
	base := brightness - chroma
	switch int(hue * 6) {
	case 0:
		colors = [3]float64{chroma, secondary, 0}
	case 1:
		colors = [3]float64{secondary, chroma, 0}
	case 2:
		colors = [3]float64{0, chroma, secondary}
	case 3:
		colors = [3]float64{0, secondary, chroma}
	case 4:
		colors = [3]float64{secondary, 0, chroma}
	default:
		colors = [3]float64{chroma, 0, secondary}
	}
	for index := range colors {
		colors[index] += base
	}
	return colors
}
