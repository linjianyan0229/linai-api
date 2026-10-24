//go:build unit

package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/releaseupdate"
	"github.com/stretchr/testify/require"
)

type releaseClientStub struct {
	GitHubReleaseClient
	releases   []*GitHubRelease
	queries    int
	downloads  int
	repository string
	archive    []byte
	checksum   []byte
}

func (c *releaseClientStub) FetchRecentReleases(_ context.Context, repo string, _ int) ([]*GitHubRelease, error) {
	c.queries++
	c.repository = repo
	return c.releases, nil
}
func (c *releaseClientStub) DownloadFile(ctx context.Context, _ string, path string, _ int64) error {
	c.downloads++
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(path, c.archive, 0o600)
}
func (c *releaseClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return c.checksum, nil
}
func releaseFixture(version string) *GitHubRelease {
	tag := "v" + version
	name := fmt.Sprintf("sub2api_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	base := "https://github.com/" + releaseupdate.DefaultRepository + "/releases/download/" + tag + "/"
	return &GitHubRelease{TagName: tag, Assets: []GitHubAsset{{Name: name, BrowserDownloadURL: base + name}, {Name: "checksums.txt", BrowserDownloadURL: base + "checksums.txt"}}}
}
func updateFixture(t *testing.T) (*ReleaseUpdateService, *releaseClientStub, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux executable installation")
	}
	binary, err := os.ReadFile("/bin/busybox")
	if err != nil {
		binary, err = os.ReadFile("/bin/true")
	}
	require.NoError(t, err)
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	require.NoError(t, writer.WriteHeader(&tar.Header{Name: "sub2api", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}))
	_, err = writer.Write(binary)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, gz.Close())
	client := &releaseClientStub{releases: []*GitHubRelease{releaseFixture("1.10.0"), releaseFixture("1.11.0")}, archive: buffer.Bytes()}
	client.checksum = []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(client.archive), client.releases[0].Assets[0].Name))
	svc := NewReleaseUpdateService(client, "", "1.9.0", "release")
	svc.storage = t.TempDir()
	running := filepath.Join(t.TempDir(), "sub2api")
	require.NoError(t, os.WriteFile(running, []byte("running-original"), 0o755))
	svc.executable = func() (string, error) { return running, nil }
	return svc, client, running
}

func TestReleaseChecksOwnRepositorySortNumericallyAndNeverInstall(t *testing.T) {
	client := &releaseClientStub{releases: []*GitHubRelease{releaseFixture("1.9.0"), releaseFixture("1.10.0"), releaseFixture("1.11.0")}}
	draft := releaseFixture("2.0.0")
	draft.Draft = true
	client.releases = append(client.releases, draft)
	prerelease := releaseFixture("3.0.0")
	prerelease.Prerelease = true
	client.releases = append(client.releases, prerelease)
	svc := NewReleaseUpdateService(client, "", "1.9.0", "release")
	info, err := svc.Check(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, releaseupdate.DefaultRepository, client.repository)
	require.Equal(t, "1.11.0", info.LatestVersion)
	require.Len(t, info.Releases, 2)
	require.Equal(t, "1.11.0", info.Releases[0].Version)
	_, err = svc.Check(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, 1, client.queries)
	_, err = svc.Check(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, 2, client.queries)
	require.Zero(t, client.downloads)
}

func TestReleaseInstallUsesTheExplicitSelectionAndPersistsContainerUpdate(t *testing.T) {
	svc, client, running := updateFixture(t)
	require.NoError(t, svc.Install(context.Background(), "1.10.0"))
	require.NoError(t, svc.Install(context.Background(), "1.10.0"))
	require.Equal(t, 1, client.downloads)
	state, err := releaseupdate.ReadState(svc.storage)
	require.NoError(t, err)
	require.Equal(t, "1.10.0", state.Version)
	path, err := releaseupdate.Resolve(svc.storage, "1.9.0")
	require.NoError(t, err)
	require.FileExists(t, path)
	original, err := os.ReadFile(running)
	require.NoError(t, err)
	require.Equal(t, "running-original", string(original))
	info, err := svc.Check(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "1.10.0", info.PendingVersion)
}

func TestFailedChecksumPreservesTheRunningAndPreviouslyStagedVersions(t *testing.T) {
	svc, client, running := updateFixture(t)
	require.NoError(t, svc.Install(context.Background(), "1.10.0"))
	previous, err := os.ReadFile(filepath.Join(svc.storage, "current.json"))
	require.NoError(t, err)
	svc.current = "1.10.0"
	svc.pending = "" // Simulate the restarted application.
	client.checksum = []byte("wrong checksum")
	require.ErrorIs(t, svc.Install(context.Background(), "1.11.0"), ErrReleaseInstallFailed)
	after, err := os.ReadFile(filepath.Join(svc.storage, "current.json"))
	require.NoError(t, err)
	require.Equal(t, previous, after)
	original, err := os.ReadFile(running)
	require.NoError(t, err)
	require.Equal(t, "running-original", string(original))
}

func TestReleaseUpdateRejectsForeignPackagesDowngradesAndSourceBuilds(t *testing.T) {
	svc, client, _ := updateFixture(t)
	client.releases[0].Assets[0].BrowserDownloadURL = "https://github.com/Wei-Shaw/sub2api/releases/download/v1.10.0/package.tar.gz"
	require.ErrorIs(t, svc.Install(context.Background(), "1.10.0"), ErrReleaseNotAllowed)
	require.ErrorIs(t, svc.Install(context.Background(), "1.8.0"), ErrReleaseNotAllowed)
	svc.buildType = "source"
	require.ErrorIs(t, svc.Install(context.Background(), "1.11.0"), ErrReleaseUpdateUnsupported)
	require.Zero(t, client.downloads)
}

func TestNativeInstallKeepsABackupAndRetainsPendingVersionAcrossPageReloads(t *testing.T) {
	svc, _, running := updateFixture(t)
	svc.storage = ""
	require.NoError(t, svc.Install(context.Background(), "1.10.0"))
	backup, err := os.ReadFile(running + ".backup")
	require.NoError(t, err)
	require.Equal(t, "running-original", string(backup))
	require.NoError(t, releaseupdate.ValidateBinary(running, runtime.GOARCH))
	info, err := svc.Check(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "1.10.0", info.PendingVersion)
}
