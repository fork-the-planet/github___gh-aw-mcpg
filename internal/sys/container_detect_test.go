package sys

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempFile creates a temporary file with the given content and returns its path.
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestDetectContainerIDWithPaths_DockerEnvOnly tests Method 1 (/.dockerenv presence)
// when there are no cgroup files that yield a container ID.
func TestDetectContainerIDWithPaths_DockerEnvOnly(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := writeTempFile(t, dir, ".dockerenv", "")

	// Point cgroup paths at non-existent files so extractContainerIDFromCgroupFiles returns "".
	detected, id := detectContainerIDWithPaths(dockerEnv, []string{filepath.Join(dir, "nonexistent")})

	assert.True(t, detected, "should detect container via /.dockerenv")
	assert.Empty(t, id, "container ID should be empty when cgroup yields no ID")
}

// TestDetectContainerIDWithPaths_DockerEnvWithCgroupID tests Method 1 where the
// cgroup file also contains a parseable container ID.
func TestDetectContainerIDWithPaths_DockerEnvWithCgroupID(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := writeTempFile(t, dir, ".dockerenv", "")
	cgroupContent := "0::/docker/abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via /.dockerenv")
	assert.Equal(t, "abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678", id)
}

// TestDetectContainerIDWithPaths_CgroupDockerWithID tests Method 2 where the
// cgroup file contains a docker indicator and a parseable container ID.
func TestDetectContainerIDWithPaths_CgroupDockerWithID(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/docker/abcdef123456\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via cgroup docker indicator")
	assert.Equal(t, "abcdef123456", id)
}

// TestDetectContainerIDWithPaths_CgroupContainerdWithID tests Method 2 where the
// cgroup file contains a containerd indicator and a parseable container ID.
func TestDetectContainerIDWithPaths_CgroupContainerdWithID(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/containerd/fedcba654321\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via cgroup containerd indicator")
	assert.Equal(t, "fedcba654321", id)
}

// TestDetectContainerIDWithPaths_CgroupKubepodsNoID tests Method 2 where the
// cgroup file contains a kubepods indicator but no parseable container ID
// (extractContainerIDFromContent returns "").
func TestDetectContainerIDWithPaths_CgroupKubepodsNoID(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	// kubepods is a container indicator but does not follow the docker/containerd ID format.
	cgroupContent := "0::/kubepods/besteffort/pod123abc\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via kubepods indicator")
	assert.Empty(t, id, "container ID should be empty for kubepods cgroup (no ID extractable)")
}

// TestDetectContainerIDWithPaths_CgroupDockerShortIDNoExtraction tests Method 2
// where the cgroup line has a docker indicator but the candidate ID is too short
// (< 12 chars), so no ID is returned but container IS detected.
func TestDetectContainerIDWithPaths_CgroupDockerShortIDNoExtraction(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/docker/short\n" // "short" is only 5 chars, below 12-char minimum
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via docker indicator even with short ID segment")
	assert.Empty(t, id, "container ID should be empty when cgroup path segment is too short")
}

// TestDetectContainerIDWithPaths_FirstCgroupFileMissing tests that when the
// first cgroup path is missing, the function falls back to the second one.
func TestDetectContainerIDWithPaths_FirstCgroupFileMissing(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/docker/abcdef123456\n"
	secondCgroup := writeTempFile(t, dir, "cgroup2", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{
		filepath.Join(dir, "nonexistent-cgroup"), // first path doesn't exist
		secondCgroup,
	})

	assert.True(t, detected, "should detect container via second cgroup file")
	assert.Equal(t, "abcdef123456", id)
}

// TestDetectContainerIDWithPaths_NoCgroupIndicators tests the path where no
// container indicators are found and env var is not set (host environment).
func TestDetectContainerIDWithPaths_NoCgroupIndicators(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/user.slice/user-1000.slice\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.False(t, detected, "should not detect container on host")
	assert.Empty(t, id)
}

// TestDetectContainerIDWithPaths_NoCgroupFiles tests that when all cgroup paths are
// unreadable and dockerenv does not exist the function falls through to env var check.
func TestDetectContainerIDWithPaths_NoCgroupFiles(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{
		filepath.Join(dir, "nonexistent1"),
		filepath.Join(dir, "nonexistent2"),
	})

	assert.False(t, detected)
	assert.Empty(t, id)
}

