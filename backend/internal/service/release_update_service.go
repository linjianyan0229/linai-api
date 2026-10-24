package service

import (
	"bufio"
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/releaseupdate"
)

var (
	ErrReleaseCheckFailed       = infraerrors.ServiceUnavailable("RELEASE_CHECK_FAILED", "could not check the configured GitHub releases")
	ErrReleaseNotAllowed        = infraerrors.BadRequest("RELEASE_NOT_ALLOWED", "select an available newer stable release with a matching update package")
	ErrReleaseUpdateUnsupported = infraerrors.BadRequest("RELEASE_UPDATE_UNSUPPORTED", "online updates require a Linux release build")
	ErrReleaseInstallFailed     = infraerrors.InternalServer("RELEASE_INSTALL_FAILED", "release installation failed; the running version was preserved")
	ErrReleaseRestartRequired   = infraerrors.Conflict("RELEASE_RESTART_REQUIRED", "restart the staged release before installing another version")
	releaseRepositoryPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

type AvailableRelease struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	PublishedAt string `json:"published_at"`
	URL         string `json:"url"`
	CanInstall  bool   `json:"can_install"`
	assets      []GitHubAsset
	tag         string
}

type ReleaseUpdateInfo struct {
	CurrentVersion    string             `json:"current_version"`
	LatestVersion     string             `json:"latest_version"`
	Repository        string             `json:"repository"`
	HasUpdate         bool               `json:"has_update"`
	CanUpdate         bool               `json:"can_update"`
	UnsupportedReason string             `json:"unsupported_reason,omitempty"`
	Deployment        string             `json:"deployment"`
	PendingVersion    string             `json:"pending_version,omitempty"`
	CheckedAt         time.Time          `json:"checked_at"`
	Releases          []AvailableRelease `json:"releases"`
}

type ReleaseUpdateService struct {
	client     GitHubReleaseClient
	repository string
	current    string
	buildType  string
	storage    string
	executable func() (string, error)
	mu         sync.Mutex
	installMu  sync.Mutex
	cache      *ReleaseUpdateInfo
	pending    string
}

func NewReleaseUpdateService(client GitHubReleaseClient, repository, version, buildType string) *ReleaseUpdateService {
	if repository == "" {
		repository = releaseupdate.DefaultRepository
	}
	return &ReleaseUpdateService{client: client, repository: repository, current: version, buildType: buildType, storage: os.Getenv("UPDATE_STORAGE_DIR"), executable: os.Executable}
}

func (s *ReleaseUpdateService) Check(ctx context.Context, force bool) (*ReleaseUpdateInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !releaseRepositoryPattern.MatchString(s.repository) {
		return nil, ErrReleaseCheckFailed
	}
	if force || s.cache == nil || time.Since(s.cache.CheckedAt) >= 20*time.Minute {
		releases, err := s.client.FetchRecentReleases(ctx, s.repository, 30)
		if err != nil {
			return nil, ErrReleaseCheckFailed.WithCause(err)
		}
		info := &ReleaseUpdateInfo{CurrentVersion: s.current, LatestVersion: s.current, Repository: s.repository, CheckedAt: time.Now().UTC(), Releases: []AvailableRelease{}}
		seen := map[string]bool{}
		for _, release := range releases {
			if release == nil || release.Draft || release.Prerelease {
				continue
			}
			version := releaseupdate.Version(release.TagName)
			if version == "" || seen[version] || !releaseupdate.Newer(version, s.current) {
				continue
			}
			seen[version] = true
			item := AvailableRelease{Version: version, Name: release.Name, Notes: release.Body, PublishedAt: release.PublishedAt, URL: fmt.Sprintf("https://github.com/%s/releases/tag/%s", s.repository, release.TagName), assets: release.Assets, tag: release.TagName}
			_, _, item.CanInstall = s.assets(item)
			info.Releases = append(info.Releases, item)
			if releaseupdate.Newer(version, info.LatestVersion) {
				info.LatestVersion = version
			}
		}
		sort.Slice(info.Releases, func(i, j int) bool { return releaseupdate.Newer(info.Releases[i].Version, info.Releases[j].Version) })
		info.HasUpdate = len(info.Releases) > 0
		s.cache = info
	}
	info := *s.cache
	info.PendingVersion = s.pending
	info.CanUpdate = runtime.GOOS == "linux" && s.buildType == "release" && releaseupdate.Version(s.current) != ""
	if !info.CanUpdate {
		info.UnsupportedReason = "release_build_required"
	}
	info.Deployment = "binary"
	if s.storage != "" {
		info.Deployment = "container"
		if state, err := releaseupdate.ReadState(s.storage); err == nil && releaseupdate.Newer(state.Version, s.current) {
			info.PendingVersion = state.Version
		}
	}
	return &info, nil
}

