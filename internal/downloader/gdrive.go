package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// GoogleOAuth represents an OAuth record from the oauths collection
type GoogleOAuth struct {
	ID           string      `bson:"_id" json:"id"`
	ClientID     string      `bson:"client_id" json:"client_id"`
	ClientSecret string      `bson:"client_secret" json:"client_secret"`
	RefreshToken string      `bson:"refresh_token" json:"refresh_token"`
	Enable       bool        `bson:"enable" json:"enable"`
	TokenAt      *time.Time  `bson:"tokenAt,omitempty" json:"tokenAt,omitempty"`
	Token        *OAuthToken `bson:"token,omitempty" json:"token,omitempty"`
}

type OAuthToken struct {
	AccessToken string `bson:"access_token" json:"access_token"`
	TokenType   string `bson:"token_type" json:"token_type"`
}

// GDriveFileInfo contains Google Drive file metadata
type GDriveFileInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     string `json:"size"`
	MimeType string `json:"mimeType"`
}

func refreshAccessToken(oauth *GoogleOAuth, oauthsCol *mongo.Collection) (string, error) {
	if oauth.Token != nil && oauth.Token.AccessToken != "" && oauth.TokenAt != nil {
		elapsed := time.Since(*oauth.TokenAt).Seconds()
		if elapsed < 3500 {
			return oauth.Token.AccessToken, nil
		}
	}

	log.Printf("🔄 Refreshing Google OAuth token...")

	data := url.Values{}
	data.Set("client_id", oauth.ClientID)
	data.Set("client_secret", oauth.ClientSecret)
	data.Set("refresh_token", oauth.RefreshToken)
	data.Set("grant_type", "refresh_token")

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	if result.Error != "" {
		if oauthsCol != nil {
			oauthsCol.UpdateOne(context.Background(),
				bson.M{"_id": oauth.ID},
				bson.M{"$set": bson.M{"enable": false}},
			)
		}
		return "", fmt.Errorf("token refresh error: %s - %s", result.Error, result.ErrorDesc)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	if oauthsCol != nil {
		now := time.Now()
		oauthsCol.UpdateOne(context.Background(),
			bson.M{"_id": oauth.ID},
			bson.M{"$set": bson.M{
				"enable":  true,
				"token":   bson.M{"access_token": result.AccessToken, "token_type": result.TokenType},
				"tokenAt": now,
			}},
		)
	}

	log.Printf("✅ Token refreshed successfully")
	return result.AccessToken, nil
}

// GetRandomOAuth finds a random enabled OAuth record from the oauths collection.
// It first tries to find one scoped to the given spaceId, then falls back to global (no spaceId).
func GetRandomOAuth(oauthsCol *mongo.Collection, spaceId string) (*GoogleOAuth, error) {
	ctx := context.Background()

	// 1) Try workspace-scoped OAuth first
	if spaceId != "" {
		if oauth, err := findRandomOAuth(ctx, oauthsCol, bson.M{
			"enable":  true,
			"spaceId": spaceId,
		}); err == nil {
			return oauth, nil
		}
	}

	// 2) Fallback to global OAuth (no spaceId)
	if oauth, err := findRandomOAuth(ctx, oauthsCol, bson.M{
		"enable": true,
		"spaceId": bson.M{"$exists": false},
	}); err == nil {
		return oauth, nil
	}

	// 3) Last resort: any enabled OAuth
	if oauth, err := findRandomOAuth(ctx, oauthsCol, bson.M{
		"enable": true,
	}); err == nil {
		return oauth, nil
	}

	return nil, fmt.Errorf("no enabled OAuth credentials found")
}

func findRandomOAuth(ctx context.Context, col *mongo.Collection, filter bson.M) (*GoogleOAuth, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$sample", Value: bson.M{"size": 1}}},
	}

	cursor, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return nil, fmt.Errorf("no match")
	}

	var oauth GoogleOAuth
	if err := cursor.Decode(&oauth); err != nil {
		return nil, err
	}
	return &oauth, nil
}

