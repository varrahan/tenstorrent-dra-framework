package test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNodeCommandRejectsNonPositiveIntervalBeforeClusterSetup(t *testing.T) {
	command := exec.Command("go", "run", "../cmd/tt-dra-driver", "node", "--interval=0s")
	command.Env = append(os.Environ(), "NODE_NAME=worker-a")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "inventory interval must be positive") {
		t.Fatalf("command error = %v, output = %s", err, output)
	}
}

func TestNodeCommandRejectsUnsafeNoopResetMode(t *testing.T) {
	command := exec.Command("go", "run", "../cmd/tt-dra-driver", "node", "--reset-mode=noop")
	command.Env = append(os.Environ(), "NODE_NAME=worker-a")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "synthetic validation only") {
		t.Fatalf("command error = %v, output = %s", err, output)
	}
}