func (s *ReleaseUpdateService) assets(release AvailableRelease) (GitHubAsset, GitHubAsset, bool) {
	name := fmt.Sprintf("sub2api_%s_%s_%s.tar.gz", release.Version, runtime.GOOS, runtime.GOARCH)
	var archive, checksum GitHubAsset
	for _, asset := range release.assets {
		if asset.Name != name && asset.Name != "checksums.txt" {
			continue
		}
		// Assets must belong to this repository and selected tag. Never accept
		// arbitrary URLs supplied through a release body or an update request.
		if asset.BrowserDownloadURL != fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", s.repository, release.tag, asset.Name) {
			continue
		}
		if asset.Name == name {
			archive = asset
		} else {
			checksum = asset
		}
	}
	return archive, checksum, archive.Name != "" && checksum.Name != ""
}

func (s *ReleaseUpdateService) Install(ctx context.Context, version string) error {
	if !s.installMu.TryLock() {
		return ErrSystemOperationBusy
	}
	defer s.installMu.Unlock()
	info, err := s.Check(ctx, true)
	if err != nil {
		return err
	}
	if !info.CanUpdate {
		return ErrReleaseUpdateUnsupported
	}
	var selected *AvailableRelease
	for i := range info.Releases {
		if info.Releases[i].Version == releaseupdate.Version(version) {
			selected = &info.Releases[i]
			break
		}
	}
	if selected == nil || !selected.CanInstall {
		return ErrReleaseNotAllowed
	}
	if info.PendingVersion == selected.Version {
		return nil
	}
	if info.PendingVersion != "" {
		return ErrReleaseRestartRequired
	}
	archive, checksum, ok := s.assets(*selected)
	if !ok || archive.Size > 256<<20 {
		return ErrReleaseNotAllowed
	}
	executable, err := s.executable()
	if err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	directory := filepath.Dir(executable)
	if s.storage != "" {
		directory = s.storage
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return ErrReleaseInstallFailed.WithCause(err)
		}
	}
	stage, err := os.MkdirTemp(directory, ".linai-update-*")
	if err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	defer os.RemoveAll(stage)
	packagePath := filepath.Join(stage, "release.tar.gz")
	if err := s.client.DownloadFile(ctx, archive.BrowserDownloadURL, packagePath, 256<<20); err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	checksums, err := s.client.FetchChecksumFile(ctx, checksum.BrowserDownloadURL)
	if err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	actual, err := releaseupdate.Digest(packagePath)
	if err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	valid := false
	scanner := bufio.NewScanner(strings.NewReader(string(checksums)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == archive.Name && len(fields[0]) == 64 && subtle.ConstantTimeCompare([]byte(strings.ToLower(fields[0])), []byte(actual)) == 1 {
			valid = true
		}
	}
	if !valid || scanner.Err() != nil {
		return ErrReleaseInstallFailed.WithCause(fmt.Errorf("release checksum mismatch"))
	}
	binary := filepath.Join(stage, "sub2api")
	if err := releaseupdate.ExtractBinary(packagePath, binary); err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	if err := releaseupdate.ValidateBinary(binary, runtime.GOARCH); err != nil {
		return ErrReleaseInstallFailed.WithCause(err)
	}
	if s.storage != "" {
		if err := releaseupdate.Activate(directory, binary, selected.Version, s.repository); err != nil {
			return ErrReleaseInstallFailed.WithCause(err)
		}
	} else {
		// Keep the running binary available throughout installation. Rename on
		// the same filesystem atomically replaces it only after backup succeeds.
		if err := copyReleaseBackup(executable, filepath.Join(stage, "backup")); err != nil {
			return ErrReleaseInstallFailed.WithCause(err)
		}
		if err := os.Rename(filepath.Join(stage, "backup"), executable+".backup"); err != nil {
			return ErrReleaseInstallFailed.WithCause(err)
		}
		if err := os.Rename(binary, executable); err != nil {
			return ErrReleaseInstallFailed.WithCause(err)
		}
	}
	s.mu.Lock()
	s.pending = selected.Version
	s.mu.Unlock()
	return nil
}

func copyReleaseBackup(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
