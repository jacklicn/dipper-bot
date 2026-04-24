package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAdaptiveControllerIncreasesThresholdOnBadRatio(t *testing.T) {
	c := NewAdaptiveThresholdController(60, 60, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
		OnlineTuning:   false,
	})
	c.Observe(1, 9)
	q, cf := c.Current()
	if q <= 60 || cf <= 60 {
		t.Fatalf("expected thresholds to increase, got quality=%d confidence=%d", q, cf)
	}
}

func TestAdaptiveControllerDecreasesThresholdOnGoodRatio(t *testing.T) {
	c := NewAdaptiveThresholdController(80, 80, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
		OnlineTuning:   false,
	})
	c.Observe(10, 0)
	q, cf := c.Current()
	if q >= 80 || cf >= 80 {
		t.Fatalf("expected thresholds to decrease, got quality=%d confidence=%d", q, cf)
	}
}

func TestAdaptiveControllerObserveNilSafe(t *testing.T) {
	var c *AdaptiveThresholdController
	c.Observe(1, 1)
}

func TestAdaptiveControllerObserveZeroTotalNoOp(t *testing.T) {
	c := NewAdaptiveThresholdController(55, 55, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		MinFloor:       40,
		MaxFloor:       95,
	})
	c.Observe(0, 0)
	c.Observe(-1, 1)
	q, cf := c.Current()
	if q != 55 || cf != 55 {
		t.Fatalf("expected unchanged 55/55, got %d/%d", q, cf)
	}
}

func TestAdaptiveControllerNewAppliesDefaults(t *testing.T) {
	c := NewAdaptiveThresholdController(50, 50, AdaptiveControllerParams{})
	if c.targetBad != 0.08 || c.minFloor != 40 || c.maxFloor != 95 {
		t.Fatalf("defaults: targetBad=%v min=%d max=%d", c.targetBad, c.minFloor, c.maxFloor)
	}
	if c.kp != 16 || c.ki != 4 || c.kd != 6 {
		t.Fatalf("defaults: kp=%v ki=%v kd=%v", c.kp, c.ki, c.kd)
	}
}

func TestAdaptiveControllerClampsToFloors(t *testing.T) {
	c := NewAdaptiveThresholdController(40, 40, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
	})
	c.Observe(1, 9)
	q, cf := c.Current()
	if q < 40 || cf < 40 {
		t.Fatalf("below minFloor: %d %d", q, cf)
	}
	c2 := NewAdaptiveThresholdController(95, 95, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
	})
	c2.Observe(10, 0)
	q2, cf2 := c2.Current()
	if q2 > 95 || cf2 > 95 {
		t.Fatalf("above maxFloor: %d %d", q2, cf2)
	}
}

func TestAdaptiveControllerLoadStateOverridesInitial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adaptive.json")
	st := adaptiveControllerState{
		TargetBadRatio:  0.1,
		Kp:              20,
		Ki:              5,
		Kd:              8,
		QualityFloor:    72,
		ConfidenceFloor: 71,
		UpdatedAt:       "2020-01-01T00:00:00Z",
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewAdaptiveThresholdController(10, 10, AdaptiveControllerParams{StatePath: path})
	q, cf := c.Current()
	if q != 72 || cf != 71 {
		t.Fatalf("load floors: want 72/71 got %d/%d", q, cf)
	}
	if c.targetBad != 0.1 || c.kp != 20 || c.ki != 5 || c.kd != 8 {
		t.Fatalf("load tuning: target=%v kp=%v ki=%v kd=%v", c.targetBad, c.kp, c.ki, c.kd)
	}
}

func TestAdaptiveControllerSaveAndReloadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	params := AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
		StatePath:      path,
	}
	c := NewAdaptiveThresholdController(60, 60, params)
	c.Observe(10, 0)
	q1, cf1 := c.Current()
	c2 := NewAdaptiveThresholdController(1, 1, params)
	q2, cf2 := c2.Current()
	if q2 != q1 || cf2 != cf1 {
		t.Fatalf("reload mismatch: first %d/%d second %d/%d", q1, cf1, q2, cf2)
	}
}

func TestAdaptiveControllerOnlineTuningRaisesKpOnHighError(t *testing.T) {
	c := NewAdaptiveThresholdController(60, 60, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
		OnlineTuning:   true,
	})
	kp0 := c.kp
	c.Observe(1, 9)
	if c.kp <= kp0 {
		t.Fatalf("expected kp to increase under high error, kp0=%v kp=%v", kp0, c.kp)
	}
}

func TestAdaptiveControllerOnlineTuningLowersKpOnLowError(t *testing.T) {
	c := NewAdaptiveThresholdController(60, 60, AdaptiveControllerParams{
		TargetBadRatio: 0.08,
		Kp:             16,
		Ki:             4,
		Kd:             6,
		MinFloor:       40,
		MaxFloor:       95,
		OnlineTuning:   true,
	})
	kp0 := c.kp
	c.Observe(100, 0)
	if c.kp >= kp0 {
		t.Fatalf("expected kp to decrease under low error, kp0=%v kp=%v", kp0, c.kp)
	}
}
