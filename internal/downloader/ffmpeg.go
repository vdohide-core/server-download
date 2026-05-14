package downloader

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MergeResult contains the result of ffmpeg merge
type MergeResult struct {
	OutputPath string
	FileSize   int64
}

// IsDiskFullError checks if an error is caused by disk full
func IsDiskFullError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "No space left") ||
		strings.Contains(err.Error(), "disk full")
}

// MergeToMP4 merges .ts segment files into a single MP4 using ffmpeg
func MergeToMP4(segmentFiles []string, outputPath string, onProgress func(percent int)) (*MergeResult, error) {
	if len(segmentFiles) == 0 {
		return nil, fmt.Errorf("no segment files to merge")
	}

	var validSegments []string
	for _, f := range segmentFiles {
		if info, err := os.Stat(f); err == nil && info.Size() >= 188 {
			if validateSegmentFile(f) == nil {
				validSegments = append(validSegments, f)
			} else {
				log.Printf("⚠️  Removing corrupt segment: %s", filepath.Base(f))
				os.Remove(f)
			}
		}
	}
	if len(validSegments) == 0 {
		return nil, fmt.Errorf("no valid segment files found (all missing or corrupt)")
	}
	if len(validSegments) < len(segmentFiles) {
		log.Printf("⚠️  %d/%d segments invalid, proceeding with %d valid", len(segmentFiles)-len(validSegments), len(segmentFiles), len(validSegments))
	}
	segmentFiles = validSegments

	log.Printf("🎬 Merging %d segments to %s", len(segmentFiles), outputPath)

	if len(segmentFiles) > 0 {
		codecInfo, err := DetectCodecs(segmentFiles[0])
		if err != nil {
			log.Printf("⚠️  Codec detection failed: %v - attempting merge anyway", err)
		} else {
			log.Printf("📹 Detected codecs: video=%s, audio=%s", codecInfo.VideoCodec, codecInfo.AudioCodec)

			if !codecInfo.IsCompatible {
				log.Printf("⚠️  %s - using re-encode mode", codecInfo.Reason)
				return MergeToMP4WithReencode(segmentFiles, outputPath, onProgress)
			}

			if !strings.EqualFold(codecInfo.VideoCodec, "h264") {
				log.Printf("🔄 Video codec is %s (not h264) — re-encoding to h264", codecInfo.VideoCodec)
				return MergeToMP4WithReencode(segmentFiles, outputPath, onProgress)
			}
		}
	}

	totalDuration := getSegmentsDuration(segmentFiles)

	listPath := filepath.Join(filepath.Dir(segmentFiles[0]), "concat_list.txt")
	listFile, err := os.Create(listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create concat list: %w", err)
	}

	for _, segFile := range segmentFiles {
		fmt.Fprintf(listFile, "file '%s'\n", filepath.Base(segFile))
	}
	listFile.Close()

	cmd := exec.Command("ffmpeg",
		"-y",
		"-fflags", "+genpts+igndts+discardcorrupt",
		"-err_detect", "ignore_err",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-max_muxing_queue_size", "9999",
		"-movflags", "+faststart",
		outputPath,
	)

	cmd.Dir = filepath.Dir(listPath)

	err = runFFmpegWithProgress(cmd, totalDuration, onProgress)
	if err != nil {
		log.Printf("⚠️  Copy mode failed: %v", err)
		os.Remove(outputPath)
		os.Remove(listPath)
		if strings.Contains(err.Error(), "No space left") {
			return nil, fmt.Errorf("merge failed (disk full): %w", err)
		}
		log.Printf("🔄 Attempting re-encode fallback...")
		return MergeToMP4WithReencode(segmentFiles, outputPath, onProgress)
	}

	os.Remove(listPath)

	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat output: %w", err)
	}

	sizeMB := float64(info.Size()) / 1024 / 1024
	log.Printf("✅ Merged to MP4: %s (%.2f MB)", outputPath, sizeMB)

	return &MergeResult{
		OutputPath: outputPath,
		FileSize:   info.Size(),
	}, nil
}

func getSegmentsDuration(segmentFiles []string) float64 {
	if len(segmentFiles) == 0 {
		return 0
	}

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		segmentFiles[0],
	)

	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0
	}

	return duration * float64(len(segmentFiles))
}