// TestDetectContainerIDWithPaths_EnvVarOverride tests Method 3 where neither
// /.dockerenv nor cgroup indicators are present but RUNNING_IN_CONTAINER=true.
func TestDetectContainerIDWithPaths_EnvVarOverride(t *testing.T) {
	t.Setenv("RUNNING_IN_CONTAINER", "true")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via RUNNING_IN_CONTAINER env var")
	assert.Empty(t, id, "env-var detection never provides a container ID")
}

// TestDetectContainerIDWithPaths_LxcIndicator tests Method 2 with lxc indicator.
func TestDetectContainerIDWithPaths_LxcIndicator(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "0::/lxc/mycontainer\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected, "should detect container via lxc indicator")
	assert.Empty(t, id, "lxc cgroup paths don't match docker/containerd ID extraction")
}

// TestDetectContainerIDWithPaths_MultipleIndicatorLines tests that the first
// matching indicator line in a multi-line cgroup file yields the ID.
func TestDetectContainerIDWithPaths_MultipleIndicatorLines(t *testing.T) {
	unsetEnvForTest(t, "RUNNING_IN_CONTAINER")

	dir := t.TempDir()
	dockerEnv := filepath.Join(dir, "nonexistent-dockerenv")
	cgroupContent := "12:blkio:/\n11:devices:/docker/aabbccddeeff\n10:memory:/\n"
	cgroupFile := writeTempFile(t, dir, "cgroup", cgroupContent)

	detected, id := detectContainerIDWithPaths(dockerEnv, []string{cgroupFile})

	assert.True(t, detected)
	assert.Equal(t, "aabbccddeeff", id)
}

// TestExtractContainerIDFromCgroupFiles_ReturnsFirstFound tests that
// extractContainerIDFromCgroupFiles returns the ID from the first file that yields one.
func TestExtractContainerIDFromCgroupFiles_ReturnsFirstFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first := writeTempFile(t, dir, "cgroup1", "0::/docker/aabbccdd1234\n")
	second := writeTempFile(t, dir, "cgroup2", "0::/docker/11223344aabb\n")

	id := extractContainerIDFromCgroupFiles([]string{first, second})
	assert.Equal(t, "aabbccdd1234", id, "should return ID from first matching file")
}

// TestExtractContainerIDFromCgroupFiles_SkipsEmptyFile tests that a file with
// no container ID is skipped and the next file is tried.
func TestExtractContainerIDFromCgroupFiles_SkipsEmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	noMatch := writeTempFile(t, dir, "cgroup1", "0::/user.slice\n")
	match := writeTempFile(t, dir, "cgroup2", "0::/docker/aabbccdd1234\n")

	id := extractContainerIDFromCgroupFiles([]string{noMatch, match})
	assert.Equal(t, "aabbccdd1234", id, "should return ID from second file when first has no match")
}

// TestExtractContainerIDFromCgroupFiles_AllEmpty tests that "" is returned when
// no files yield a container ID.
func TestExtractContainerIDFromCgroupFiles_AllEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	f1 := writeTempFile(t, dir, "cgroup1", "0::/\n")
	f2 := writeTempFile(t, dir, "cgroup2", "0::/user.slice\n")

	id := extractContainerIDFromCgroupFiles([]string{f1, f2})
	assert.Empty(t, id)
}

// TestExtractContainerIDFromCgroupFiles_NilPaths tests the nil-slice edge case.
func TestExtractContainerIDFromCgroupFiles_NilPaths(t *testing.T) {
	t.Parallel()
	id := extractContainerIDFromCgroupFiles(nil)
	assert.Empty(t, id)
}

// TestExtractContainerIDFromCgroupFiles_EmptyPaths tests the empty-slice edge case.
func TestExtractContainerIDFromCgroupFiles_EmptyPaths(t *testing.T) {
	t.Parallel()
	id := extractContainerIDFromCgroupFiles([]string{})
	assert.Empty(t, id)
}

// TestExtractContainerIDFromCgroupFiles_NonexistentPaths tests that missing files
// are silently skipped and "" is returned when none exist.
func TestExtractContainerIDFromCgroupFiles_NonexistentPaths(t *testing.T) {
	t.Parallel()
	id := extractContainerIDFromCgroupFiles([]string{"/nonexistent/a", "/nonexistent/b"})
	assert.Empty(t, id)
}
