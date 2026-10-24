// Package releaseupdate stages verified release binaries and resolves persistent
// container updates without requiring access to the Docker daemon.
package releaseupdate

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

const DefaultRepository = "linjianyan0229/linai-api"
const MaxBinarySize int64 = 512 << 20

type State struct {
	Version    string `json:"version"`
	Repository string `json:"repository"`
	File       string `json:"file"`
	SHA256     string `json:"sha256"`
}

// Version accepts only complete stable versions. Canonical semver comparison
// avoids lexical mistakes such as treating 1.10.0 as older than 1.9.0.
func Version(value string) string {
	v := "v" + strings.TrimPrefix(strings.TrimSpace(value), "v")
	if !semver.IsValid(v) || semver.Canonical(v) != v || semver.Prerelease(v) != "" || semver.Build(v) != "" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}

func Newer(candidate, current string) bool {
	a, b := Version(candidate), Version(current)
	return a != "" && b != "" && semver.Compare("v"+a, "v"+b) > 0
}

func Digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ValidateBinary(path, arch string) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("release executable is not a Linux ELF binary: %w", err)
	}
	defer f.Close()
	wanted := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}
	if f.Class != elf.ELFCLASS64 || wanted[arch] == 0 || f.Machine != wanted[arch] || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) {
		return fmt.Errorf("release executable does not match the current platform")
	}
	return nil
}

// ExtractBinary never writes archive paths to disk. Only one regular root-level
// executable is accepted; symlinks, duplicate binaries and oversized files fail.
func ExtractBinary(archive, destination string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, MaxBinarySize+(32<<20)))
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "sub2api" && header.Name != "./sub2api" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > MaxBinarySize {
			return fmt.Errorf("release archive has an invalid executable")
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(out, reader, header.Size)
		syncErr := out.Sync()
		closeErr := out.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("release archive has no executable")
	}
	return nil
}

func ReadState(directory string) (*State, error) {
	f, err := os.Open(filepath.Join(directory, "current.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var state State
	if err := json.NewDecoder(io.LimitReader(f, 4096)).Decode(&state); err != nil {
		return nil, err
	}
	if state.Version == "" || Version(state.Version) != state.Version || len(state.SHA256) != 64 || state.File != state.Version+"-"+state.SHA256+".bin" {
		return nil, fmt.Errorf("invalid installed release metadata")
	}
	if _, err := hex.DecodeString(state.SHA256); err != nil {
		return nil, err
	}
	return &state, nil
}

// Resolve ignores overrides older than a newly pulled base image. Corrupt or
// missing overrides return an error so the launcher can fall back to its image.
func Resolve(directory, baseVersion string) (string, error) {
	if directory == "" {
		return "", nil
	}
	state, err := ReadState(directory)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !Newer(state.Version, baseVersion) {
		return "", nil
	}
	path := filepath.Join(directory, state.File)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("installed release is not an executable file")
	}
	digest, err := Digest(path)
	if err != nil {
		return "", err
	}
	if digest != state.SHA256 {
		return "", fmt.Errorf("installed release checksum mismatch")
	}
	return filepath.Abs(path)
}

// Activate atomically publishes the pointer only after the complete binary has
// reached the persistent volume. A failed download never changes this pointer.
func Activate(directory, binary, version, repository string) error {
	if version == "" || Version(version) != version {
		return fmt.Errorf("invalid release version")
	}
	digest, err := Digest(binary)
	if err != nil {
		return err
	}
	state := State{Version: version, Repository: repository, File: version + "-" + digest + ".bin", SHA256: digest}
	if err := os.Rename(binary, filepath.Join(directory, state.File)); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".current-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, "current.json"))
}