// DownloadFromGDrive downloads a file from Google Drive using OAuth credentials
func DownloadFromGDrive(gdriveFileID string, outputPath string, oauthsCol *mongo.Collection, spaceId string, onProgress func(downloaded, total int64)) error {
	log.Printf("📥 Google Drive download: %s", gdriveFileID)

	var accessToken string
	oauth, err := GetRandomOAuth(oauthsCol, spaceId)
	if err != nil {
		log.Printf("⚠️  No OAuth credentials found — trying public download")
	} else {
		token, err := refreshAccessToken(oauth, oauthsCol)
		if err != nil {
			log.Printf("⚠️  OAuth token refresh failed: %v — trying public download", err)
		} else {
			accessToken = token
			// Mask client_id for logging: show first 8 chars
			maskedClient := oauth.ClientID
			if len(maskedClient) > 8 {
				maskedClient = maskedClient[:8] + "..."
			}
			log.Printf("🔑 [oauth] Using OAuth id=%s client=%s", oauth.ID, maskedClient)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Public download (no OAuth token)
	if accessToken == "" {
		log.Printf("📥 [public] Downloading via public URL (no OAuth)...")
		return downloadGDrivePublic(gdriveFileID, outputPath, onProgress)
	}

	log.Printf("📥 [oauth] Downloading via authenticated API...")

	// Authenticated download
	fileInfo, err := getGDriveFileInfo(gdriveFileID, accessToken)
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}

	log.Printf("📋 File: %s (%s bytes, %s)", fileInfo.Name, fileInfo.Size, fileInfo.MimeType)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		err := downloadGDriveFile(gdriveFileID, accessToken, outputPath, onProgress)
		if err == nil {
			log.Printf("✅ Google Drive download complete: %s", fileInfo.Name)
			return nil
		}
		lastErr = err
		if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "404") {
			return err
		}
		if attempt < 3 {
			log.Printf("⚠️ GDrive download attempt %d/3 failed: %v - retrying...", attempt, err)
			time.Sleep(5 * time.Second)
		}
	}

	return fmt.Errorf("GDrive download failed after 3 attempts: %v", lastErr)
}

