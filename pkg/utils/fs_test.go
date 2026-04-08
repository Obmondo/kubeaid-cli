// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetParentDirPath(t *testing.T) {
	assert.Equal(t, "/home/user", GetParentDirPath("/home/user/file.txt"))
	assert.Equal(t, "/home/user/deep", GetParentDirPath("/home/user/deep/file.txt"))
	assert.Equal(t, ".", GetParentDirPath("file.txt"))
}

func TestGetParentDirPath_RootLevel(t *testing.T) {
	result := GetParentDirPath("/foo")
	assert.Equal(t, "/", result)
	assert.Equal(t, filepath.Dir("/foo"), result)
}

func TestToAbsolutePath_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	result := ToAbsolutePath(context.Background(), "~/documents/file.txt")
	assert.Equal(t, home+"/documents/file.txt", result)
}

func TestToAbsolutePath_RelativePath(t *testing.T) {
	cwd, _ := os.Getwd()

	result := ToAbsolutePath(context.Background(), "some/relative/path")
	assert.Equal(t, filepath.Join(cwd, "some/relative/path"), result)
}

func TestToAbsolutePath_AlreadyAbsolute(t *testing.T) {
	result := ToAbsolutePath(context.Background(), "/usr/local/bin")
	assert.Equal(t, "/usr/local/bin", result)
}

func TestMustMoveFile(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "source.txt")
	dst := filepath.Join(dir, "dest.txt")

	require.NoError(t, os.WriteFile(src, []byte("hello"), 0o600))

	MustMoveFile(context.Background(), src, dst)

	// source should be gone
	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err))

	// dest should have the content
	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestMustMoveFileOverwrites(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "new.txt")
	dst := filepath.Join(dir, "existing.txt")

	require.NoError(t, os.WriteFile(src, []byte("new content"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old content"), 0o600))

	MustMoveFile(context.Background(), src, dst)

	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(content))
}
