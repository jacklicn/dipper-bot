package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jacklicn/dipper-bot/config"
)

func TestNormalizeUsageCostCurrency(t *testing.T) {
	if NormalizeUsageCostCurrency("") != "CNY" {
		t.Fatal()
	}
	if NormalizeUsageCostCurrency(" 美元 ") != "USD" {
		t.Fatal()
	}
	if NormalizeUsageCostCurrency("人民币") != "CNY" {
		t.Fatal()
	}
	if NormalizeUsageCostCurrency("cny") != "CNY" {
		t.Fatal()
	}
}

func TestEstimateUsageCostLayered_CNY_builtin(t *testing.T) {
	amt, st := EstimateUsageCostLayered("gpt-4o", 1_000_000, 0, "CNY", 7.2, nil, nil)
	if st != "priced" {
		t.Fatalf("status %q", st)
	}
	usd, _ := estimateBuiltinUSD("gpt-4o", 1_000_000, 0)
	want := usd * 7.2
	if amt < want-0.01 || amt > want+0.01 {
		t.Fatalf("amt %v want %v", amt, want)
	}
	_, st2 := EstimateUsageCostLayered("unknown-model-xyz", 1000, 1000, "CNY", 7.2, nil, nil)
	if st2 != "unknown_model" {
		t.Fatalf("unknown status %q", st2)
	}
}

func TestEstimateUsageCostLayered_USD_builtin(t *testing.T) {
	amt, st := EstimateUsageCostLayered("gpt-4o", 1_000_000, 0, "USD", 0, nil, nil)
	if st != "priced" {
		t.Fatalf("status %q", st)
	}
	usd, _ := estimateBuiltinUSD("gpt-4o", 1_000_000, 0)
	if amt < usd-0.0001 || amt > usd+0.0001 {
		t.Fatalf("amt %v usd %v", amt, usd)
	}
}

func TestEstimateUsageCostLayered_config(t *testing.T) {
	cfg := []config.UsagePricingOverrideEntry{
		{MatchSubstring: "my-custom", InputPerMillion: 18, OutputPerMillion: 36},
	}
	amt, st := EstimateUsageCostLayered("prefix/my-custom-model", 1_000_000, 0, "CNY", 7.2, cfg, nil)
	if st != "priced_config" {
		t.Fatalf("status %q", st)
	}
	if amt < 17.99 || amt > 18.01 {
		t.Fatalf("amt %v want 18", amt)
	}
}

func TestReadWorkspacePricingOverrides(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o750)
	p := filepath.Join(dir, "memory", "pricing_overrides.json")
	if err := os.WriteFile(p, []byte(`[{"matchSubstring":"acme","inputPerMillion":50,"outputPerMillion":100}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	ov, err := ReadWorkspacePricingOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov) != 1 || ov[0].InputPerMillion != 50 {
		t.Fatalf("%+v", ov)
	}
	amt, st := EstimateUsageCostLayered("openai/acme-reasoning", 1_000_000, 0, "CNY", 7.2, nil, ov)
	if st != "priced_file" {
		t.Fatalf("status %q", st)
	}
	if amt < 49.99 || amt > 50.01 {
		t.Fatalf("amt %v", amt)
	}
}
