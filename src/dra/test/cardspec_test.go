package test

import (
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestSupportedCardSpecsMatchTenstorrentTables(t *testing.T) {
	want := map[string]struct {
		part       string
		chip       string
		series     string
		tensix     int
		memoryGB   int
		bandwidth  int
		powerWatts int
	}{
		"n150d": {"TC-02002", "wormhole", "n150", 72, 12, 288, 160},
		"n150s": {"TC-02001", "wormhole", "n150", 72, 12, 288, 160},
		"n300d": {"TC-02004", "wormhole", "n300", 128, 24, 576, 300},
		"n300s": {"TC-02003", "wormhole", "n300", 128, 24, 576, 300},
		"p100a": {"TC-03008", "blackhole", "p100", 120, 28, 448, 300},
		"p150a": {"TC-03003", "blackhole", "p150", 120, 32, 512, 300},
		"p150b": {"TC-03002", "blackhole", "p150", 120, 32, 512, 300},
	}

	if len(dra.SupportedCardSpecs) != len(want) {
		t.Fatalf("supported card specs = %d, want %d", len(dra.SupportedCardSpecs), len(want))
	}

	for _, spec := range dra.SupportedCardSpecs {
		expected, ok := want[spec.CardModel]
		if !ok {
			t.Fatalf("unexpected card model %q", spec.CardModel)
		}
		if spec.PartNumber != expected.part ||
			spec.ChipSeries != expected.chip ||
			spec.CardSeries != expected.series ||
			spec.TensixCores != expected.tensix ||
			spec.MemoryGB != expected.memoryGB ||
			spec.MemoryBandwidthGBPerSec != expected.bandwidth ||
			spec.TBPWatts != expected.powerWatts {
			t.Fatalf("spec %s = %#v, want %#v", spec.CardModel, spec, expected)
		}
	}
}

func TestCardSpecForModel(t *testing.T) {
	spec, ok := dra.CardSpecForModel("p150b")
	if !ok {
		t.Fatal("CardSpecForModel returned false for p150b")
	}
	if spec.Cooling != "Passive" {
		t.Fatalf("p150b cooling = %q, want Passive", spec.Cooling)
	}

	if _, ok := dra.CardSpecForModel("p300"); ok {
		t.Fatal("CardSpecForModel returned true for non-existent p300")
	}
}
