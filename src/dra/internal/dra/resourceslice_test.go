package dra

import (
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/device"
)

func TestNewResourceSliceModelUsesDefaultsAndMapsDevices(t *testing.T) {
	model := NewResourceSliceModel("", "node-a", []device.Node{
		{ID: "0", Path: "/dev/tenstorrent/0", Major: 241, Minor: 0},
	})

	if model.DriverName != DefaultDriverName {
		t.Fatalf("DriverName = %q, want %q", model.DriverName, DefaultDriverName)
	}
	if model.NodeName != "node-a" {
		t.Fatalf("NodeName = %q, want node-a", model.NodeName)
	}
	if len(model.Devices) != 1 {
		t.Fatalf("Devices length = %d, want 1", len(model.Devices))
	}

	got := model.Devices[0]
	if got.Name != "tt-0" || got.Path != "/dev/tenstorrent/0" || got.Major != 241 || got.Minor != 0 {
		t.Fatalf("device mapping = %#v", got)
	}
	if got.Attributes["tenstorrent.com/device-id"] != "0" {
		t.Fatalf("device-id attribute = %q, want 0", got.Attributes["tenstorrent.com/device-id"])
	}
}
