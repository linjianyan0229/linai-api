//go:build unit

package releaseupdate

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStableVersionOrdering(t *testing.T) {
	require.True(t, Newer("v1.10.0", "1.9.9"))
	for _, value := range []string{"", "1.2", "01.2.3", "1.2.3-beta.1", "1.2.3+build", "../1.2.3"} {
		require.Empty(t, Version(value))
	}
	require.False(t, Newer("1.2.3", "1.2.3"))
	require.False(t, Newer("1.2.3", "2.0.0"))
}

func TestPersistentUpdateSurvivesLauncherRecreationAndYieldsToNewerImages(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "staged")
	require.NoError(t, os.WriteFile(binary, []byte("verified executable fixture"), 0o755))
	require.NoError(t, Activate(directory, binary, "1.10.0", DefaultRepository))
	state, err := ReadState(directory)
	require.NoError(t, err)
	path, err := Resolve(directory, "1.9.0")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(directory, state.File), path)
	path, err = Resolve(directory, "1.10.0")
	require.NoError(t, err)
	require.Empty(t, path)
	path, err = Resolve(directory, "1.11.0")
	require.NoError(t, err)
	require.Empty(t, path)
	require.NoError(t, os.WriteFile(filepath.Join(directory, state.File), []byte("corrupt"), 0o755))
	_, err = Resolve(directory, "1.9.0")
	require.ErrorContains(t, err, "checksum")
}

func TestManagedStateCannotSelectArbitraryFiles(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "current.json"), []byte(`{"version":"1.2.3","file":"../../elsewhere","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`), 0o600))
	_, err := Resolve(directory, "1.0.0")
	require.Error(t, err)
}

func TestArchiveOnlyAcceptsOneRegularRootBinary(t *testing.T) {
	for _, scenario := range []string{"valid", "symlink", "duplicate", "traversal", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "release.tar.gz")
			file, err := os.Create(path)
			require.NoError(t, err)
			gz := gzip.NewWriter(file)
			writer := tar.NewWriter(gz)
			header := &tar.Header{Name: "sub2api", Typeflag: tar.TypeReg, Size: 3, Mode: 0o755}
			switch scenario {
			case "symlink":
				header.Typeflag = tar.TypeSymlink
				header.Size = 0
				header.Linkname = "/bin/sh"
			case "traversal":
				header.Name = "../../escape"
			case "oversized":
				header.Size = MaxBinarySize + 1
			}
			require.NoError(t, writer.WriteHeader(header))
			if header.Size == 3 {
				_, err = writer.Write([]byte("new"))
				require.NoError(t, err)
			}
			if scenario == "duplicate" {
				require.NoError(t, writer.WriteHeader(header))
				_, err = writer.Write([]byte("new"))
				require.NoError(t, err)
			}
			_ = writer.Close()
			require.NoError(t, gz.Close())
			require.NoError(t, file.Close())
			err = ExtractBinary(path, filepath.Join(directory, "installed"))
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
