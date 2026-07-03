package test

import (
	"reflect"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestDeviceClassModelsBuildSupportedChipAndCardClasses(t *testing.T) {
	models := dra.NewDeviceClassModels("")

	want := []dra.DeviceClassModel{
		{Name: "tenstorrent-wormhole-n150d", DriverName: dra.DefaultDriverName, ChipSeries: "wormhole", CardSeries: "n150", CardModel: "n150d"},
		{Name: "tenstorrent-wormhole-n150s", DriverName: dra.DefaultDriverName, ChipSeries: "wormhole", CardSeries: "n150", CardModel: "n150s"},
		{Name: "tenstorrent-wormhole-n300d", DriverName: dra.DefaultDriverName, ChipSeries: "wormhole", CardSeries: "n300", CardModel: "n300d"},
		{Name: "tenstorrent-wormhole-n300s", DriverName: dra.DefaultDriverName, ChipSeries: "wormhole", CardSeries: "n300", CardModel: "n300s"},
		{Name: "tenstorrent-blackhole-p100a", DriverName: dra.DefaultDriverName, ChipSeries: "blackhole", CardSeries: "p100", CardModel: "p100a"},
		{Name: "tenstorrent-blackhole-p150a", DriverName: dra.DefaultDriverName, ChipSeries: "blackhole", CardSeries: "p150", CardModel: "p150a"},
		{Name: "tenstorrent-blackhole-p150b", DriverName: dra.DefaultDriverName, ChipSeries: "blackhole", CardSeries: "p150", CardModel: "p150b"},
	}

	if len(models) != len(want) {
		t.Fatalf("device class model count = %d, want %d", len(models), len(want))
	}

	for i := range want {
		got := models[i]
		if got.Name != want[i].Name ||
			got.DriverName != want[i].DriverName ||
			got.ChipSeries != want[i].ChipSeries ||
			got.CardSeries != want[i].CardSeries ||
			got.CardModel != want[i].CardModel {
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
		CardModel:  "n300d",
	})
	want := "device.driver == \"tenstorrent.com/dra\" &&\n" +
		"device.attributes[\"tenstorrent.com\"].chipSeries == \"wormhole\" &&\n" +
		"device.attributes[\"tenstorrent.com\"].cardSeries == \"n300\" &&\n" +
		"device.attributes[\"tenstorrent.com\"].cardModel == \"n300d\""

	if got != want {
		t.Fatalf("selector expression = %q, want %q", got, want)
	}
}

func TestSupportedDeviceClassVariants(t *testing.T) {
	got := dra.SupportedDeviceClassVariants
	want := []dra.DeviceClassVariant{
		{ChipSeries: "wormhole", CardSeries: "n150", CardModel: "n150d"},
		{ChipSeries: "wormhole", CardSeries: "n150", CardModel: "n150s"},
		{ChipSeries: "wormhole", CardSeries: "n300", CardModel: "n300d"},
		{ChipSeries: "wormhole", CardSeries: "n300", CardModel: "n300s"},
		{ChipSeries: "blackhole", CardSeries: "p100", CardModel: "p100a"},
		{ChipSeries: "blackhole", CardSeries: "p150", CardModel: "p150a"},
		{ChipSeries: "blackhole", CardSeries: "p150", CardModel: "p150b"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supported variants = %#v, want %#v", got, want)
	}
}
