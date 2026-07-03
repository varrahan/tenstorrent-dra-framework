package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func main() {
	outputDir := flag.String("output-dir", "", "directory to write generated manifest files; stdout is used when empty")
	kind := flag.String("kind", "all", "manifest kind to generate: all, deviceclasses, or resourceslices")
	nodeName := flag.String("node-name", dra.ReferenceNodeName, "node name for reference ResourceSlice manifests")
	driverName := flag.String("driver-name", dra.DefaultDriverName, "DRA driver name")
	flag.Parse()

	manifests, err := generate(*kind, *driverName, *nodeName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate manifests: %v\n", err)
		os.Exit(1)
	}

	if *outputDir == "" {
		writeStdout(manifests)
		return
	}
	if err := writeFiles(*outputDir, manifests); err != nil {
		fmt.Fprintf(os.Stderr, "write manifests: %v\n", err)
		os.Exit(1)
	}
}

func generate(kind, driverName, nodeName string) (map[string][]byte, error) {
	manifests := map[string][]byte{}

	switch kind {
	case "all", "deviceclasses":
		data, err := dra.DeviceClassManifestYAML(driverName)
		if err != nil {
			return nil, err
		}
		manifests["deviceclasses.yaml"] = data
	}

	switch kind {
	case "all", "resourceslices":
		data, err := dra.ReferenceResourceSliceManifestYAML(driverName, nodeName)
		if err != nil {
			return nil, err
		}
		manifests["resourceslices.yaml"] = data
	}

	if len(manifests) == 0 {
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	return manifests, nil
}

func writeStdout(manifests map[string][]byte) {
	for _, name := range []string{"deviceclasses.yaml", "resourceslices.yaml"} {
		data, ok := manifests[name]
		if !ok {
			continue
		}
		if len(manifests) > 1 {
			fmt.Printf("# %s\n", name)
		}
		os.Stdout.Write(data)
	}
}

func writeFiles(outputDir string, manifests map[string][]byte) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	for name, data := range manifests {
		path := filepath.Join(outputDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
