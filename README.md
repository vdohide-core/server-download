# Server Download

Worker service สำหรับดาวน์โหลดและประมวลผลวิดีโอจากแหล่งต่างๆ สำหรับ [VDOHide](https://vdohide.com)

## Features

- **Upload (S3/Local)** — ประมวลผลไฟล์ที่ผู้ใช้ upload ผ่าน `ingests` record
- **Google Drive** — ดาวน์โหลดผ่าน OAuth2 จาก `oauths` collection
- **HLS / Remote** — ดาวน์โหลด m3u8 playlist และ merge เป็น MP4
- **Scraper** — เรียก Scraper API เพื่อดึง m3u8 URL ล่าสุด
- **SCP Upload** — อัพโหลดไฟล์ไปยัง storage server ผ่าน SCP (Node.js)
- **Multi-Worker** — รัน worker หลายตัวพร้อมกัน ด้วย `WORKER_ID`
- **Auto Retry** — retry อัตโนมัติ 3 ครั้ง, reset file → waiting หาก fail
- **Progress Tracking** — ติดตาม download/merge/upload ใน `video_process` collection
- **Clone Media** — clone media record ไปยังไฟล์ที่ clonedFrom อัตโนมัติ

## Requirements

- **FFmpeg** + **FFprobe** (ต้องอยู่ใน PATH)
- **Node.js** 18+ (สำหรับ SCP upload script)
- **MongoDB** (vdohide platform database)

---

## Installation (Linux Server)

### One-line install

```bash
curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-download/main/install.sh | sudo -E bash -s -- \
    --mongodb-uri "mongodb+srv://user:pass@cluster.mongodb.net/platform" \
    -n 1
```

### Options

| Option | Default | คำอธิบาย |
|---|---|---|
| `-n, --count` | `1` | จำนวน worker instances |
| `--mongodb-uri` | `""` | MongoDB connection string |
| `--storage-id` | `""` | Storage ID (optional) |
| `--storage-path` | `/home/files` | Local storage path |
| `--node-version` | `22` | Node.js version |
| `--uninstall` | — | ถอนการติดตั้ง |

### Examples

```bash
# 2 workers
curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-download/main/install.sh | sudo -E bash -s -- \
    --mongodb-uri "mongodb+srv://..." \
    --storage-path /home/files \
    -n 2

# Uninstall
curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-download/main/install.sh | sudo bash -s -- --uninstall
```

### After install

```bash
# ดู logs
journalctl -u "server-download@*" -f

# ดู worker 1
journalctl -u "server-download@1" -f

# Restart workers
for i in $(seq 1 2); do systemctl restart server-download@$i; done

# Stop workers
for i in $(seq 1 2); do systemctl stop server-download@$i; done
```

---

## Download Latest Release

```bash
# Linux amd64
curl -L https://github.com/vdohide-core/server-download/releases/latest/download/linux -o server-download
chmod +x server-download

# Linux ARM64
curl -L https://github.com/vdohide-core/server-download/releases/latest/download/linux-arm64 -o server-download
chmod +x server-download

# Scripts (Node.js SCP)
curl -L https://github.com/vdohide-core/server-download/releases/latest/download/scripts.tar.gz | tar xz
cd scripts && npm install --production
```

---

## Configuration (.env)

```env
# Required
MONGODB_URI=mongodb+srv://user:pass@cluster.mongodb.net/platform

# Optional — ถ้าไม่ตั้งค่าจะใช้ storage จาก DB
STORAGE_ID=your-storage-uuid
STORAGE_PATH=/home/files

# Optional — Worker ID (default: hostname@1)
WORKER_ID=worker-1

# Optional — Scraper URL สำหรับ HLS
SCRAPER_URL=http://localhost:8081
```

---

## Development

```bash
# Clone
git clone https://github.com/vdohide-core/server-download.git
cd server-download

# ติดตั้ง Node.js dependencies (SCP script)
cd scripts && npm install && cd ..

# สร้าง .env
cp .env.example .env
# แก้ไข MONGODB_URI

# Run
go run ./cmd

# Build all platforms
./build.bat
```

---

## Release

```bash
git tag v1.2.0
git push origin v1.2.0
```

GitHub Actions จะ build และ release อัตโนมัติพร้อม:
- `linux` — Linux amd64 binary
- `linux-arm64` — Linux ARM64 binary  
- `scripts.tar.gz` — Node.js SCP scripts

---

## Architecture

```
Worker Loop (5s tick)
├── resumeOrClaimProcess()   — resume งานที่ค้างของ worker นี้
├── findAndClaimFile()       — หา waiting video → claim เป็น processing
│   ├── sourceType=upload    → processIngest (S3 / local)
│   ├── sourceType=gdrive    → processGDrive (OAuth2)
│   └── sourceType=remote    → processHLS (m3u8 download)
└── findAndClaimFailedProcess() — retry failed jobs (max 3 ครั้ง)
```

## File Processing Flow

```
Download → Merge (FFmpeg h264+faststart) → Upload (SCP/Local) → Media Record → Done
```

## Collections Used

| Collection | การใช้งาน |
|---|---|
| `files` | หา waiting video, update status |
| `video_process` | track download/merge/upload progress |
| `ingests` | หา uploaded file path (S3 / local) |
| `storages` | หา storage config (SSH, S3, path) |
| `medias` | บันทึก processed media record |
| `oauths` | OAuth2 token สำหรับ GDrive |
| `settings` | `download_enabled`, `url_scraping` |
