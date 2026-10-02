package buildinfo

import (
	"runtime"
	"testing"
)

func TestCurrent(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildTime := BuildTime
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildTime = originalBuildTime
	})

	Version = "v1.2.3"
	Commit = "abc123"
	BuildTime = "2026-10-03T00:00:00Z"

	info := Current()
	if info.Version != Version || info.Commit != Commit || info.BuildTime != BuildTime {
		t.Fatalf("Current() = %+v", info)
	}
	if info.GoVersion != runtime.Version() {
		t.Fatalf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
}
