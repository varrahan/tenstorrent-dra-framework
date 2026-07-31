package test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/device"
)

func TestDeviceDiscoverSingleCharacterDevice(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("character device major/minor assertions are Linux-specific")
	}

	nodes, err := device.Discover("/dev/null")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes length = %d, want 1", len(nodes))
	}

	got := nodes[0]
	if got.ID != "null" || got.Path != "/dev/null" {
		t.Fatalf("node identity = %#v, want /dev/null", got)
	}
	if got.Major != 1 || got.Minor != 3 {
		t.Fatalf("major/minor = (%d, %d), want (1, 3)", got.Major, got.Minor)
	}
}

func TestDeviceDiscoverIgnoresNonCharacterNodes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "z"))
	writeFile(t, filepath.Join(root, "a"))

	nodes, err := device.Discover(root)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes = %#v, want no character devices", nodes)
	}
}

func TestDeviceDiscoverSingleRegularPathIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "0")
	writeFile(t, path)

	nodes, err := device.Discover(path)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes = %#v, want no character devices", nodes)
	}
}

func TestDeviceDiscoverMissingRootReturnsError(t *testing.T) {
	_, err := device.Discover(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("Discover returned nil error for missing root")
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