func runFFmpegWithProgress(cmd *exec.Cmd, totalDuration float64, onProgress func(percent int)) error {
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	lastPercent := -5
	var lastLines []string
	const maxLastLines = 10

	scanner := bufio.NewScanner(stderr)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		for i, b := range data {
			if b == '\r' || b == '\n' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if len(strings.TrimSpace(line)) == 0 {
				continue
			}

			lastLines = append(lastLines, line)
			if len(lastLines) > maxLastLines {
				lastLines = lastLines[1:]
			}

			if idx := strings.Index(line, "time="); idx >= 0 {
				timeStr := line[idx+5:]
				if spaceIdx := strings.IndexAny(timeStr, " \t"); spaceIdx > 0 {
					timeStr = timeStr[:spaceIdx]
				}

				currentSec := parseTimeToSeconds(timeStr)
				if currentSec > 0 && totalDuration > 0 {
					percent := int(currentSec / totalDuration * 100)
					if percent > 100 {
						percent = 100
					}
					if percent >= lastPercent+5 {
						lastPercent = percent
						log.Printf("🎬 Merge progress: %d%%", percent)
						if onProgress != nil {
							onProgress(percent)
						}
					}
				}
			}
		}
	}()

	<-done
	waitErr := cmd.Wait()

	if waitErr != nil {
		var stderrMsg string
		if len(lastLines) > 0 {
			log.Printf("⚠️  ffmpeg stderr (last %d lines):", len(lastLines))
			for _, line := range lastLines {
				log.Printf("   %s", line)
			}
			stderrMsg = strings.Join(lastLines, "\n")
		}
		return fmt.Errorf("%w: %s", waitErr, stderrMsg)
	}

	return nil
}

func parseTimeToSeconds(timeStr string) float64 {
	if strings.HasPrefix(timeStr, "-") {
		return 0
	}

	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, _ := strconv.ParseFloat(parts[0], 64)
	minutes, _ := strconv.ParseFloat(parts[1], 64)
	seconds, _ := strconv.ParseFloat(parts[2], 64)

	return hours*3600 + minutes*60 + seconds
}

// CheckFFmpeg verifies ffmpeg is available
func CheckFFmpeg() error {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	return nil
}

// VideoInfo contains video metadata from ffprobe
type VideoInfo struct {
	Width    int64
	Height   int64
	Duration int64
}

// ProbeVideoInfo extracts width, height, and duration from a video file
func ProbeVideoInfo(filePath string) (*VideoInfo, error) {
	info := &VideoInfo{}

	probeResolution := func() {
		cmd := exec.Command("ffprobe",
			"-v", "error",
			"-select_streams", "v:0",
			"-show_entries", "stream=width,height",
			"-of", "csv=s=x:p=0",
			filePath,
		)
		output, err := cmd.Output()
		if err != nil {
			log.Printf("⚠️  ffprobe resolution failed: %v", err)
			return
		}
		raw := strings.TrimSpace(string(output))
		parts := strings.Split(raw, "x")
		if len(parts) == 2 {
			w, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			h, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			info.Width = w
			info.Height = h
		} else {
			log.Printf("⚠️  ffprobe resolution unexpected output: %q", raw)
		}
	}

	probeDuration := func() {
		cmd := exec.Command("ffprobe",
			"-v", "error",
			"-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
			filePath,
		)
		output, err := cmd.Output()
		if err != nil {
			log.Printf("⚠️  ffprobe duration failed: %v", err)
			return
		}
		dur, _ := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
		info.Duration = int64(dur)
	}

	probeResolution()
	probeDuration()

	// Retry once if resolution is missing (file may still be flushing to disk)
	if info.Width == 0 && info.Height == 0 {
		log.Printf("⚠️  Resolution is 0x0 — retrying probe in 2s...")
		time.Sleep(2 * time.Second)
		probeResolution()
		if info.Duration == 0 {
			probeDuration()
		}
	}

	if info.Width == 0 && info.Height == 0 {
		return info, fmt.Errorf("ffprobe failed to detect resolution for %s", filepath.Base(filePath))
	}

	return info, nil
}

// CodecInfo contains codec information from ffprobe
type CodecInfo struct {
	VideoCodec   string
	AudioCodec   string
	IsCompatible bool
	Reason       string
}

