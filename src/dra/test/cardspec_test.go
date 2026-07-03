package test

import (
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestSupportedCardSpecsMatchComputeEquivalentTenstorrentRows(t *testing.T) {
	want := []struct {
		chip                 string
		series               string
		tensix               int
		memoryGB             int
		bandwidth            int
		powerWatts           int
		systemInterfaceType  string
		systemInterfaceCount int64
		connectivity         bool
	}{
		{"wormhole", "n150", 72, 12, 288, 160, "PCIe 4.0", 16, true},
		{"wormhole", "n300", 128, 24, 576, 300, "PCIe 4.0", 16, true},
		{"blackhole", "p100", 120, 28, 448, 300, "PCIe 5.0", 16, false},
		{"blackhole", "p150", 120, 32, 512, 300, "PCIe 5.0", 16, true},
	}

	if len(dra.SupportedCardSpecs) != len(want) {
		t.Fatalf("supported card specs = %d, want %d", len(dra.SupportedCardSpecs), len(want))
	}

	for i, spec := range dra.SupportedCardSpecs {
		expected := want[i]
		if spec.ChipSeries != expected.chip ||
			spec.CardSeries != expected.series ||
			spec.TensixCores != expected.tensix ||
			spec.MemoryGB != expected.memoryGB ||
			spec.MemoryBandwidthGBPerSec != expected.bandwidth ||
			spec.TBPWatts != expected.powerWatts ||
			spec.SystemInterfaceType != expected.systemInterfaceType ||
			spec.SystemInterfaceCount != expected.systemInterfaceCount ||
			spec.Connectivity != expected.connectivity {
			t.Fatalf("spec %d = %#v, want %#v", i, spec, expected)
		}
	}
}

func TestCardSpecForClass(t *testing.T) {
	spec, ok := dra.CardSpecForClass("blackhole", "p150")
	if !ok {
		t.Fatal("CardSpecForClass returned false for blackhole p150")
	}
	if spec.MemoryGB != 32 {
		t.Fatalf("p150 memory = %d, want 32", spec.MemoryGB)
	}

	if _, ok := dra.CardSpecForClass("blackhole", "p300"); ok {
		t.Fatal("CardSpecForClass returned true for non-existent p300")
	}
}
