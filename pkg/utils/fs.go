// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// InitTempDir creates the temp dir where KubeAid Bootstrap Script will clone
// repos. If the dir already exists it is reused.
func InitTempDir(ctx context.Context) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", constants.TempDirectory),
	})

	if info, err := os.Stat(constants.TempDirectory); err == nil && info.IsDir() {
		slog.InfoContext(ctx, "Skipped creating temp dir, since it already exists")
		return nil
	}

	if err := os.MkdirAll(constants.TempDirectory, 0o750); err != nil {
		return fmt.Errorf("creating temp dir %s: %w", constants.TempDirectory, err)
	}

	slog.InfoContext(ctx, "Created temp dir")
	return nil
}

// Returns path to the parent dir of the given file.
func GetParentDirPath(filePath string) string {
	return filepath.Dir(filePath)
}

// CreateIntermediateDirsForFile creates intermediate directories which don't
// exist for the given file path.
func CreateIntermediateDirsForFile(filePath string) error {
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, 0o750); err != nil {
		return fmt.Errorf("creating intermediate dirs for %s: %w", filePath, err)
	}
	return nil
}

// Returns path to the directory where the KubeAid repository is cloned.
func GetKubeAidDir() string {
	return git.GetRepoDir(config.ParsedGeneralConfig.Forks.KubeaidFork.ParsedURL)
}

// Returns path to the directory where the KubeAid Config repository is cloned.
func GetKubeAidConfigDir() string {
	return git.GetRepoDir(config.ParsedGeneralConfig.Forks.KubeaidConfigFork.ParsedURL)
}

// Returns path to the directory containing cluster specific config, in the KubeAid Config dir.
func GetClusterDir() string {
	return path.Join(
		GetKubeAidConfigDir(),
		"k8s",
		config.ParsedGeneralConfig.Forks.KubeaidConfigFork.Directory,
	)
}

// Returns the path to the local temp directory, where contents of the given blob storage bucket
// will be / is downloaded.
func GetDownloadedStorageBucketContentsDir(bucketName string) string {
	return path.Join(constants.TempDirectory, "buckets", bucketName)
}

// ToAbsolutePath returns the canonical version of the given path. A bare "~"
// or a "~/" prefix is expanded to the user's home directory.
// REFER : https://www.gnu.org/software/bash/manual/html_node/Tilde-Expansion.html.
func ToAbsolutePath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		if p == "~" {
			return homeDir, nil
		}
		return filepath.Join(homeDir, p[2:]), nil
	}

	absolutePath, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("canonicalizing path %s: %w", p, err)
	}
	return filepath.Clean(absolutePath), nil
}

var renameFn = os.Rename

// MoveFile moves the source file to the destination file. It first tries
// os.Rename (atomic on the same filesystem); on failure it falls back to
// copy + delete, which works across filesystems too.
func MoveFile(src, dst string) (err error) {
	// Fast path: atomic rename when src and dst share a filesystem.
	if err = renameFn(src, dst); err == nil {
		return nil
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file %s: %w", src, err)
	}
	defer sourceFile.Close()

	destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening destination file %s: %w", dst, err)
	}
	defer func() {
		destFile.Close()
		if err != nil {
			// On any failure past this point, remove the partial dst.
			_ = os.Remove(dst)
		}
	}()

	if _, err = io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}

	if err = destFile.Sync(); err != nil {
		return fmt.Errorf("syncing destination file %s: %w", dst, err)
	}

	if err = os.Remove(src); err != nil {
		return fmt.Errorf("removing source file %s: %w", src, err)
	}

	return nil
}
