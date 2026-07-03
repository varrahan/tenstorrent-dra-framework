package device

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// Node describes a character device exposed by tt-kmd.
type Node struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Major      uint64 `json:"major"`
	Minor      uint64 `json:"minor"`
	Mode       string `json:"mode"`
	ChipSeries string `json:"chipSeries,omitempty"`
	CardSeries string `json:"cardSeries,omitempty"`
	CardModel  string `json:"cardModel,omitempty"`
}

type classifier func(path string, info fs.FileInfo) (Node, bool, error)

// Discover scans a Tenstorrent device root and returns character devices.
func Discover(root string) ([]Node, error) {
	return discover(root, classifyCharacterDevice)
}

func discover(root string, classify classifier) ([]Node, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("stat device root %q: %w", root, err)
	}

	if !info.IsDir() {
		node, ok, err := classify(root, info)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return []Node{node}, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read device root %q: %w", root, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	nodes := make([]Node, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat device entry %q: %w", path, err)
		}

		node, ok, err := classify(path, info)
		if err != nil {
			return nil, err
		}
		if ok {
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

func classifyCharacterDevice(path string, info fs.FileInfo) (Node, bool, error) {
	if info.Mode()&os.ModeCharDevice == 0 {
		return Node{}, false, nil
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Node{}, false, fmt.Errorf("device %q has unsupported stat type %T", path, info.Sys())
	}

	major, minor := majorMinor(uint64(stat.Rdev))
	return Node{
		ID:    filepath.Base(path),
		Path:  path,
		Major: major,
		Minor: minor,
		Mode:  info.Mode().String(),
	}, true, nil
}

func majorMinor(device uint64) (uint64, uint64) {
	major := (device >> 8) & 0xfff
	major |= (device >> 32) & ^uint64(0xfff)

	minor := device & 0xff
	minor |= (device >> 12) & ^uint64(0xff)

	return major, minor
}
