package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/device"
)

func main() {
	deviceRoot := flag.String("device-root", "/dev/tenstorrent", "Tenstorrent device root or device node")
	flag.Parse()

	devices, err := device.Discover(*deviceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover devices: %v\n", err)
		os.Exit(1)
	}

	output := struct {
		DeviceRoot string        `json:"deviceRoot"`
		Devices    []device.Node `json:"devices"`
	}{
		DeviceRoot: *deviceRoot,
		Devices:    devices,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "encode discovery output: %v\n", err)
		os.Exit(1)
	}
}
