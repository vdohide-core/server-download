package downloader

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type ScraperResponse struct {
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error,omitempty"`
}

func FetchM3U8FromScraper(scraperURL, sourceURL string) (string, string, error) {
	if scraperURL == "" {
		return "", "", fmt.Errorf("scraper URL is empty")
	}
	scraperURL = strings.TrimRight(scraperURL, "/")
	if !strings.HasSuffix(scraperURL, "/scraper") {
		scraperURL += "/scraper"
	}
	log.Printf("🔍 Calling scraper: %s for %s", scraperURL, sourceURL)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		body := strings.NewReader(fmt.Sprintf(`{"url":"%s"}`, sourceURL))
		req, _ := http.NewRequest("POST", scraperURL, body)
		req.Header.Set("Content-Type", "application/json")

		resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
		if err != nil {
			lastErr = err
			if attempt < 3 {
				time.Sleep(5 * time.Second)
			}
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("scraper status %d", resp.StatusCode)
			if attempt < 3 {
				time.Sleep(5 * time.Second)
			}
			continue
		}

		var result ScraperResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", "", fmt.Errorf("parse scraper response: %w", err)
		}
		if !result.Success {
			return "", "", fmt.Errorf("scraper: %s", result.Error)
		}

		m3u8, _ := result.Data["m3u8Url"].(string)
		if m3u8 == "" {
			return "", "", fmt.Errorf("scraper response missing m3u8Url")
		}
		title, _ := result.Data["title"].(string)
		log.Printf("✅ Scraper returned m3u8 URL")
		return m3u8, title, nil
	}
	return "", "", fmt.Errorf("scraper failed after 3 attempts: %v", lastErr)
}
