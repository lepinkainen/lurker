package updates

import (
	"testing"
)

func TestCompareRemoteByCommit(t *testing.T) {
	current := BuildInfo{Commit: "abc123", Version: "dev"}
	remote := remoteStatus{Commit: "def456", Version: "main"}
	if !compareRemote(current, remote) {
		t.Fatal("expected update available when commit differs")
	}
}

func TestCompareRemoteByVersionFallback(t *testing.T) {
	current := BuildInfo{Commit: "unknown", Version: "1.0.0"}
	remote := remoteStatus{Version: "1.0.1"}
	if !compareRemote(current, remote) {
		t.Fatal("expected update available when version differs")
	}
}

func TestCompareRemoteNoData(t *testing.T) {
	if compareRemote(BuildInfo{}, remoteStatus{}) {
		t.Fatal("expected no update when compare data missing")
	}
}

func TestNormalizeImage(t *testing.T) {
	repo, err := normalizeImage("ghcr.io/lepinkainen/lurker")
	if err != nil {
		t.Fatalf("normalizeImage returned err: %v", err)
	}
	if repo != "lepinkainen/lurker" {
		t.Fatalf("got %q", repo)
	}
}

func TestChooseManifest(t *testing.T) {
	entry, ok := chooseManifest([]manifestListEntry{
		{Digest: "sha256:a", Platform: manifestPlatform{OS: "linux", Architecture: "arm64"}},
		{Digest: "sha256:b", Platform: manifestPlatform{OS: "linux", Architecture: "amd64"}},
	}, Platform{OS: "linux", Architecture: "amd64"})
	if !ok {
		t.Fatal("expected matching manifest")
	}
	if entry.Digest != "sha256:b" {
		t.Fatalf("got digest %q", entry.Digest)
	}
}
