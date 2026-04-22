package downloader

import (
	"fmt"
	"net/http"
	"net/url"
)

// Default headers to mimic a browser request.
var defaultHeaders = map[string]string{
	"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Accept":          "*/*",
	"Accept-Language": "en-US,en;q=0.9",
	"Sec-Fetch-Dest":  "empty",
	"Sec-Fetch-Mode":  "cors",
	"Sec-Fetch-Site":  "cross-site",
}

// originMap maps CDN hostnames to the Origin/Referer they expect.
var originMap = map[string]string{
	"surrit.com": "https://missav.ai",
	"phncdn.com": "https://www.pornhub.com",
}

// rateLimitedDomains are CDN domains that enforce rate limiting.
var rateLimitedDomains = map[string]bool{
	"surrit.com": true,
	"phncdn.com": true,
}

// httpGet performs an HTTP GET with browser-like headers.
func httpGet(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}

	parsed, err := url.Parse(rawURL)
	if err == nil {
		host := parsed.Hostname()
		origin := findOrigin(host)
		if origin != "" {
			req.Header.Set("Origin", origin)
			req.Header.Set("Referer", origin+"/")
		}
	}

	return http.DefaultClient.Do(req)
}

func findOrigin(host string) string {
	if origin, ok := originMap[host]; ok {
		return origin
	}
	for {
		idx := len(host)
		for i := 0; i < len(host); i++ {
			if host[i] == '.' {
				idx = i
				break
			}
		}
		if idx >= len(host)-1 {
			break
		}
		host = host[idx+1:]
		if origin, ok := originMap[host]; ok {
			return origin
		}
	}
	return ""
}

// IsRateLimitedDomain checks if a URL belongs to a rate-limited domain.
func IsRateLimitedDomain(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return findRateLimited(parsed.Hostname())
}

func findRateLimited(host string) bool {
	if rateLimitedDomains[host] {
		return true
	}
	for {
		idx := len(host)
		for i := 0; i < len(host); i++ {
			if host[i] == '.' {
				idx = i
				break
			}
		}
		if idx >= len(host)-1 {
			break
		}
		host = host[idx+1:]
		if rateLimitedDomains[host] {
			return true
		}
	}
	return false
}
