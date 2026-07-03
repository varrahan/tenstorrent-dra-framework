package test

import (
	"os"
	"strings"
	"testing"
)

func TestManifestsUseSupportedCardModels(t *testing.T) {
	deviceClasses := readManifest(t, "../manifests/deviceclasses.yaml")
	resourceSlices := readManifest(t, "../manifests/resourceslices.yaml")

	for _, model := range []string{"n150d", "n150s", "n300d", "n300s", "p100a", "p150a", "p150b"} {
		className := "tenstorrent-"
		if strings.HasPrefix(model, "n") {
			className += "wormhole-"
		} else {
			className += "blackhole-"
		}
		className += model

		if !strings.Contains(deviceClasses, className) {
			t.Fatalf("deviceclasses manifest is missing %q", className)
		}
		if !strings.Contains(resourceSlices, "cardModel:\n        string: "+model) {
			t.Fatalf("resourceslices manifest is missing cardModel %q", model)
		}
	}

	if strings.Contains(deviceClasses, "blackhole-p300") || strings.Contains(resourceSlices, "blackhole-p300") {
		t.Fatal("manifests include non-existent blackhole-p300")
	}
}

func TestManifestDocumentCounts(t *testing.T) {
	deviceClasses := readManifest(t, "../manifests/deviceclasses.yaml")
	resourceSlices := readManifest(t, "../manifests/resourceslices.yaml")

	if got := strings.Count(deviceClasses, "kind: DeviceClass"); got != 7 {
		t.Fatalf("DeviceClass document count = %d, want 7", got)
	}
	if got := strings.Count(resourceSlices, "kind: ResourceSlice"); got != 7 {
		t.Fatalf("ResourceSlice document count = %d, want 7", got)
	}
}

func readManifest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
