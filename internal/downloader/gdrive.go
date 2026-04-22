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

// GetRandomOAuth finds a random enabled OAuth record from the oauths collection
func GetRandomOAuth(oauthsCol *mongo.Collection) (*GoogleOAuth, error) {
	ctx := context.Background()

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"enable": true}}},
		{{Key: "$sample", Value: bson.M{"size": 1}}},
	}

	cursor, err := oauthsCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("query oauths: %w", err)
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return nil, fmt.Errorf("no enabled OAuth credentials found in oauths collection")
	}

	var oauth GoogleOAuth
	if err := cursor.Decode(&oauth); err != nil {
		return nil, fmt.Errorf("decode oauth: %w", err)
	}

	return &oauth, nil
}

// DownloadFromGDrive downloads a file from Google Drive using OAuth credentials
func DownloadFromGDrive(gdriveFileID string, outputPath string, oauthsCol *mongo.Collection, onProgress func(downloaded, total int64)) error {
	log.Printf("📥 Google Drive download: %s", gdriveFileID)

	oauth, err := GetRandomOAuth(oauthsCol)
	if err != nil {
		return fmt.Errorf("get OAuth: %w", err)
	}

	accessToken, err := refreshAccessToken(oauth, oauthsCol)
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	fileInfo, err := getGDriveFileInfo(gdriveFileID, accessToken)
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}

	log.Printf("📋 File: %s (%s bytes, %s)", fileInfo.Name, fileInfo.Size, fileInfo.MimeType)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

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

func getGDriveFileInfo(fileID, accessToken string) (*GDriveFileInfo, error) {
	apiURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=id,name,size,mimeType&supportsAllDrives=true", fileID)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

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
	req.Header.Set("Authorization", "Bearer "+accessToken)

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

	if err := os.Rename(outputPath+".tmp", outputPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	sizeMB := float64(downloaded) / 1024 / 1024
	log.Printf("✅ Downloaded %.2f MB from Google Drive", sizeMB)
	return nil
}
