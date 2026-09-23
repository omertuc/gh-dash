package data

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// errCacheDisabled is returned when there's nowhere to keep cache files.
var errCacheDisabled = errors.New("cache disabled")

// cacheDirOverride replaces the cache directory, see SetCacheDirForTesting.
var cacheDirOverride string

// SetCacheDirForTesting makes cache files be kept in dir. Tests don't keep
// cache files otherwise, so they never touch the user's cache.
func SetCacheDirForTesting(dir string) {
	cacheDirOverride = dir
}

// getCacheFilePath returns where the cache file of the given name is kept,
// under $XDG_CACHE_HOME/gh-dash (~/.cache/gh-dash by default).
func getCacheFilePath(filename string) (string, error) {
	if cacheDirOverride != "" {
		return filepath.Join(cacheDirOverride, filename), nil
	}
	if testing.Testing() {
		return "", errCacheDisabled
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, dashDir, filename), nil
}

// ReadCacheFile returns the contents of the cache file of the given name.
func ReadCacheFile(filename string) ([]byte, error) {
	path, err := getCacheFilePath(filename)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// WriteCacheFile replaces the cache file of the given name. The file is
// replaced atomically, so readers never see it half written.
func WriteCacheFile(filename string, contents []byte) error {
	path, err := getCacheFilePath(filename)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filename+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
