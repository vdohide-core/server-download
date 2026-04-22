# Server Download

เซิร์ฟเวอร์ Go สำหรับดาวน์โหลดและประมวลผลวิดีโอจากแหล่งต่างๆ สำหรับ [vdohide.com](https://vdohide.com)

## Features

- **MissAV** — ดาวน์โหลด HLS playlist จาก `file.metadata.playlist`
- **XVideos / PornHub** — เรียก Scraper API เพื่อดึง m3u8 URL ล่าสุด (URL หมดอายุได้)
- **Google Drive** — ดาวน์โหลดผ่าน OAuth2 จาก `oauths` collection
- **Upload (S3)** — ดาวน์โหลดไฟล์ที่อัพโหลดจาก S3 storage ผ่าน `ingests` record
- **Auto Worker** — poll ทุก 5 วินาที หา video ที่รอ download
- **SSH Upload** — อัพโหลดไฟล์ไปยัง storage server ผ่าน SFTP
- **Progress Tracking** — ติดตามความคืบหน้าแบบ real-time ใน `video_process` collection
- **Retry & Resume** — รองรับ retry สำหรับ failed jobs และ resume downloads

## Requirements

- **Go** 1.24+
- **FFmpeg** + **FFprobe** (ต้องอยู่ใน PATH)
- **MongoDB** (vdohide database)

## Quick Start

```bash
# 1. Clone & install dependencies
git clone <repo>
cd server-download
go mod tidy

# 2. สร้างไฟล์ .env
cp .env.example .env
# แก้ไข MONGODB_URI ให้ชี้ไปที่ database ของคุณ

# 3. Run
go run ./cmd
```

## Configuration

สร้างไฟล์ `.env` ที่ root ของโปรเจกต์:

```env
# MongoDB connection (required)
MONGODB_URI=mongodb+srv://user:pass@host/vdohide

# Storage config — ใช้อย่างใดอย่างหนึ่ง:
# Option A: Local storage (running on storage server)
STORAGE_PATH="/home/files"
STORAGE_ID="storage-uuid-here"

# Option B: SSH upload (auto-detect จาก DB)
# ไม่ต้องตั้ง STORAGE_PATH/STORAGE_ID — จะหา storage ที่มี SSH credentials อัตโนมัติ

# Scraper URL (optional — fallback ใช้ setting "url_scraping" จาก DB)
# SCRAPER_URL="http://scraper-server:8081"

# Worker ID (optional — default: hostname@1)
# WORKER_ID="worker-1"
```

### Database Settings

ตั้งค่าใน `settings` collection:

| Setting Name | Type | Description |
|---|---|---|
| `download_enabled` | boolean | เปิด/ปิดการทำงาน (default: `false`) |
| `url_scraping` | string | URL ของ Scraper API (e.g. `http://scraper:8081`) |

## Usage

### Worker Mode (แนะนำ)

```bash
# Run ปกติ — poll หา jobs อัตโนมัติ
./server-download

# หรือผ่าน go run
go run ./cmd
```

Worker จะ:
1. ตรวจสอบ `download_enabled` setting ทุก 5 วินาที
2. หา video files ที่ `status=waiting` + มี `metadata.sourceType`
3. Download → Merge → Upload → สร้าง Media record
4. อัพเดทไฟล์เป็น `status=ready`

### CLI Mode

```bash
# ดาวน์โหลดจาก m3u8 URL โดยตรง
./server-download <m3u8_url> <file_id>
```

## Build

```bash
# Build for current OS
go build -o server-download ./cmd

# Cross-compile (Windows/Linux/ARM64)
build.bat
```

Output จะอยู่ที่ `../binary-build/server-download/`

## Architecture

```
server-download/
├── cmd/main.go                     # Entry point + worker loop
├── internal/
│   ├── config/                     # .env loading
│   ├── database/                   # MongoDB connection
│   ├── models/                     # Data models
│   │   ├── file.go                 # files collection
│   │   ├── media.go                # medias collection (fileIds[])
│   │   ├── storage.go              # storages collection (local.ssh)
│   │   ├── ingest.go               # ingests collection
│   │   ├── setting.go              # settings collection
│   │   └── video_process.go        # video_process tracking
│   ├── downloader/
│   │   ├── httpclient.go           # Browser-like HTTP client
│   │   ├── m3u8.go                 # M3U8 playlist parser
│   │   ├── hls.go                  # HLS segment downloader
│   │   ├── ffmpeg.go               # FFmpeg merge + probe
│   │   ├── scraper.go              # Scraper API client
│   │   ├── s3download.go           # S3 download
│   │   └── gdrive.go              # Google Drive download (OAuth)
│   ├── uploader/
│   │   ├── local.go                # Local file move
│   │   └── ssh.go                  # SSH/SFTP upload
│   └── utils/                      # Helpers
├── .env                            # Configuration
├── build.bat                       # Cross-compile script
└── go.mod                          # Go module
```

## Processing Flow

```
┌─────────────────────────────────────────────────┐
│                   Worker Loop                    │
│              (poll every 5 seconds)              │
└──────────────────────┬──────────────────────────┘
                       │
                       ▼
              ┌────────────────┐
              │  Find WAITING  │
              │  video files   │
              └───────┬────────┘
                      │
          ┌───────────┼───────────┬──────────┐
          ▼           ▼           ▼          ▼
      ┌───────┐  ┌────────┐  ┌───────┐  ┌───────┐
      │missav │  │xvideos │  │upload │  │gdrive │
      │       │  │pornhub │  │       │  │       │
      └───┬───┘  └───┬────┘  └───┬───┘  └───┬───┘
          │          │           │           │
          │     Scraper API      │      Google API
          │          │           │           │
          ▼          ▼           ▼           ▼
      ┌──────────────────────────────────────────┐
      │        Download (HLS or Direct MP4)       │
      └─────────────────┬────────────────────────┘
                        │
                        ▼
              ┌────────────────┐
              │  Merge to MP4  │  (ffmpeg, skip for direct MP4)
              │  + Probe info  │
              └───────┬────────┘
                      │
                      ▼
              ┌────────────────┐
              │  Upload to     │  (Local move or SSH/SFTP)
              │  Storage       │
              └───────┬────────┘
                      │
                      ▼
              ┌────────────────┐
              │  Create Media  │  → medias collection
              │  Update File   │  → status: ready
              └────────────────┘
```

## Source Types

| sourceType | Source | Download Method | Merge Required |
|---|---|---|---|
| `missav` | `file.metadata.playlist` | HLS segments | ✅ FFmpeg |
| `xvideos` | Scraper API → m3u8 | HLS segments | ✅ FFmpeg |
| `pornhub` | Scraper API → m3u8 | HLS segments | ✅ FFmpeg |
| `upload` | `ingests.path` (S3) | Direct MP4 | ❌ |
| `gdrive` | `file.metadata.source` | Google Drive API | ❌ |

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `MONGODB_URI` | ✅ | `mongodb://localhost:27017` | MongoDB connection string |
| `STORAGE_PATH` | ❌ | — | Local storage path (if running on storage server) |
| `STORAGE_ID` | ❌ | — | Storage ID (pair with STORAGE_PATH) |
| `SCRAPER_URL` | ❌ | — | Scraper API URL (fallback: DB setting) |
| `WORKER_ID` | ❌ | `hostname@1` | Worker identifier |

## License

Private — for vdohide.com internal use only.
