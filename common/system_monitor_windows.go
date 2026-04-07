//go:build windows

package common

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// GetDiskSpaceInfo 获取缓存目录所在磁盘的空间信息 (Windows)
func GetDiskSpaceInfo() DiskSpaceInfo {
	cachePath := GetDiskCachePath()
	if cachePath == "" {
		// Use the executable's directory instead of os.TempDir(),
		// so we check the disk where new-api is actually deployed,
		// not the system temp dir (often C:\ which may be nearly full).
		exePath, err := os.Executable()
		if err == nil {
			cachePath = filepath.Dir(exePath)
		} else {
			cachePath = "."
		}
	}

	info := DiskSpaceInfo{}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	pathPtr, err := syscall.UTF16PtrFromString(cachePath)
	if err != nil {
		return info
	}

	ret, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)

	if ret == 0 {
		return info
	}

	info.Total = totalBytes
	info.Free = freeBytesAvailable
	info.Used = totalBytes - totalFreeBytes

	if info.Total > 0 {
		info.UsedPercent = float64(info.Used) / float64(info.Total) * 100
	}

	return info
}
