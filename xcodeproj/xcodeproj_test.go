package xcodeproj

import (
	"path/filepath"
	"testing"

	"github.com/bitrise-tools/xcode-project/testhelper"
	"github.com/stretchr/testify/require"
)

func TestSchemes(t *testing.T) {
	dir := testhelper.GitCloneIntoTmpDir(t, "https://github.com/bitrise-samples/xcode-project-test.git")
	project, err := Open(filepath.Join(dir, "XcodeProj.xcodeproj"))
	require.NoError(t, err)

	schemes, err := project.Schemes()
	require.NoError(t, err)
	require.Equal(t, 2, len(schemes))

	require.Equal(t, "ProjectScheme", schemes[0].Name)
	require.Equal(t, "ProjectTodayExtensionScheme", schemes[1].Name)
}

func TestOpenXcodeproj(t *testing.T) {
	dir := testhelper.GitCloneIntoTmpDir(t, "https://github.com/bitrise-samples/xcode-project-test.git")
	project, err := Open(filepath.Join(dir, "XcodeProj.xcodeproj"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "XcodeProj.xcodeproj"), project.Path)
	require.Equal(t, "XcodeProj", project.Name)
}

func TestIsXcodeProj(t *testing.T) {
	require.True(t, IsXcodeProj("./BitriseSample.xcodeproj"))
	require.False(t, IsXcodeProj("./BitriseSample.xcworkspace"))
}
