#!/bin/bash

# Server Download Installation Script
# Usage: curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-download/main/install.sh | sudo -E bash -s -- [OPTIONS]

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Defaults
WORKER_COUNT=1
UNINSTALL=false
MONGODB_URI=""
STORAGE_ID=""
STORAGE_PATH="/home/files"
NODE_VERSION="22"

APP_NAME="server-download"
APP_DIR="/opt/$APP_NAME"
SERVICE_NAME="server-download"
GITHUB_REPO="vdohide-core/server-download"
RELEASES_URL="https://github.com/$GITHUB_REPO/releases/latest/download"

print_status()  { echo -e "${GREEN}[INFO]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

# Parse args
while [[ $# -gt 0 ]]; do
    case $1 in
        --uninstall)       UNINSTALL=true; shift ;;
        --count|-w)        WORKER_COUNT="$2"; shift 2 ;;
        --mongodb-uri)     MONGODB_URI="$2"; shift 2 ;;
        --storage-id)      STORAGE_ID="$2"; shift 2 ;;
        --storage-path)    STORAGE_PATH="$2"; shift 2 ;;
        --node-version)    NODE_VERSION="$2"; shift 2 ;;
        -h|--help)
            echo "Server Download Installer"
            echo ""
            echo "Usage: curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo -E bash -s -- [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --uninstall          Uninstall completely"
            echo "  --count NUM          Number of worker instances (default: 1)"
            echo "  -w NUM               Alias for --count"
            echo "  --mongodb-uri URI    MongoDB connection string"
            echo "  --storage-id ID      Storage ID (optional)"
            echo "  --storage-path DIR   Storage path (default: /home/files)"
            echo "  --node-version VER   Node.js version (default: 22)"
            echo "  -h, --help           Show this help"
            echo ""
            echo "Examples:"
            echo "  # Install with 1 worker"
            echo "  curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo -E bash"
            echo ""
            echo "  # Install with 2 workers + MongoDB"
            echo "  curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo -E bash -s -- \\"
            echo "      --mongodb-uri \"mongodb+srv://user:pass@host/db\" --count 2"
            echo ""
            echo "  # Uninstall"
            echo "  curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo -E bash -s -- --uninstall"
            exit 0 ;;
        *)
            print_error "Unknown option: $1"; exit 1 ;;
    esac
done

# ─── Uninstall ────────────────────────────────────────────────
if [ "$UNINSTALL" = true ]; then
    print_warning "⚠️  Starting Uninstallation..."
    for i in $(seq 1 20); do
        systemctl stop "${SERVICE_NAME}@${i}"    2>/dev/null || true
        systemctl disable "${SERVICE_NAME}@${i}" 2>/dev/null || true
    done
    systemctl stop "${SERVICE_NAME}"    2>/dev/null || true
    systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
    [ -f "/etc/systemd/system/${SERVICE_NAME}@.service" ] && rm "/etc/systemd/system/${SERVICE_NAME}@.service"
    [ -f "/etc/systemd/system/${SERVICE_NAME}.service"  ] && rm "/etc/systemd/system/${SERVICE_NAME}.service"
    systemctl daemon-reload
    [ -d "$APP_DIR" ] && rm -rf "$APP_DIR"
    print_status "✅ Uninstalled successfully!"
    exit 0
fi

# Check root
if [ "$(id -u)" -ne 0 ]; then
    print_error "This script must be run as root (use sudo)"
    exit 1
fi

print_status "🚀 Starting Installation... (Workers: $WORKER_COUNT)"

# ─── System Dependencies ──────────────────────────────────────
print_status "Installing system dependencies (curl, jq, ffmpeg)..."
if command -v apt-get &>/dev/null; then
    apt-get update -qq
    apt-get install -y -qq curl jq ffmpeg
elif command -v yum &>/dev/null; then
    yum install -y curl jq ffmpeg
elif command -v dnf &>/dev/null; then
    dnf install -y curl jq ffmpeg
fi

for cmd in curl jq ffmpeg; do
    if ! command -v $cmd &>/dev/null; then
        print_error "$cmd not found. Please install it manually."
        exit 1
    fi
done

# ─── Node.js via NVM ─────────────────────────────────────────
print_status "Installing Node.js $NODE_VERSION via NVM..."
export NVM_DIR="${NVM_DIR:-/root/.nvm}"

