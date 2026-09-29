package main

// Fan curves, hysteresis, smoothing and the safety bounds every setting is
// held to.

import (
	"fmt"
	"math"
	"sort"
)

// Hard limits. The API refuses anything outside them; the control loop
// clamps again before every write.
const (
	HardMinPct       = 20 // no fan is ever driven below this
	DefaultMinPct    = 20
	DefaultCritCPU   = 85
	DefaultCritGPU   = 83
	MinCritC         = 60
	MaxCritCPU       = 95
	MaxCritGPU       = 90
	EmergencyClearC  = 5 // the emergency ends this far below the limit
	DefaultHystC     = 3
	MaxHystC         = 10
	MaxCurvePoints   = 8
	MinCurvePoints   = 2
	MinCurveTempC    = 20
	MaxCurveTempC    = 100
	RampUpPerTick    = 15 // % per control tick when a curve asks for more
	RampDownPerTick  = 3  // % per control tick when it asks for less
	StallTicks       = 3  // consecutive 0 RPM readings while driven = stalled
	WriteErrorsLimit = 3
)

type Point struct {
	T float64 `json:"t"` // °C
	P int     `json:"p"` // %
}

type Curve []Point

// validateCurve: 2-8 points, temperatures strictly rising within 20-100 °C,
// duty never falling as the temperature rises, every duty within
// [minPct, 100].
func validateCurve(c Curve, minPct int) error {
	if len(c) < MinCurvePoints || len(c) > MaxCurvePoints {
		return fmt.Errorf("a curve needs %d to %d points", MinCurvePoints, MaxCurvePoints)
	}
	for i, p := range c {
		if math.IsNaN(p.T) || p.T < MinCurveTempC || p.T > MaxCurveTempC {
			return fmt.Errorf("curve temperatures must be between %d and %d °C", MinCurveTempC, MaxCurveTempC)
		}
		if p.P < minPct || p.P > 100 {
			return fmt.Errorf("curve speeds must be between %d%% (this fan's minimum) and 100%%", minPct)
		}
		if i > 0 {
			if p.T <= c[i-1].T {
				return fmt.Errorf("curve temperatures must rise from point to point")
			}
			if p.P < c[i-1].P {
				return fmt.Errorf("a fan curve may not slow the fan down as it gets hotter")
			}
		}
	}
	return nil
}

// eval: linear interpolation, flat before the first and after the last
// point.
func (c Curve) eval(t float64) float64 {
	if len(c) == 0 {
		return 100
	}
	if t <= c[0].T {
		return float64(c[0].P)
	}
	last := c[len(c)-1]
	if t >= last.T {
		return float64(last.P)
	}
	i := sort.Search(len(c), func(i int) bool { return c[i].T >= t })
	a, b := c[i-1], c[i]
	f := (t - a.T) / (b.T - a.T)
	return float64(a.P) + f*float64(b.P-a.P)
}

// hysteresis: the temperature a curve is evaluated at follows a rise at
// once, but a fall only once it has dropped by h °C - so a temperature
// wobbling around a curve point doesn't make the fan hunt.
type hysteresis struct {
	eff  float64
	init bool
}

func (h *hysteresis) update(t, hyst float64) float64 {
	if !h.init || t >= h.eff || t <= h.eff-hyst {
		h.eff, h.init = t, true
	}
	return h.eff
}

// ramp moves from cur towards target by at most up/down per step; cur < 0
// (nothing written yet) jumps straight to target.
func ramp(cur, target, up, down int) int {
	if cur < 0 {
		return target
	}
	if target > cur+up {
		return cur + up
	}
	if target < cur-down {
		return cur - down
	}
	return target
}

func clampPct(p, minPct int) int {
	if minPct < HardMinPct {
		minPct = HardMinPct
	}
	if p < minPct {
		return minPct
	}
	if p > 100 {
		return 100
	}
	return p
}

// Presets. Curves are for the fan's temperature source: CPU package
// temperatures run hotter at idle than GPU core temperatures.
var presetNames = []string{"quiet", "balanced", "performance"}

func presetCurve(preset string, gpu bool) Curve {
	if gpu {
		switch preset {
		case "quiet":
			return Curve{{45, 30}, {60, 40}, {70, 55}, {77, 75}, {82, 100}}
		case "performance":
			return Curve{{35, 40}, {50, 55}, {60, 70}, {70, 90}, {78, 100}}
		default:
			return Curve{{40, 30}, {55, 45}, {65, 60}, {74, 80}, {80, 100}}
		}
	}
	switch preset {
	case "quiet":
		return Curve{{45, 25}, {60, 35}, {70, 50}, {78, 75}, {84, 100}}
	case "performance":
		return Curve{{35, 40}, {50, 55}, {60, 70}, {70, 90}, {78, 100}}
	default:
		return Curve{{40, 30}, {55, 40}, {65, 55}, {75, 80}, {82, 100}}
	}
}

// fitCurve lifts every point to at least minPct (a preset applied to a fan
// with a higher floor).
func fitCurve(c Curve, minPct int) Curve {
	out := make(Curve, len(c))
	for i, p := range c {
		if p.P < minPct {
			p.P = minPct
		}
		out[i] = p
	}
	return out
}
