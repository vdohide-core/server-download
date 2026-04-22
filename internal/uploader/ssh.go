package uploader

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"server-download/internal/db/models"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	SSHDialTimeout   = 30 * time.Second
	SSHMaxRetries    = 3
	SSHRetryDelay    = 5 * time.Second
	SSHUploadBufSize = 10 * 1024 * 1024
)

type SSHClient struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client
	storage    *models.Storage
}

// NewSSHClient creates a new SSH client from storage config
func NewSSHClient(storage *models.Storage) (*SSHClient, error) {
	if storage.Local == nil || storage.Local.SSH == nil || storage.Local.SSH.Username == "" {
		return nil, fmt.Errorf("storage %s has no SSH credentials", storage.Name)
	}
	config := &ssh.ClientConfig{
		User: storage.Local.SSH.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(storage.Local.SSH.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         SSHDialTimeout,
	}

	addr := fmt.Sprintf("%s:%d", storage.Local.Host, storage.Local.SSH.Port)
	log.Printf("🔗 Connecting SSH: %s@%s", storage.Local.SSH.Username, addr)

	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH dial failed: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient,
		sftp.MaxConcurrentRequestsPerFile(64),
		sftp.MaxPacket(32768),
	)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("SFTP client creation failed: %w", err)
	}

	log.Printf("✅ SSH connected to %s", storage.Name)

	return &SSHClient{
		sshClient:  sshClient,
		sftpClient: sftpClient,
		storage:    storage,
	}, nil
}

func (c *SSHClient) Close() {
	if c.sftpClient != nil {
		c.sftpClient.Close()
	}
	if c.sshClient != nil {
		c.sshClient.Close()
	}
}

type SSHUploadProgress struct {
	Total      int64
	OnProgress func(current, total int64)
}

type SSHProgressReader struct {
	io.Reader
	Total        int64
	Uploaded     *int64
	LastReported *int64
	Callback     func(int64, int64)
}

func (pr *SSHProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		*pr.Uploaded += int64(n)
		if pr.Callback != nil {
			shouldReport := false
			if *pr.Uploaded == pr.Total {
				shouldReport = true
			} else if *pr.Uploaded-*pr.LastReported >= 1024*1024 {
				shouldReport = true
			}
			if shouldReport {
				*pr.LastReported = *pr.Uploaded
				pr.Callback(*pr.Uploaded, pr.Total)
			}
		}
	}
	return n, err
}

func (c *SSHClient) UploadFile(localPath, remotePath string, onProgress func(int64)) (int64, error) {
	var lastErr error
	retryDelay := SSHRetryDelay

	for attempt := 1; attempt <= SSHMaxRetries; attempt++ {
		size, err := c.uploadFileOnce(localPath, remotePath, onProgress)
		if err == nil {
			return size, nil
		}
		lastErr = err
		errStr := err.Error()
		isRetryable := strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "broken pipe") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "EOF")
		if isRetryable && attempt < SSHMaxRetries {
			log.Printf("⚠️ Upload retry %d/%d after %v: %s", attempt, SSHMaxRetries, retryDelay, filepath.Base(localPath))
			time.Sleep(retryDelay)
			retryDelay *= 2
			continue
		}
		return 0, err
	}
	return 0, fmt.Errorf("failed after %d retries: %w", SSHMaxRetries, lastErr)
}

func (c *SSHClient) uploadFileOnce(localPath, remotePath string, onProgress func(int64)) (int64, error) {
	localFile, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("open local file: %w", err)
	}
	defer localFile.Close()

	fileInfo, _ := localFile.Stat()
	fileSize := fileInfo.Size()

	remoteDir := path.Dir(remotePath)
	if err := c.sftpClient.MkdirAll(remoteDir); err != nil {
		return 0, fmt.Errorf("create remote dir: %w", err)
	}

	remoteFile, err := c.sftpClient.Create(remotePath)
	if err != nil {
		return 0, fmt.Errorf("create remote file: %w", err)
	}
	defer remoteFile.Close()

	var uploaded, lastReported int64
	reader := &SSHProgressReader{
		Reader:       bufio.NewReaderSize(localFile, SSHUploadBufSize),
		Total:        fileSize,
		Uploaded:     &uploaded,
		LastReported: &lastReported,
		Callback: func(current, total int64) {
			if onProgress != nil {
				onProgress(current)
			}
		},
	}

	n, err := remoteFile.ReadFrom(reader)
	if err != nil {
		return 0, fmt.Errorf("copy file: %w", err)
	}
	if err := remoteFile.Close(); err != nil {
		return n, fmt.Errorf("close remote: %w", err)
	}
	if n != fileSize {
		return n, fmt.Errorf("incomplete: expected %d, wrote %d", fileSize, n)
	}
	return n, nil
}

type SSHUploadResult struct {
	VideoUploaded bool
	TotalSize     int64
}

// UploadFiles uploads video file to storage via SSH
func (c *SSHClient) UploadFiles(
	fileId string,
	videoPath string,
	fileName string,
	progress *SSHUploadProgress,
) (*SSHUploadResult, error) {
	result := &SSHUploadResult{}
	basePath := path.Join(c.storage.GetPath(), fileId)
	log.Printf("📂 Target: %s", basePath)

	var totalSize int64 = 0
	if videoPath != "" {
		info, err := os.Stat(videoPath)
		if err == nil {
			totalSize = info.Size()
		}
	}
	if progress != nil {
		progress.Total = totalSize
	}

	var completedBytes int64 = 0

	if videoPath != "" {
		remotePath := path.Join(basePath, fileName)
		size, err := c.UploadFile(videoPath, remotePath, func(fileBytes int64) {
			if progress != nil && progress.OnProgress != nil {
				progress.OnProgress(completedBytes+fileBytes, totalSize)
			}
		})
		if err != nil {
			return nil, fmt.Errorf("upload video: %w", err)
		}
		result.VideoUploaded = true
		result.TotalSize += size
		completedBytes += size
		log.Printf("✅ Uploaded %s", fileName)
	}

	log.Printf("✅ All files uploaded for %s (%.2f MB)", fileId, float64(result.TotalSize)/1024/1024)
	return result, nil
}
