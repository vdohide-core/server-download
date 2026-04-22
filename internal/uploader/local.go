package uploader

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

type LocalUploadResult struct {
	VideoUploaded bool
	TotalSize     int64
}

type LocalUploadProgress struct {
	OnProgress func(completed, total int64)
}

// MoveFilesLocal moves video file to local storage path.
// Uses fileId as directory name (matching vdohide media path convention).
func MoveFilesLocal(
	storagePath string,
	fileId string,
	videoPath string,
	fileName string,
	progress *LocalUploadProgress,
) (*LocalUploadResult, error) {
	result := &LocalUploadResult{}
	destDir := filepath.Join(storagePath, fileId)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	var totalSize int64 = 0
	if videoPath != "" {
		info, err := os.Stat(videoPath)
		if err == nil {
			totalSize = info.Size()
		}
	}

	var completedBytes int64 = 0

	if videoPath != "" {
		dest := filepath.Join(destDir, fileName)
		size, err := copyFile(videoPath, dest)
		if err != nil {
			return nil, fmt.Errorf("failed to move video: %w", err)
		}

		os.Remove(videoPath)

		completedBytes += size
		if progress != nil && progress.OnProgress != nil {
			progress.OnProgress(completedBytes, totalSize)
		}

		result.VideoUploaded = true
		result.TotalSize += size
		log.Printf("✅ Moved %s", fileName)
	}

	log.Printf("✅ All files moved to %s (%.2f MB total)",
		destDir, float64(result.TotalSize)/1024/1024)

	return result, nil
}

func copyFile(src, dst string) (int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	size, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return 0, err
	}

	if err := dstFile.Close(); err != nil {
		return size, fmt.Errorf("failed to close/flush: %w", err)
	}
	return size, nil
}
