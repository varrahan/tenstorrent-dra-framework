package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

func TestRunVersion(t *testing.T) {
	originalVersion, originalCommit, originalBuildDate := version, commit, buildDate
	t.Cleanup(func() {
		version, commit, buildDate = originalVersion, originalCommit, originalBuildDate
	})
	version, commit, buildDate = "1.2.3", "0123456789abcdef", "2026-08-04T18:41:21Z"

	var output bytes.Buffer
	if err := runVersion(&output); err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}
	var got buildInformation
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode version output: %v", err)
	}
	want := (buildInformation{Version: version, Commit: commit, BuildDate: buildDate})
	if got != want {
		t.Fatalf("runVersion() = %#v, want %#v", got, want)
	}
}

func TestRunCommandDispatchesAndRejectsInvalidInvocations(t *testing.T) {
	var output bytes.Buffer
	if err := runCommand([]string{"version"}, &output); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if !strings.Contains(output.String(), `"version":"`) {
		t.Fatalf("version output = %q", output.String())
	}
	for name, args := range map[string][]string{
		"missing": nil,
		"unknown": {"unknown"},
		"node":    {"node", "--interval=0s"},
		"cleanup": {"cleanup"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runCommand(args, &bytes.Buffer{}); err == nil {
				t.Fatal("invalid invocation succeeded")
			}
		})
	}
}

func TestCommandFlagValidationStopsBeforeClusterAccess(t *testing.T) {
	t.Setenv("NODE_NAME", "node-a")
	nodeTests := map[string][]string{
		"parse":            {"--interval=invalid"},
		"interval":         {"--interval=0s"},
		"grace":            {"--interval=2s", "--inventory-grace-period=1s"},
		"api limits":       {"--kube-api-qps=0"},
		"reset mode":       {"--reset-mode=invalid"},
		"unsafe noop mode": {"--reset-mode=noop"},
	}
	for name, args := range nodeTests {
		t.Run("node "+name, func(t *testing.T) {
			if err := runNode(args); err == nil {
				t.Fatal("invalid node flags reached cluster setup")
			}
		})
	}

	t.Setenv("NODE_NAME", "")
	if err := runNode(nil); err == nil || !strings.Contains(err.Error(), "node name") {
		t.Fatalf("missing node name error = %v", err)
	}
	for name, args := range map[string][]string{
		"parse":      {"--topology-ttl=invalid"},
		"ttl":        {"--topology-ttl=0s"},
		"placement":  {"--placement-timeout=0s"},
		"api limits": {"--kube-api-burst=0"},
	} {
		t.Run("controller "+name, func(t *testing.T) {
			if err := runController(args); err == nil {
				t.Fatal("invalid controller flags reached cluster setup")
			}
		})
	}
	if err := runCleanup([]string{"--kube-api-qps=0"}); err == nil {
		t.Fatal("invalid cleanup API limits reached cluster setup")
	}
	if err := runCleanup([]string{"--invalid"}); err == nil {
		t.Fatal("invalid cleanup flag was accepted")
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
	args := []string{
		"--device-root=" + deviceRoot,
		"--sysfs-root=" + sysfsRoot,
		"--pci-sysfs-root=" + pciRoot,
		"--state-dir=" + stateDir,
	}
	if err := runList(args); err != nil {
		t.Fatalf("empty explicit inventory failed: %v", err)
	}
	set, roots := inventoryFlags("test")
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	if source, err := provider(roots); err != nil || source.String() != "filesystem" {
		t.Fatalf("provider = %#v, %v", source, err)
	}
	roots.DeviceRoot = "relative"
	if _, err := provider(roots); err == nil {
		t.Fatal("relative inventory root was accepted")
	}
}

func TestLifecycleEventTranslation(t *testing.T) {
	reasons := map[lifecycle.AuditEvent]string{
		{Outcome: "quarantined"}:         "DeviceQuarantined",
		{Action: "claim-prepare-intent"}: "ClaimPrepareStarted",
		{Action: "claim-prepare"}:        "ClaimPrepared",
		{Action: "claim-release-intent"}: "ClaimReleaseStarted",
		{Action: "claim-release"}:        "ClaimReleased",
		{Action: "preflight-sanitize"}:   "PreflightSanitized",
		{Action: "postflight-sanitize"}:  "PostflightSanitized",
		{Action: "recovery-sanitize"}:    "DeviceRecovered",
		{Action: "recovery-health"}:      "DeviceRecovered",
		{Action: "inventory"}:            "InventoryDegraded",
		{Action: "state-recovery"}:       "StateRecovered",
		{Action: "startup-recovery"}:     "StateRecovered",
		{Action: "custom_audit-action"}:  "CustomAuditAction",
	}
	for event, want := range reasons {
		if got := lifecycleEventReason(event); got != want {
			t.Fatalf("lifecycleEventReason(%#v) = %q, want %q", event, got, want)
		}
	}
	if outcome(nil) != "success" || outcome(errors.New("failure")) != "failure" {
		t.Fatal("unexpected reconciliation outcome")
	}
	if pod, ok := podReference("controller-0", "system").(*corev1.Pod); !ok || pod.Name != "controller-0" || pod.Namespace != "system" {
		t.Fatalf("pod reference = %#v", pod)
	}

	recorder := record.NewFakeRecorder(2)
	sink := lifecycleEventSink(recorder, "node-a")
	sink(lifecycle.AuditEvent{Action: "inventory", Outcome: "degraded", Device: "device-a", Reason: "stale"}, nil)
	sink(lifecycle.AuditEvent{Action: "claim-prepare", Outcome: "success"}, &lifecycle.PreparedClaim{Name: "claim", Namespace: "default", UID: "claim-uid"})
	for index := 0; index < 2; index++ {
		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, "InventoryDegraded") && !strings.Contains(event, "ClaimPrepared") {
				t.Fatalf("unexpected event: %s", event)
			}
		default:
			t.Fatal("expected lifecycle event was not emitted")
		}
	}
}
