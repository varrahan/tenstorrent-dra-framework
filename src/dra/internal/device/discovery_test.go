package device

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverSortsAndFiltersNodes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "z"))
	writeFile(t, filepath.Join(root, "ignore"))
	writeFile(t, filepath.Join(root, "a"))

	nodes, err := discover(root, func(path string, info fs.FileInfo) (Node, bool, error) {
		switch filepath.Base(path) {
		case "a", "z":
			return Node{ID: filepath.Base(path), Path: path}, true, nil
		default:
			return Node{}, false, nil
		}
	})
	if err != nil {
		t.Fatalf("discover returned error: %v", err)
	}

	gotIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		gotIDs = append(gotIDs, node.ID)
	}
	wantIDs := []string{"a", "z"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("device IDs = %v, want %v", gotIDs, wantIDs)
	}
}

func TestDiscoverSingleDevicePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "0")
	writeFile(t, path)

	nodes, err := discover(path, func(path string, info fs.FileInfo) (Node, bool, error) {
		return Node{ID: filepath.Base(path), Path: path}, true, nil
	})
	if err != nil {
		t.Fatalf("discover returned error: %v", err)
	}

	if len(nodes) != 1 || nodes[0].ID != "0" {
		t.Fatalf("nodes = %#v, want one node with ID 0", nodes)
	}
}

func TestDiscoverMissingRootReturnsError(t *testing.T) {
	_, err := Discover(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("Discover returned nil error for missing root")
	}
}

func TestMajorMinor(t *testing.T) {
	major, minor := majorMinor(0xf100)
	if major != 0xf1 || minor != 0 {
		t.Fatalf("majorMinor(0xf100) = (%d, %d), want (241, 0)", major, minor)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