if [ ! -d "$NVM_DIR" ]; then
    print_status "Installing NVM..."
    curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash
fi

[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"
source ~/.bashrc 2>/dev/null || true
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"

if ! command -v nvm &>/dev/null; then
    print_error "Failed to load NVM. Run: source ~/.bashrc and retry."
    exit 1
fi

nvm install $NODE_VERSION
nvm use $NODE_VERSION
nvm alias default $NODE_VERSION
print_status "Node.js: $(node --version) | npm: $(npm --version)"

# ─── Stop existing services ───────────────────────────────────
print_status "Stopping existing services..."
systemctl stop ${SERVICE_NAME}@* 2>/dev/null || true
systemctl stop ${SERVICE_NAME}   2>/dev/null || true

# ─── Create app directory ─────────────────────────────────────
print_status "Creating app directory: $APP_DIR"
mkdir -p "$APP_DIR/scripts"
cd "$APP_DIR"

# ─── Download binary ──────────────────────────────────────────
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    BINARY="linux"
elif [ "$ARCH" = "aarch64" ]; then
    BINARY="linux-arm64"
else
    print_error "Unsupported architecture: $ARCH"
    exit 1
fi

print_status "Downloading binary ($BINARY) from latest release..."
curl -fsSL "$RELEASES_URL/$BINARY" -o "$APP_DIR/$APP_NAME"
chmod +x "$APP_DIR/$APP_NAME"
print_status "Binary downloaded."

# ─── Download & install scripts ───────────────────────────────
print_status "Downloading SCP scripts..."
curl -fsSL "$RELEASES_URL/scripts.tar.gz" | tar xz -C "$APP_DIR" --strip-components=0
print_status "Installing npm dependencies..."
cd "$APP_DIR/scripts"
npm install --production
cd "$APP_DIR"
print_status "Scripts installed."

# ─── Create .env ─────────────────────────────────────────────
print_status "Creating .env file..."
cat > "$APP_DIR/.env" <<EOF
MONGODB_URI=$MONGODB_URI
STORAGE_ID=$STORAGE_ID
STORAGE_PATH=$STORAGE_PATH
EOF

# ─── Systemd service template ─────────────────────────────────
print_status "Creating systemd service template..."
NODE_PATH=$(which node)
NODE_DIR=$(dirname "$NODE_PATH")

cat > /etc/systemd/system/${SERVICE_NAME}@.service <<EOF
[Unit]
Description=Server Download Worker %i
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/$APP_NAME
Restart=always
RestartSec=5
EnvironmentFile=$APP_DIR/.env
Environment="WORKER_ID=$(hostname)@%i"
Environment="PATH=$NODE_DIR:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

[Install]
WantedBy=multi-user.target
EOF

# ─── Enable & start workers ───────────────────────────────────
systemctl daemon-reload
print_status "Starting $WORKER_COUNT worker(s)..."
for i in $(seq 1 $WORKER_COUNT); do
    systemctl enable ${SERVICE_NAME}@$i
    systemctl start  ${SERVICE_NAME}@$i
    sleep 0.3
done

# ─── Verify ───────────────────────────────────────────────────
sleep 2
RUNNING=0
for i in $(seq 1 $WORKER_COUNT); do
    systemctl is-active --quiet ${SERVICE_NAME}@$i && RUNNING=$((RUNNING+1))
done

echo ""
echo "============================================"
if [ $RUNNING -eq $WORKER_COUNT ]; then
    print_status "✅ Installation completed successfully!"
else
    print_warning "$RUNNING of $WORKER_COUNT workers running — check logs below"
    journalctl -u "${SERVICE_NAME}@1" -n 15 --no-pager
fi
echo "============================================"
echo ""
echo "  Directory:  $APP_DIR"
echo "  Workers:    $RUNNING / $WORKER_COUNT running"
echo ""
echo "  Commands:"
echo "    View logs:   journalctl -u \"${SERVICE_NAME}@*\" -f"
echo "    Worker 1:    journalctl -u \"${SERVICE_NAME}@1\" -f"
echo "    Restart all: for i in \$(seq 1 $WORKER_COUNT); do systemctl restart ${SERVICE_NAME}@\$i; done"
echo "    Stop all:    for i in \$(seq 1 $WORKER_COUNT); do systemctl stop ${SERVICE_NAME}@\$i; done"
echo "    Uninstall:   curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo bash -s -- --uninstall"
echo "============================================"