// DetectCodecs probes video/audio codecs using ffprobe
func DetectCodecs(inputPath string) (*CodecInfo, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)

	videoOutput, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to probe video codec: %w", err)
	}

	videoCodec := strings.Split(strings.TrimSpace(string(videoOutput)), "\n")[0]
	videoCodec = strings.TrimSpace(videoCodec)

	cmd = exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)

	audioOutput, _ := cmd.Output()
	audioCodec := strings.Split(strings.TrimSpace(string(audioOutput)), "\n")[0]
	audioCodec = strings.TrimSpace(audioCodec)

	info := &CodecInfo{
		VideoCodec:   videoCodec,
		AudioCodec:   audioCodec,
		IsCompatible: true,
	}

	incompatibleCodecs := []string{"webp", "png", "jpeg", "gif"}
	for _, ic := range incompatibleCodecs {
		if strings.EqualFold(videoCodec, ic) {
			info.IsCompatible = false
			info.Reason = fmt.Sprintf("codec %s not supported in MP4 container", videoCodec)
			break
		}
	}

	return info, nil
}

// MergeToMP4WithReencode merges segments with re-encoding (fallback)
func MergeToMP4WithReencode(segmentFiles []string, outputPath string, onProgress func(percent int)) (*MergeResult, error) {
	if len(segmentFiles) == 0 {
		return nil, fmt.Errorf("no segment files to merge")
	}

	log.Printf("🎬 Re-encoding %d segments to %s (fallback mode)", len(segmentFiles), outputPath)

	totalDuration := getSegmentsDuration(segmentFiles)

	listPath := filepath.Join(filepath.Dir(segmentFiles[0]), "concat_list.txt")
	listFile, err := os.Create(listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create concat list: %w", err)
	}

	for _, segFile := range segmentFiles {
		fmt.Fprintf(listFile, "file '%s'\n", filepath.Base(segFile))
	}
	listFile.Close()

	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		outputPath,
	)

	cmd.Dir = filepath.Dir(listPath)

	err = runFFmpegWithProgress(cmd, totalDuration, onProgress)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg re-encode failed: %w", err)
	}

	os.Remove(listPath)

	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat output: %w", err)
	}

	sizeMB := float64(info.Size()) / 1024 / 1024
	log.Printf("✅ Re-encoded to MP4: %s (%.2f MB)", outputPath, sizeMB)

	return &MergeResult{
		OutputPath: outputPath,
		FileSize:   info.Size(),
	}, nil
}

// RemuxWithFaststart remuxes an MP4 file with -movflags +faststart
func RemuxWithFaststart(inputPath, outputPath string) error {
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", inputPath,
		"-c", "copy",
		"-movflags", "+faststart",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg faststart failed: %w\n%s", err, string(output))
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file not found: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("output file is empty")
	}

	return nil
}

// EnsureH264Faststart checks video codec and ensures output is h264 with faststart.
func EnsureH264Faststart(inputPath, outputPath string, onProgress func(percent int)) error {
	codecInfo, err := DetectCodecs(inputPath)
	if err != nil {
		log.Printf("⚠️  Codec detection failed: %v — defaulting to re-encode", err)
		return TranscodeToH264(inputPath, outputPath, onProgress)
	}

	log.Printf("📹 Detected codec: video=%s, audio=%s", codecInfo.VideoCodec, codecInfo.AudioCodec)

	if strings.EqualFold(codecInfo.VideoCodec, "h264") {
		log.Printf("✅ Video is h264 — remuxing with faststart (copy mode)")
		return RemuxWithFaststart(inputPath, outputPath)
	}

	log.Printf("🔄 Video is %s (not h264) — re-encoding to h264 with faststart", codecInfo.VideoCodec)
	return TranscodeToH264(inputPath, outputPath, onProgress)
}

// TranscodeToH264 re-encodes a video file to h264/aac with faststart
func TranscodeToH264(inputPath, outputPath string, onProgress func(percent int)) error {
	var totalDuration float64
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)
	if output, err := cmd.Output(); err == nil {
		totalDuration, _ = strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	}

	cmd = exec.Command("ffmpeg",
		"-y",
		"-i", inputPath,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		outputPath,
	)

	err := runFFmpegWithProgress(cmd, totalDuration, onProgress)
	if err != nil {
		return fmt.Errorf("h264 transcode failed: %w", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file not found: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("output file is empty")
	}

	log.Printf("✅ Transcoded to h264: %s (%.2f MB)", outputPath, float64(info.Size())/1024/1024)
	return nil
}
