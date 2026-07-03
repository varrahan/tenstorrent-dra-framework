package test

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
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
	for _, key := range []string{
		"aiClockMHz",
		"connectivity",
		"systemInterfaceType",
		"systemInterfaceCount",
	} {
		if !strings.Contains(resourceSlices, key) {
			t.Fatalf("resourceslices manifest is missing %q", key)
		}
	}
	if !strings.Contains(resourceSlices, "aiClockMHz:\n        int:") {
		t.Fatal("resourceslices manifest must publish aiClockMHz as an int DeviceAttribute")
	}
	if strings.Contains(resourceSlices, "systemInterface:\n        string:") {
		t.Fatal("resourceslices manifest still uses combined systemInterface string")
	}
	if strings.Contains(resourceSlices, "aiClockGHz:\n        value:") {
		t.Fatal("resourceslices manifest uses invalid value field for aiClockGHz attribute")
	}
	if strings.Contains(resourceSlices, "standard_float:") {
		t.Fatal("resourceslices manifest uses invalid standard_float DeviceAttribute field")
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

func TestDeviceClassManifestMatchesModels(t *testing.T) {
	deviceClasses := readManifest(t, "../manifests/deviceclasses.yaml")

	for _, model := range dra.NewDeviceClassModels("") {
		block := manifestBlockForName(t, deviceClasses, model.Name)
		for _, want := range []string{
			"kind: DeviceClass",
			"name: " + model.Name,
			"tenstorrent.com/chip-series: " + model.ChipSeries,
			"tenstorrent.com/card-series: " + model.CardSeries,
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("DeviceClass manifest block for %q is missing %q", model.Name, want)
			}
		}
		if !strings.Contains(unindentManifestBlock(block), model.SelectorExpression) {
			t.Fatalf("DeviceClass manifest block for %q is missing selector expression %q", model.Name, model.SelectorExpression)
		}
	}
}

func TestResourceSliceManifestMatchesSupportedCardSpecs(t *testing.T) {
	resourceSlices := readManifest(t, "../manifests/resourceslices.yaml")

	for _, spec := range dra.SupportedCardSpecs {
		block := manifestBlockForName(t, resourceSlices, "ttsim-"+spec.ChipSeries+"-"+spec.CardSeries)
		for _, want := range []string{
			"kind: ResourceSlice",
			"name: ttsim-" + spec.ChipSeries + "-" + spec.CardSeries,
			"tenstorrent.com/chip-series: " + spec.ChipSeries,
			"tenstorrent.com/card-series: " + spec.CardSeries,
			"driver: " + dra.DefaultDriverName,
			"name: ttsim-node/" + spec.ChipSeries + "-" + spec.CardSeries,
			"- name: tt-" + spec.ChipSeries + "-" + spec.CardSeries + "-0",
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("ResourceSlice manifest block for %s/%s is missing %q", spec.ChipSeries, spec.CardSeries, want)
			}
		}

		gotAttributeKeys := manifestSectionKeys(t, block, "attributes", "capacity")
		wantAttributeKeys := mapKeys(spec.Attributes())
		if !reflect.DeepEqual(gotAttributeKeys, wantAttributeKeys) {
			t.Fatalf("ResourceSlice attribute keys for %s/%s = %#v, want %#v", spec.ChipSeries, spec.CardSeries, gotAttributeKeys, wantAttributeKeys)
		}
		for key, attribute := range spec.Attributes() {
			assertAttributeInManifest(t, block, key, attribute)
		}

		gotCapacityKeys := manifestSectionKeys(t, block, "capacity", "")
		wantCapacityKeys := mapKeys(spec.Capacity())
		if !reflect.DeepEqual(gotCapacityKeys, wantCapacityKeys) {
			t.Fatalf("ResourceSlice capacity keys for %s/%s = %#v, want %#v", spec.ChipSeries, spec.CardSeries, gotCapacityKeys, wantCapacityKeys)
		}
		for key, capacity := range spec.Capacity() {
			assertScalarInManifest(t, block, key, "value", capacity.Value)
		}
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

func manifestBlockForName(t *testing.T, manifest, name string) string {
	t.Helper()
	for _, block := range strings.Split(manifest, "\n---\n") {
		if strings.Contains(block, "name: "+name) {
			return block
		}
	}
	t.Fatalf("manifest block for %q not found", name)
	return ""
}

func manifestSectionKeys(t *testing.T, block, sectionName, nextSectionName string) []string {
	t.Helper()
	startMarker := "\n    " + sectionName + ":\n"
	start := strings.Index(block, startMarker)
	if start == -1 {
		t.Fatalf("manifest block is missing %q section:\n%s", sectionName, block)
	}
	section := block[start+len(startMarker):]
	if nextSectionName != "" {
		endMarker := "\n    " + nextSectionName + ":\n"
		end := strings.Index(section, endMarker)
		if end == -1 {
			t.Fatalf("manifest block is missing %q section after %q:\n%s", nextSectionName, sectionName, block)
		}
		section = section[:end]
	}

	keys := []string(nil)
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "      "+dra.DeviceAttributeDomain+"/") && strings.HasSuffix(trimmed, ":") {
			keys = append(keys, strings.TrimSuffix(trimmed, ":"))
		}
	}
	sort.Strings(keys)
	return keys
}

func unindentManifestBlock(block string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, " ")
	}
	return strings.Join(lines, "\n")
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertAttributeInManifest(t *testing.T, block, key string, attribute dra.DeviceAttribute) {
	t.Helper()
	switch {
	case attribute.String != nil:
		assertScalarInManifest(t, block, key, "string", *attribute.String)
	case attribute.Int != nil:
		assertScalarInManifest(t, block, key, "int", fmt.Sprint(*attribute.Int))
	case attribute.Bool != nil:
		assertScalarInManifest(t, block, key, "bool", fmt.Sprint(*attribute.Bool))
	default:
		t.Fatalf("attribute %q has no typed value", key)
	}
}

func assertScalarInManifest(t *testing.T, block, key, field, value string) {
	t.Helper()
	for _, pattern := range []string{
		fmt.Sprintf("%s:\n        %s: %s", key, field, value),
		fmt.Sprintf("%s:\n        %s: %q", key, field, value),
	} {
		if strings.Contains(block, pattern) {
			return
		}
	}
	t.Fatalf("manifest block is missing %s %s value %q:\n%s", key, field, value, block)
}
