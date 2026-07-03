package test

import (
	"reflect"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestDeviceClassModelsBuildSupportedChipAndCardClasses(t *testing.T) {
	models := dra.NewDeviceClassModels("")

	want := []dra.DeviceClassModel{
		{Name: "tenstorrent-wormhole-n150", DriverName: dra.DefaultDriverName, ChipSeries: "wormhole", CardSeries: "n150"},
		{Name: "tenstorrent-wormhole-n300", DriverName: dra.DefaultDriverName, ChipSeries: "wormhole", CardSeries: "n300"},
		{Name: "tenstorrent-blackhole-p100", DriverName: dra.DefaultDriverName, ChipSeries: "blackhole", CardSeries: "p100"},
		{Name: "tenstorrent-blackhole-p150", DriverName: dra.DefaultDriverName, ChipSeries: "blackhole", CardSeries: "p150"},
	}

	if len(models) != len(want) {
		t.Fatalf("device class model count = %d, want %d", len(models), len(want))
	}

	for i := range want {
		got := models[i]
		if got.Name != want[i].Name ||
			got.DriverName != want[i].DriverName ||
			got.ChipSeries != want[i].ChipSeries ||
			got.CardSeries != want[i].CardSeries {
			t.Fatalf("model %d = %#v, want %#v", i, got, want[i])
		}
		if got.SelectorExpression == "" {
			t.Fatalf("model %d has empty selector expression", i)
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
