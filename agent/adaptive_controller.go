package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AdaptiveThresholdController adjusts thresholds with PID-like feedback.
// Error source is "bad event" ratio in [0,1], target is desired bad ratio.
type AdaptiveThresholdController struct {
	mu         sync.Mutex
	quality    int
	confidence int
	targetBad  float64
	kp         float64
	ki         float64
	kd         float64
	minFloor   int
	maxFloor   int
	integral   float64
	prevErr    float64
	onlineTune bool
	statePath  string
}

type AdaptiveControllerParams struct {
	TargetBadRatio float64
	Kp             float64
	Ki             float64
	Kd             float64
	MinFloor       int
	MaxFloor       int
	OnlineTuning   bool
	StatePath      string
}

type adaptiveControllerState struct {
	TargetBadRatio float64   `json:"targetBadRatio"`
	Kp             float64   `json:"kp"`
	Ki             float64   `json:"ki"`
	Kd             float64   `json:"kd"`
	QualityFloor   int       `json:"qualityFloor"`
	ConfidenceFloor int      `json:"confidenceFloor"`
	UpdatedAt      string    `json:"updatedAt"`
}

func NewAdaptiveThresholdController(quality, confidence int, params AdaptiveControllerParams) *AdaptiveThresholdController {
	c := &AdaptiveThresholdController{
		quality:    quality,
		confidence: confidence,
		targetBad:  params.TargetBadRatio,
		kp:         params.Kp,
		ki:         params.Ki,
		kd:         params.Kd,
		minFloor:   params.MinFloor,
		maxFloor:   params.MaxFloor,
		onlineTune: params.OnlineTuning,
		statePath:  params.StatePath,
	}
	if c.minFloor <= 0 {
		c.minFloor = 40
	}
	if c.maxFloor <= 0 {
		c.maxFloor = 95
	}
	if c.targetBad <= 0 {
		c.targetBad = 0.08
	}
	if c.kp == 0 {
		c.kp = 16
	}
	if c.ki == 0 {
		c.ki = 4
	}
	if c.kd == 0 {
		c.kd = 6
	}
	_ = c.loadState()
	return c
}

func (c *AdaptiveThresholdController) loadState() error {
	if c == nil || c.statePath == "" {
		return nil
	}
	b, err := os.ReadFile(c.statePath)
	if err != nil {
		return nil
	}
	var st adaptiveControllerState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil
	}
	if st.QualityFloor > 0 {
		c.quality = st.QualityFloor
	}
	if st.ConfidenceFloor > 0 {
		c.confidence = st.ConfidenceFloor
	}
	if st.TargetBadRatio > 0 {
		c.targetBad = st.TargetBadRatio
	}
	if st.Kp != 0 {
		c.kp = st.Kp
	}
	if st.Ki != 0 {
		c.ki = st.Ki
	}
	if st.Kd != 0 {
		c.kd = st.Kd
	}
	return nil
}

func (c *AdaptiveThresholdController) Current() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quality, c.confidence
}

func (c *AdaptiveThresholdController) saveStateLocked() {
	if c == nil || c.statePath == "" {
		return
	}
	st := adaptiveControllerState{
		TargetBadRatio:  c.targetBad,
		Kp:              c.kp,
		Ki:              c.ki,
		Kd:              c.kd,
		QualityFloor:   c.quality,
		ConfidenceFloor: c.confidence,
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.statePath), 0o750)
	_ = os.WriteFile(c.statePath, b, 0o600)
}

func (c *AdaptiveThresholdController) Observe(good, bad int) {
	if c == nil {
		return
	}
	total := good + bad
	if total <= 0 {
		return
	}
	ratio := float64(bad) / float64(total)
	err := ratio - c.targetBad
	c.mu.Lock()
	defer c.mu.Unlock()
	c.integral += err
	if c.integral > 3.0 {
		c.integral = 3.0
	}
	if c.integral < -3.0 {
		c.integral = -3.0
	}
	derivative := err - c.prevErr
	c.prevErr = err
	output := c.kp*err + c.ki*c.integral + c.kd*derivative
	delta := int(output)
	if delta > 7 {
		delta = 7
	}
	if delta < -7 {
		delta = -7
	}
	c.quality = clampInt(c.quality+delta, c.minFloor, c.maxFloor)
	c.confidence = clampInt(c.confidence+delta, c.minFloor, c.maxFloor)

	// Online governance for PID coefficients (simple stability guard).
	if c.onlineTune {
		if err > 0.02 {
			c.kp = clampFloat(c.kp*1.03, 1, 60)
			c.ki = clampFloat(c.ki*1.02, 0, 40)
			c.kd = clampFloat(c.kd*1.01, 0, 60)
		} else if err < -0.02 {
			c.kp = clampFloat(c.kp*0.97, 1, 60)
			c.ki = clampFloat(c.ki*0.98, 0, 40)
			c.kd = clampFloat(c.kd*0.99, 0, 60)
		}
	}
	c.saveStateLocked()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

