package main

import (
	"bytes"
	"encoding/json"
	"testing"
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
