package test

import (
	"reflect"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestDeviceClassesBuildSupportedChipAndCardClasses(t *testing.T) {
	classes := dra.NewDeviceClasses("")

	want := []struct {
		name string
		chip string
		card string
	}{
		{name: "tenstorrent-wormhole-n150", chip: "wormhole", card: "n150"},
		{name: "tenstorrent-wormhole-n300", chip: "wormhole", card: "n300"},
		{name: "tenstorrent-blackhole-p100", chip: "blackhole", card: "p100"},
		{name: "tenstorrent-blackhole-p150", chip: "blackhole", card: "p150"},
	}

	if len(classes) != len(want) {
		t.Fatalf("device class count = %d, want %d", len(classes), len(want))
	}

	for i := range want {
		got := classes[i]
		if got.APIVersion != "resource.k8s.io/v1" || got.Kind != "DeviceClass" {
			t.Fatalf("class %d type = %s/%s, want resource.k8s.io/v1/DeviceClass", i, got.APIVersion, got.Kind)
		}
		if got.Name != want[i].name ||
			got.Labels["tenstorrent.com/chip-series"] != want[i].chip ||
			got.Labels["tenstorrent.com/card-series"] != want[i].card {
			t.Fatalf("class %d = %#v, want %#v", i, got, want[i])
		}
		if len(got.Spec.Selectors) != 1 || got.Spec.Selectors[0].CEL == nil || got.Spec.Selectors[0].CEL.Expression == "" {
			t.Fatalf("class %d has no CEL selector", i)
		}
	}
}

func TestDeviceClassSelectorExpressionUsesDriverAndAttributes(t *testing.T) {
	got := dra.DeviceClassSelectorExpression("", dra.DeviceClassVariant{
		ChipSeries: "wormhole",
		CardSeries: "n300",
	})
	want := "device.driver == \"tenstorrent.com/dra\" &&\n" +
		"device.attributes[\"tenstorrent.com\"].chipSeries == \"wormhole\" &&\n" +
		"device.attributes[\"tenstorrent.com\"].cardSeries == \"n300\""

	if got != want {
		t.Fatalf("selector expression = %q, want %q", got, want)
	}
}

func TestSupportedDeviceClassVariants(t *testing.T) {
	got := dra.SupportedDeviceClassVariants
	want := []dra.DeviceClassVariant{
		{ChipSeries: "wormhole", CardSeries: "n150"},
		{ChipSeries: "wormhole", CardSeries: "n300"},
		{ChipSeries: "blackhole", CardSeries: "p100"},
		{ChipSeries: "blackhole", CardSeries: "p150"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supported variants = %#v, want %#v", got, want)
	}
}
