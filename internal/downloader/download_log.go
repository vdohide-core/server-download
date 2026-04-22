package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SegmentResult represents the result of downloading a single segment
type SegmentResult struct {
	Index   int
	URL     string
	Success bool
	Err     error
}

// writeDownloadLog writes a detailed log file of the download process
func writeDownloadLog(outputDir string, results []SegmentResult, totalSegments int) error {
	logPath := filepath.Join(outputDir, "log.txt")

	file, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer file.Close()

	fmt.Fprintf(file, "Download Log\n")
	fmt.Fprintf(file, "============\n")
	fmt.Fprintf(file, "Generated: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(file, "Total Segments: %d\n\n", totalSegments)

	successCount := 0
	failureCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		} else {
			failureCount++
		}
	}

	fmt.Fprintf(file, "Summary\n")
	fmt.Fprintf(file, "-------\n")
	fmt.Fprintf(file, "✅ Success: %d (%.1f%%)\n", successCount, float64(successCount)/float64(totalSegments)*100)
	fmt.Fprintf(file, "❌ Failed:  %d (%.1f%%)\n\n", failureCount, float64(failureCount)/float64(totalSegments)*100)

	if failureCount > 0 {
		fmt.Fprintf(file, "Failed Segments\n")
		fmt.Fprintf(file, "---------------\n")
		for _, result := range results {
			if !result.Success {
				errMsg := "unknown error"
				if result.Err != nil {
					errMsg = result.Err.Error()
				}
				fmt.Fprintf(file, "❌ segment_%04d.ts - %s\n", result.Index, errMsg)
				fmt.Fprintf(file, "   URL: %s\n\n", result.URL)
			}
		}
	}

	if successCount > 0 {
		fmt.Fprintf(file, "\nSuccessful Segments\n")
		fmt.Fprintf(file, "-------------------\n")

		var ranges []string
		rangeStart := -1
		rangeEnd := -1

		for _, result := range results {
			if result.Success {
				if rangeStart == -1 {
					rangeStart = result.Index
					rangeEnd = result.Index
				} else if result.Index == rangeEnd+1 {
					rangeEnd = result.Index
				} else {
					if rangeStart == rangeEnd {
						ranges = append(ranges, fmt.Sprintf("segment_%04d.ts", rangeStart))
					} else {
						ranges = append(ranges, fmt.Sprintf("segment_%04d.ts - segment_%04d.ts", rangeStart, rangeEnd))
					}
					rangeStart = result.Index
					rangeEnd = result.Index
				}
			}
		}

		if rangeStart != -1 {
			if rangeStart == rangeEnd {
				ranges = append(ranges, fmt.Sprintf("segment_%04d.ts", rangeStart))
			} else {
				ranges = append(ranges, fmt.Sprintf("segment_%04d.ts - segment_%04d.ts", rangeStart, rangeEnd))
			}
		}

		fmt.Fprintf(file, "✅ %s\n", strings.Join(ranges, "\n✅ "))
	}

	return nil
}
