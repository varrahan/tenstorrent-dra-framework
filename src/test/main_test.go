package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runDriverCommand(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	command := exec.Command("go", append([]string{"run", "../cmd/tt-dra-driver"}, args...)...)
	command.Env = os.Environ()
	return command.CombinedOutput()
}

func TestRunVersionCommand(t *testing.T) {
	output, err := runDriverCommand(t, "version")
	if err != nil {
		t.Fatalf("version command failed: %v, output = %s", err, output)
	}
	var got map[string]any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode version output: %v", err)
	}
	for _, key := range []string{"version", "commit", "buildDate"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("version output missing %q: %#v", key, got)
		}
	}
}

func TestRunCommandDispatchesAndRejectsInvalidInvocations(t *testing.T) {
	output, err := runDriverCommand(t, "version")
	if err != nil || !strings.Contains(string(output), `"version":"`) {
		t.Fatalf("version command failed: %v, output = %s", err, output)
	}
	for name, args := range map[string][]string{
		"missing": nil,
		"unknown": {"unknown"},
		"node":    {"node", "--interval=0s"},
		"cleanup": {"cleanup"},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := runDriverCommand(t, args...)
			if err == nil {
				t.Fatalf("invalid invocation unexpectedly succeeded: %s", output)
			}
		})
	}
}

func TestCommandFlagValidationStopsBeforeClusterAccess(t *testing.T) {
	t.Setenv("NODE_NAME", "node-a")
	for name, args := range map[string][]string{
		"parse":         {"node", "--interval=invalid"},
		"interval":      {"node", "--interval=0s"},
		"grace":         {"node", "--inventory-grace-period=0s"},
		"api limits":    {"node", "--kube-api-burst=0"},
		"noop mode":     {"node", "--reset-mode=noop"},
		"topology":      {"controller", "--topology-ttl=invalid"},
		"placement":     {"controller", "--placement-timeout=0s"},
		"bad burst":     {"controller", "--kube-api-burst=0"},
		"bad qps":       {"controller", "--kube-api-qps=0"},
		"cleanup qps":   {"cleanup", "--kube-api-qps=0"},
		"cleanup flags": {"cleanup", "--invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := runDriverCommand(t, args...)
			if err == nil {
				t.Fatalf("invalid flags should fail before cluster setup: %s", output)
			}
		})
	}
	t.Setenv("NODE_NAME", "")
	output, err := runDriverCommand(t, "node")
	if err == nil || !strings.Contains(string(output), "node name is required") {
		t.Fatalf("missing node name behavior incorrect: err=%v output=%s", err, output)
	}
}

func TestInventoryCommandUsesExplicitRoots(t *testing.T) {
	root := t.TempDir()
	deviceRoot := filepath.Join(root, "dev")
	sysfsRoot := filepath.Join(root, "sysfs")
	pciRoot := filepath.Join(root, "pci")
	stateDir := filepath.Join(root, "state")
	for _, path := range []string{deviceRoot, sysfsRoot, pciRoot, stateDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	output, err := runDriverCommand(t,
		"list",
		"--device-root="+deviceRoot,
		"--sysfs-root="+sysfsRoot,
		"--pci-sysfs-root="+pciRoot,
		"--state-dir="+stateDir,
	)
	if err != nil {
		t.Fatalf("explicit inventory command failed: %v output=%s", err, output)
	}

	output, err = runDriverCommand(t, "list", "--device-root=relative")
	if err == nil {
		t.Fatalf("relative inventory root was accepted: %s", output)
	}
}
