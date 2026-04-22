/**
 * SCP Upload Script
 * Called by Go server-download after merge to upload files via SCP
 * 
 * Usage: node scp-upload.js <json-config>
 * 
 * JSON config (base64 encoded):
 * {
 *   "localPath": "/path/to/file.mp4",
 *   "host": "192.168.1.1",
 *   "port": 22,
 *   "username": "user",
 *   "password": "pass",
 *   "remotePath": "/home/files",
 *   "fileName": "video.mp4"
 * }
 * 
 * Output: JSON lines
 *   {"type":"progress","percent":50,"transferred":1024,"total":2048}
 *   {"type":"success","remotePath":"/home/files/video.mp4","fileSize":2048}
 *   {"type":"error","message":"Connection refused"}
 */

const scp = require('node-scp');
const Client = scp.Client || scp.default || scp;
const fs = require('fs');
const path = require('path');

async function main() {
  const configBase64 = process.argv[2];
  if (!configBase64) {
    output('error', { message: 'Missing config argument' });
    process.exit(1);
  }

  let config;
  try {
    config = JSON.parse(Buffer.from(configBase64, 'base64').toString('utf-8'));
  } catch (e) {
    output('error', { message: `Invalid config: ${e.message}` });
    process.exit(1);
  }

  const { localPath, host, port = 22, username, password, remotePath, fileName } = config;

  // Validate
  if (!localPath || !host || !username || !remotePath) {
    output('error', { message: 'Missing required fields: localPath, host, username, remotePath' });
    process.exit(1);
  }

  if (!fs.existsSync(localPath)) {
    output('error', { message: `File not found: ${localPath}` });
    process.exit(1);
  }

  const uploadFileName = fileName || path.basename(localPath);
  const stats = fs.statSync(localPath);
  const fileSize = stats.size;

  output('start', { fileName: uploadFileName, fileSize, host, remotePath });

  let client;
  try {
    // Connect (auto-accept host key for first connection)
    const authConfig = { host, port, username, hostVerifier: () => true };
    if (password) authConfig.password = password;

    client = await Client(authConfig);

    // Ensure remote directory exists
    const dirExists = await client.exists(remotePath);
    if (!dirExists) {
      await client.mkdir(remotePath, { recursive: true });
    }

    const targetPath = `${remotePath}/${uploadFileName}`;
    let lastPercent = 0;

    // Upload with progress
    await client.uploadFile(localPath, targetPath, {
      step: (transferred, chunk, total) => {
        const percent = Math.round((transferred / total) * 100);
        // Report every 5%
        if (percent >= lastPercent + 5 || percent === 100) {
          lastPercent = percent;
          output('progress', { percent, transferred, total });
        }
      }
    });

    await client.close();

    output('success', {
      remotePath: targetPath,
      fileName: uploadFileName,
      fileSize
    });

  } catch (err) {
    if (client) {
      try { await client.close(); } catch (_) {}
    }
    output('error', { message: err.message || String(err) });
    process.exit(1);
  }
}

function output(type, data) {
  console.log(JSON.stringify({ type, ...data }));
}

main();
