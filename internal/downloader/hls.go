package downloader

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxConcurrentDownloads   = 10
	MaxConcurrentRateLimited = 3
	MaxRetries               = 8
	RetryDelay               = 2 * time.Second
	RateLimitDelay           = 200 * time.Millisecond
	RateLimitBackoff         = 30 * time.Second
)

var rateLimiter = make(chan struct{}, 1)

func init() {
	rateLimiter <- struct{}{}
}

func acquireRateLimit(rateLimited bool) {
	if !rateLimited {
		return
	}
	<-rateLimiter
	go func() {
		time.Sleep(RateLimitDelay)
		rateLimiter <- struct{}{}
	}()
}

// DownloadProgress tracks download progress
type DownloadProgress struct {
	Total      int
	Completed  int32
	Failed     int32
	OnProgress func(downloaded, total int)
}

// HLSDownloadResult contains the result of HLS download
type HLSDownloadResult struct {
	OutputDir      string
	Resolution     string
	ResolutionFull string
	SegmentCount   int
	TotalSize      int64
	SegmentFiles   []string
}

// DownloadHLSSegments downloads HLS stream segments as .ts files
func DownloadHLSSegments(m3u8URL string, outputDir string, progress *DownloadProgress) (*HLSDownloadResult, error) {
	log.Printf("📥 Parsing master playlist: %s", m3u8URL)

	rateLimited := IsRateLimitedDomain(m3u8URL)
	concurrency := MaxConcurrentDownloads
	if rateLimited {
		concurrency = MaxConcurrentRateLimited
		log.Printf("🐢 Rate-limited domain detected, using %d concurrent downloads with throttling", concurrency)
	} else {
		log.Printf("🚀 Normal domain, using %d concurrent downloads", concurrency)
	}

	streams, err := ParseMasterPlaylist(m3u8URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse master playlist: %w", err)
	}

	selected := SelectHighestResolution(streams)
	log.Printf("✅ Selected resolution: %s", selected.Resolution)

	resolutionFolder := extractResolutionFolder(selected.Resolution)

	segmentsDir := filepath.Join(outputDir, "segments")
	if err := os.MkdirAll(segmentsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	log.Printf("📥 Parsing segment playlist: %s", selected.URL)

	segments, err := ParseSegmentPlaylist(selected.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse segment playlist: %w", err)
	}

	log.Printf("📦 Found %d segments to download", len(segments))
	progress.Total = len(segments)

	if progress.OnProgress != nil {
		progress.OnProgress(0, progress.Total)
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)
	errors := make(chan error, len(segments))
	results := make(chan SegmentResult, len(segments))

	for i, segmentURL := range segments {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			filename := fmt.Sprintf("segment_%04d.ts", idx)
			outputPath := filepath.Join(segmentsDir, filename)

			if info, err := os.Stat(outputPath); err == nil && info.Size() >= 188 {
				if validateSegmentFile(outputPath) == nil {
					atomic.AddInt32(&progress.Completed, 1)
					results <- SegmentResult{Index: idx, URL: url, Success: true}
					completed := int(atomic.LoadInt32(&progress.Completed))
					if progress.OnProgress != nil {
						progress.OnProgress(completed, progress.Total)
					}
					return
				}
			}

			if err := downloadSegment(url, outputPath, rateLimited); err != nil {
				atomic.AddInt32(&progress.Failed, 1)
				errors <- fmt.Errorf("segment %d: %w", idx, err)
				results <- SegmentResult{Index: idx, URL: url, Success: false, Err: err}
				return
			}

			atomic.AddInt32(&progress.Completed, 1)
			results <- SegmentResult{Index: idx, URL: url, Success: true}

			completed := int(atomic.LoadInt32(&progress.Completed))
			if progress.OnProgress != nil {
				progress.OnProgress(completed, progress.Total)
			}

			if completed%10 == 0 || completed == progress.Total {
				percent := float64(completed) / float64(progress.Total) * 100
				log.Printf("⬇️  Progress: %d/%d (%.1f%%)", completed, progress.Total, percent)
			}
		}(i, segmentURL)
	}

	wg.Wait()
	close(errors)
	close(results)

	var allResults []SegmentResult
	for result := range results {
		allResults = append(allResults, result)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Index < allResults[j].Index
	})

	if err := writeDownloadLog(outputDir, allResults, len(segments)); err != nil {
		log.Printf("⚠️  Failed to write log file: %v", err)
	}

	var downloadErrors []error
	var failedSegments []int
	for err := range errors {
		downloadErrors = append(downloadErrors, err)
		var segNum int
		if _, parseErr := fmt.Sscanf(err.Error(), "segment %d:", &segNum); parseErr == nil {
			failedSegments = append(failedSegments, segNum)
		}
	}

	if len(downloadErrors) > 0 {
		sort.Ints(failedSegments)

		var segmentList string
		if len(failedSegments) <= 5 {
			segmentNames := make([]string, len(failedSegments))
			for i, num := range failedSegments {
				segmentNames[i] = fmt.Sprintf("segment_%04d.ts", num)
			}
			segmentList = strings.Join(segmentNames, ", ")
		} else {
			segmentList = fmt.Sprintf("segment_%04d.ts ... segment_%04d.ts",
				failedSegments[0], failedSegments[len(failedSegments)-1])
		}

		log.Printf("❌ Failed segments: %v", failedSegments)

		successRate := float64(len(segments)-len(downloadErrors)) / float64(len(segments)) * 100

		if successRate >= 95.0 {
			log.Printf("⚠️  Downloaded %.1f%% (%d/%d segments) - proceeding with merge despite %d missing",
				successRate, len(segments)-len(downloadErrors), len(segments), len(downloadErrors))
			log.Printf("⚠️  Missing segments: %s", segmentList)
		} else {
			return nil, fmt.Errorf("failed to download %d segments (%.1f%% success): %s",
				len(downloadErrors), successRate, segmentList)
		}
	}

	log.Printf("✅ Downloaded all %d segments", len(segments))

	segmentFiles, err := getOrderedSegmentFiles(segmentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list segment files: %w", err)
	}

	totalSize := calculateDirSize(segmentsDir)

	log.Printf("✅ Downloaded %.2f MB of segments", float64(totalSize)/1024/1024)

	return &HLSDownloadResult{
		OutputDir:      outputDir,
		Resolution:     resolutionFolder,
		ResolutionFull: selected.Resolution,
		SegmentCount:   len(segments),
		TotalSize:      totalSize,
		SegmentFiles:   segmentFiles,
	}, nil
}

func getOrderedSegmentFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	skippedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".ts") {
			fullPath := filepath.Join(dir, entry.Name())
			if info, err := os.Stat(fullPath); err == nil && info.Size() > 0 {
				files = append(files, fullPath)
			} else {
				skippedCount++
				log.Printf("⚠️  Skipping missing/empty segment: %s", entry.Name())
			}
		}
	}

	if skippedCount > 0 {
		log.Printf("⚠️  Skipped %d missing/empty segment(s)", skippedCount)
	}

	sort.Strings(files)
	return files, nil
}

func extractResolutionFolder(resolution string) string {
	parts := strings.Split(resolution, "x")
	if len(parts) != 2 {
		return resolution
	}

	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil {
		return parts[1]
	}

	shortSide := h
	if w < h {
		shortSide = w
	}

	switch {
	case shortSide >= 900:
		return "1080"
	case shortSide >= 600:
		return "720"
	case shortSide >= 400:
		return "480"
	default:
		return "360"
	}
}

func downloadSegment(url string, outputPath string, rateLimited bool) error {
	var lastErr error

	for attempt := 1; attempt <= MaxRetries; attempt++ {
		acquireRateLimit(rateLimited)

		statusCode, err := downloadFile(url, outputPath)
		if err != nil {
			lastErr = err
			if attempt < MaxRetries {
				var backoff time.Duration
				if statusCode == http.StatusTooManyRequests {
					backoff = RateLimitBackoff * time.Duration(1<<(attempt-1))
					if backoff > 4*time.Minute {
						backoff = 4 * time.Minute
					}
					log.Printf("⏳ Rate limited (attempt %d/%d): retrying in %v", attempt, MaxRetries, backoff)
				} else {
					backoff = RetryDelay * time.Duration(1<<(attempt-1))
					log.Printf("⚠️  Segment download failed (attempt %d/%d): %v - retrying in %v",
						attempt, MaxRetries, err, backoff)
				}
				time.Sleep(backoff)
			}
			continue
		}

		if err := validateSegmentFile(outputPath); err != nil {
			lastErr = fmt.Errorf("validation failed: %w", err)
			if attempt < MaxRetries {
				backoff := RetryDelay * time.Duration(1<<(attempt-1))
				log.Printf("⚠️  Segment validation failed (attempt %d/%d): %v - retrying in %v",
					attempt, MaxRetries, err, backoff)
				os.Remove(outputPath)
				time.Sleep(backoff)
				continue
			}
			return lastErr
		}

		return nil
	}

	return fmt.Errorf("failed after %d attempts: %w", MaxRetries, lastErr)
}

func validateSegmentFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot stat file: %w", err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("file is empty")
	}

	if info.Size() < 188 {
		return fmt.Errorf("file too small (%d bytes)", info.Size())
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	syncByte := make([]byte, 1)
	if _, err := file.Read(syncByte); err != nil {
		return fmt.Errorf("cannot read sync byte: %w", err)
	}

	if syncByte[0] != 0x47 {
		return fmt.Errorf("invalid TS sync byte: 0x%02x (expected 0x47)", syncByte[0])
	}

	return nil
}

func downloadFile(url string, outputPath string) (int, error) {
	resp, err := httpGet(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return resp.StatusCode, err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return resp.StatusCode, err
}

// Cleanup removes the directory
func Cleanup(dir string) error {
	log.Printf("🧹 Cleaning up: %s", dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to cleanup: %w", err)
	}
	log.Printf("✅ Cleanup complete")
	return nil
}

func calculateDirSize(dir string) int64 {
	var totalSize int64
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	return totalSize
}