// downloadGDrivePublic downloads a public GDrive file without OAuth
// using the direct download URL that doesn't require API credentials.
func downloadGDrivePublic(fileID, outputPath string, onProgress func(downloaded, total int64)) error {
	dlURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)

	client := &http.Client{
		Timeout: 5 * time.Hour,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil // follow redirects
		},
	}

	resp, err := client.Get(dlURL)
	if err != nil {
		return fmt.Errorf("public download request: %w", err)
	}
	defer resp.Body.Close()

	// Handle large file confirmation / virus scan warning page
	if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		htmlStr := string(body)
		confirmURL := ""

		// Strategy 1: New Google Drive format (2024+)
		// <form action="https://drive.usercontent.google.com/download" method="get">
		//   <input type="hidden" name="id" value="...">
		//   <input type="hidden" name="export" value="download">
		//   <input type="hidden" name="confirm" value="t">
		//   <input type="hidden" name="uuid" value="...">
		// </form>
		if idx := strings.Index(htmlStr, "drive.usercontent.google.com/download"); idx != -1 {
			params := extractHiddenInputs(htmlStr)
			if params.Has("id") {
				confirmURL = "https://drive.usercontent.google.com/download?" + params.Encode()
			}
		}

		// Strategy 2: Legacy format — href="/uc?export=download&amp;..."
		if confirmURL == "" {
			if idx := strings.Index(htmlStr, "href=\"/uc?export=download&amp;"); idx != -1 {
				end := strings.Index(htmlStr[idx+6:], "\"")
				if end != -1 {
					confirmURL = "https://drive.google.com" + strings.ReplaceAll(htmlStr[idx+6:idx+6+end], "&amp;", "&")
				}
			}
		}

		// Strategy 3: Fallback — new usercontent domain with confirm=t
		if confirmURL == "" {
			confirmURL = fmt.Sprintf("https://drive.usercontent.google.com/download?id=%s&export=download&confirm=t", fileID)
		}

		log.Printf("📥 [public] Confirm URL: %s", confirmURL)

		resp, err = client.Get(confirmURL)
		if err != nil {
			return fmt.Errorf("public download confirm: %w", err)
		}
		defer resp.Body.Close()

		// After confirm redirect, check if we still got HTML (auth wall / error)
		if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			return fmt.Errorf("access denied to Google Drive file: %s — file requires authentication (got HTML page after confirm)", fileID)
		}
	}

	if resp.StatusCode == 404 {
		return fmt.Errorf("Google Drive file not found: %s (404)", fileID)
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("access denied to Google Drive file: %s (403) — file may not be public", fileID)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("public download error %d: %s", resp.StatusCode, string(body))
	}

	// Final Content-Type guard: if we still get HTML, the file is not publicly accessible
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		return fmt.Errorf("access denied to Google Drive file: %s — file requires authentication (received HTML instead of file data)", fileID)
	}

	totalSize := resp.ContentLength
	log.Printf("📋 [public] Download started (size: %d bytes)", totalSize)

	out, err := os.Create(outputPath + ".tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	var downloaded int64
	buf := make([]byte, 256*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				out.Close()
				os.Remove(outputPath + ".tmp")
				return fmt.Errorf("write error: %w", wErr)
			}
			downloaded += int64(n)
			if onProgress != nil && totalSize > 0 {
				onProgress(downloaded, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Close()
			os.Remove(outputPath + ".tmp")
			return fmt.Errorf("read error: %w", err)
		}
	}
	out.Close()

	// Guard: if downloaded size is suspiciously small, it's likely an error page
	if downloaded < 10*1024 {
		os.Remove(outputPath + ".tmp")
		return fmt.Errorf("Google Drive public download failed: file too small (%d bytes) — likely an auth/error page, not the actual file", downloaded)
	}

	if err := os.Rename(outputPath+".tmp", outputPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Printf("✅ [public] Downloaded %.2f MB from Google Drive", float64(downloaded)/1024/1024)
	return nil
}

func getGDriveFileInfo(fileID, accessToken string) (*GDriveFileInfo, error) {
	apiURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=id,name,size,mimeType&supportsAllDrives=true", fileID)

	req, _ := http.NewRequest("GET", apiURL, nil)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("Google Drive file not found: %s", fileID)
	}
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("access denied to Google Drive file: %s", fileID)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Drive API error %d: %s", resp.StatusCode, string(body))
	}

	var info GDriveFileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func downloadGDriveFile(fileID, accessToken, outputPath string, onProgress func(downloaded, total int64)) error {
	apiURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media&supportsAllDrives=true", fileID)

	req, _ := http.NewRequest("GET", apiURL, nil)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	client := &http.Client{Timeout: 5 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download error %d: %s", resp.StatusCode, string(body))
	}

	totalSize := resp.ContentLength

	out, err := os.Create(outputPath + ".tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	var downloaded int64
	buf := make([]byte, 256*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				out.Close()
				os.Remove(outputPath + ".tmp")
				return fmt.Errorf("write error: %w", wErr)
			}
			downloaded += int64(n)

			if onProgress != nil && totalSize > 0 {
				onProgress(downloaded, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Close()
			os.Remove(outputPath + ".tmp")
			return fmt.Errorf("read error: %w", err)
		}
	}
	out.Close()

	// Guard: reject empty or suspiciously small files
	if downloaded < 10*1024 {
		os.Remove(outputPath + ".tmp")
		return fmt.Errorf("GDrive download failed: file too small (%d bytes) — likely not the actual video file", downloaded)
	}

	if err := os.Rename(outputPath+".tmp", outputPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	sizeMB := float64(downloaded) / 1024 / 1024
	log.Printf("✅ Downloaded %.2f MB from Google Drive", sizeMB)
	return nil
}

// extractHiddenInputs parses all <input type="hidden" name="..." value="..."> from HTML
// and returns them as url.Values for building query strings.
func extractHiddenInputs(html string) url.Values {
	params := url.Values{}
	// Match <input type="hidden" name="..." value="...">
	// Handle both single and double quotes, and any attribute order
	remaining := html
	for {
		idx := strings.Index(remaining, "<input ")
		if idx == -1 {
			break
		}
		// Find the end of this tag
		end := strings.Index(remaining[idx:], ">")
		if end == -1 {
			break
		}
		tag := remaining[idx : idx+end+1]
		remaining = remaining[idx+end+1:]

		// Only process hidden inputs
		if !strings.Contains(tag, "type=\"hidden\"") {
			continue
		}

		name := extractAttr(tag, "name")
		value := extractAttr(tag, "value")
		if name != "" {
			params.Set(name, value)
		}
	}
	return params
}

// extractAttr extracts an attribute value from an HTML tag string.
// e.g. extractAttr(`<input name="id" value="abc">`, "name") returns "id"
func extractAttr(tag, attr string) string {
	search := attr + "=\""
	idx := strings.Index(tag, search)
	if idx == -1 {
		return ""
	}
	start := idx + len(search)
	end := strings.Index(tag[start:], "\"")
	if end == -1 {
		return ""
	}
	return tag[start : start+end]
}
