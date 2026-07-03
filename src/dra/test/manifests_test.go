package test

import (
	"os"
	"strings"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestCheckedInManifestsMatchGeneratedDRAObjects(t *testing.T) {
	wantDeviceClasses, err := dra.DeviceClassManifestYAML("")
	if err != nil {
		t.Fatalf("generate DeviceClass manifest: %v", err)
	}
	wantResourceSlices, err := dra.ReferenceResourceSliceManifestYAML("", dra.ReferenceNodeName)
	if err != nil {
		t.Fatalf("generate ResourceSlice manifest: %v", err)
	}

	assertManifestEquals(t, "../manifests/deviceclasses.yaml", string(wantDeviceClasses))
	assertManifestEquals(t, "../manifests/resourceslices.yaml", string(wantResourceSlices))
}

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

	for _, key := range []string{
		"aiClockMHz",
		"connectivity",
		"systemInterfaceType",
		"systemInterfaceCount",
		"tensixCores",
		"memoryBandwidthBytesPerSecond",
	} {
		if !strings.Contains(resourceSlices, key) {
			t.Fatalf("resourceslices manifest is missing %q", key)
		}
	}
	if strings.Contains(resourceSlices, "tensix-cores") {
		t.Fatal("resourceslices manifest uses an invalid hyphenated QualifiedName identifier")
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

func assertManifestEquals(t *testing.T, path, want string) {
	t.Helper()
	got := readManifest(t, path)
	if got != want {
		t.Fatalf("%s is not generated from Go source; run go generate ./src/dra", path)
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
