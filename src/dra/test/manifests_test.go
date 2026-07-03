package test

import (
	"os"
	"strings"
	"testing"
)

func TestManifestsUseSupportedComputeClasses(t *testing.T) {
	deviceClasses := readManifest(t, "../manifests/deviceclasses.yaml")
	resourceSlices := readManifest(t, "../manifests/resourceslices.yaml")

	for _, className := range []string{
		"tenstorrent-wormhole-n150",
		"tenstorrent-wormhole-n300",
		"tenstorrent-blackhole-p100",
		"tenstorrent-blackhole-p150",
	} {
		if !strings.Contains(deviceClasses, className) {
			t.Fatalf("deviceclasses manifest is missing %q", className)
		}
	}

	for _, series := range []string{"n150", "n300", "p100", "p150"} {
		if !strings.Contains(resourceSlices, "cardSeries:\n        string: "+series) {
			t.Fatalf("resourceslices manifest is missing cardSeries %q", series)
		}
	}

	for _, redundant := range []string{
		"n150d",
		"n150s",
		"n300d",
		"n300s",
		"p100a",
		"p150a",
		"p150b",
		"blackhole-p300",
		"cardModel",
		"cooling",
		"dimensions",
	} {
		if strings.Contains(deviceClasses, redundant) || strings.Contains(resourceSlices, redundant) {
			t.Fatalf("manifests include redundant physical variant data %q", redundant)
		}
	}
}

func TestManifestDocumentCounts(t *testing.T) {
	deviceClasses := readManifest(t, "../manifests/deviceclasses.yaml")
	resourceSlices := readManifest(t, "../manifests/resourceslices.yaml")

	if got := strings.Count(deviceClasses, "kind: DeviceClass"); got != 4 {
		t.Fatalf("DeviceClass document count = %d, want 4", got)
	}
	if got := strings.Count(resourceSlices, "kind: ResourceSlice"); got != 4 {
		t.Fatalf("ResourceSlice document count = %d, want 4", got)
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
