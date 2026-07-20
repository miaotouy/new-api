package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiskCachePathResolution(t *testing.T) {
	originalConfig := GetDiskCacheConfig()
	t.Cleanup(func() {
		SetDiskCacheConfig(originalConfig)
	})

	t.Run("system temp directory by default", func(t *testing.T) {
		config := originalConfig
		config.Path = ""
		SetDiskCacheConfig(config)

		assert.Equal(t, os.TempDir(), getDiskCacheBasePath())
		assert.Equal(t, filepath.Join(os.TempDir(), diskCacheDir), GetDiskCacheDir())
	})

	t.Run("configured directory", func(t *testing.T) {
		cachePath := t.TempDir()
		config := originalConfig
		config.Path = cachePath
		SetDiskCacheConfig(config)

		assert.Equal(t, cachePath, getDiskCacheBasePath())
		assert.Equal(t, filepath.Join(cachePath, diskCacheDir), GetDiskCacheDir())
	})
}
