package downloader

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// directVideoExtensions คือ extension ที่ถือเป็น direct video file (ไม่ใช่ HLS)
var directVideoExtensions = map[string]bool{
	"mp4":  true,
	"mkv":  true,
	"avi":  true,
	"mov":  true,
	"webm": true,
	"flv":  true,
	"wmv":  true,
	"ts":   true,
	"m4v":  true,
	"3gp":  true,
	"mpg":  true,
	"mpeg": true,
}

// IsDirectVideoURL ตรวจสอบว่า URL เป็น direct video file (ไม่ใช่ m3u8/HLS)
func IsDirectVideoURL(rawURL string) bool {
	// ตัด query string ออก
	path := strings.Split(rawURL, "?")[0]
	path = strings.Split(path, "#")[0]

	parts := strings.Split(strings.ToLower(path), ".")
	if len(parts) < 2 {
		return false
	}
	ext := parts[len(parts)-1]
	return directVideoExtensions[ext]
}

// DownloadDirectFile downloads a direct video URL to outputPath with progress callback.
// progressFn(bytesDownloaded, totalBytes) — totalBytes may be 0 if Content-Length unknown.
func DownloadDirectFile(rawURL, outputPath string, progressFn func(done, total int64)) error {
	log.Printf("📥 [direct] Starting download: %s → %s", rawURL, outputPath)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// browser-like headers
	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 0} // no timeout — large files
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	total := resp.ContentLength // -1 if unknown

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	buf := make([]byte, 512*1024) // 512 KB buffer
	var downloaded int64
	lastReport := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write: %w", writeErr)
			}
			downloaded += int64(n)

			// Report progress max ทุก 1 วินาที
			if time.Since(lastReport) >= time.Second {
				if progressFn != nil {
					t := total
					if t < 0 {
						t = 0
					}
					progressFn(downloaded, t)
				}
				if total > 0 {
					pct := float64(downloaded) / float64(total) * 100
					log.Printf("⬇️  [direct] %.1f%% (%.2f / %.2f MB)",
						pct,
						float64(downloaded)/1024/1024,
						float64(total)/1024/1024)
				} else {
					log.Printf("⬇️  [direct] %.2f MB downloaded", float64(downloaded)/1024/1024)
				}
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}
	}

	// final progress report
	if progressFn != nil {
		t := total
		if t < 0 {
			t = 0
		}
		progressFn(downloaded, t)
	}
	log.Printf("✅ [direct] Download complete: %.2f MB", float64(downloaded)/1024/1024)
	return nil
}
