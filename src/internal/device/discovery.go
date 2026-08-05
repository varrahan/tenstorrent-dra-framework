package device

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
}

// classifyCharacterDevice validates a filesystem entry and extracts its device numbers.
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

// majorMinor decodes Linux's packed device identifier into major and minor numbers.
func majorMinor(device uint64) (uint64, uint64) {
	major := (device >> 8) & 0xfff
	major |= (device >> 32) & ^uint64(0xfff)

	minor := device & 0xff
	minor |= (device >> 12) & ^uint64(0xff)

	return major, minor
}
