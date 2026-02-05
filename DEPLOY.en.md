# DS2API Deployment Guide

Language: [中文](DEPLOY.md) | [English](DEPLOY.en.md)

This document covers all supported DS2API deployment methods.

---

## Table of Contents

- [Vercel Deployment (Recommended)](#vercel-deployment-recommended)
- [Docker Deployment (Recommended)](#docker-deployment-recommended)
- [Local Development](#local-development)
- [Production Deployment](#production-deployment)
- [FAQ](#faq)

---

## Vercel Deployment (Recommended)

### One-click deployment

[![Deploy with Vercel](https://vercel.com/button)](https://vercel.com/new/clone?repository-url=https%3A%2F%2Fgithub.com%2FCJackHwang%2Fds2api&env=DS2API_ADMIN_KEY&envDescription=Admin%20console%20access%20key%20%28required%29&envLink=https%3A%2F%2Fgithub.com%2FCJackHwang%2Fds2api%23environment-variables&project-name=ds2api&repository-name=ds2api)

### Steps

1. **Click the deploy button**
   - Sign in to GitHub
   - Authorize Vercel access

2. **Set environment variables**
   - `DS2API_ADMIN_KEY`: Admin console password (**required**)

3. **Wait for deployment**
   - Vercel builds and deploys automatically
   - You will receive a deployment URL

4. **Configure accounts**
   - Visit `https://your-project.vercel.app/admin`
   - Log in with the admin key
   - Add DeepSeek accounts
   - Set custom API keys

5. **Sync configuration**
   - Click "Sync to Vercel"
   - The first sync requires a Vercel token and project ID
   - After sync, the configuration is persisted

### Get Vercel credentials

**Vercel token**:
1. Visit https://vercel.com/account/tokens
2. Click "Create Token"
3. Set a name and expiration
4. Copy the token

**Project ID**:
1. Open your Vercel project
2. Go to Settings → General
3. Copy the "Project ID"

---

## Local Development

### Requirements

- Python 3.9+
- Node.js 18+ (WebUI development)
- pip

### Quick start

```bash
# 1. Clone the repo
git clone https://github.com/CJackHwang/ds2api.git
cd ds2api

# 2. Install Python dependencies
pip install -r requirements.txt

# 3. Configure accounts
cp config.example.json config.json
# Edit config.json and fill in DeepSeek account info

# 4. Start the service
python dev.py
```

### Config example

```json
{
  "keys": ["my-api-key-1", "my-api-key-2"],
  "accounts": [
    {
      "email": "your-email@example.com",
      "password": "your-password",
      "token": ""
    },
    {
      "mobile": "12345678901",
      "password": "your-password",
      "token": ""
    }
  ]
}
```

**Notes**:
- `keys`: Custom API keys for calling the service
- `accounts`: DeepSeek Web accounts
  - Supports `email` or `mobile` login
  - Leave `token` blank; it will be fetched automatically

### WebUI development

```bash
# Enter the WebUI directory
cd webui

# Install dependencies
npm install

# Start the dev server
npm run dev
```

The WebUI dev server runs on `http://localhost:5173` and proxies API requests to `http://localhost:5001`.

### WebUI build

Build artifacts are located in `static/admin/`.

**Automatic build (recommended)**:
- Vercel builds the WebUI during deployment (see `vercel.json` `buildCommand`)
- The GitHub Actions WebUI build workflow is disabled
- `static/admin/` build artifacts are no longer committed

**Manual build**:
```bash
# Option 1: use script
./scripts/build-webui.sh

# Option 2: run directly
cd webui
npm install
npm run build
```

> **Contributor note**: No manual build is required after modifying WebUI; Vercel deploys will build it automatically.

---

## Docker Deployment (Recommended)

Docker uses a **non-invasive, decoupled design**:
- Dockerfile executes standard Python steps and avoids hardcoded project configs
- WebUI is built during image build (for non-Vercel deployments)
- Configuration lives in environment variables and `.env`
- **Rebuild the image to update code without touching Docker config**

### Quick start (Docker Compose)

```bash
# 1. Copy the environment template
cp .env.example .env
# Edit .env with DS2API_ADMIN_KEY and DS2API_CONFIG_JSON

# 2. Start the service
docker-compose up -d

# 3. Check logs
docker-compose logs -f

# 4. Rebuild after code updates
docker-compose up -d --build
```

### Mount a config file

To use `config.json` instead of environment variables:

```yaml
# docker-compose.yml
services:
  ds2api:
    build: .
    ports:
      - "5001:5001"
    environment:
      - DS2API_ADMIN_KEY=your-admin-key
    volumes:
      - ./config.json:/app/config.json:ro
    restart: unless-stopped
```

### Docker CLI deployment

```bash
# Build the image
docker build -t ds2api:latest .

# Run with env variables
docker run -d \
  --name ds2api \
  -p 5001:5001 \
  -e DS2API_ADMIN_KEY=your-admin-key \
  -e DS2API_CONFIG_JSON='{"keys":["api-key"],"accounts":[...]}' \
  --restart unless-stopped \
  ds2api:latest

# Or mount a config file
docker run -d \
  --name ds2api \
  -p 5001:5001 \
  -e DS2API_ADMIN_KEY=your-admin-key \
  -v $(pwd)/config.json:/app/config.json:ro \
  --restart unless-stopped \
  ds2api:latest
```

### Development mode (hot reload)

```bash
# Use the dev compose file to enable hot reload
docker-compose -f docker-compose.dev.yml up
```

Development mode:
- Source code is mounted into the container
- Log level set to DEBUG
- Reads local `config.json`

### Maintenance commands

```bash
# Check container status
docker-compose ps

# View logs
docker-compose logs -f ds2api

# Restart
docker-compose restart

# Stop
docker-compose down

# Full rebuild (clear cache)
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

---

## Production Deployment

### Using systemd (Linux)

1. **Create the service file**

```bash
sudo nano /etc/systemd/system/ds2api.service
```

```ini
[Unit]
Description=DS2API Service
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/ds2api
ExecStart=/usr/bin/python3 app.py
Restart=always
RestartSec=10
Environment=PORT=5001
Environment=DS2API_ADMIN_KEY=your-admin-key

[Install]
WantedBy=multi-user.target
```

2. **Start the service**

```bash
sudo systemctl daemon-reload
sudo systemctl enable ds2api
sudo systemctl start ds2api
```

3. **Check status**

```bash
sudo systemctl status ds2api
sudo journalctl -u ds2api -f
```

### Nginx reverse proxy

```nginx
server {
    listen 80;
    server_name api.yourdomain.com;

    # SSL configuration (recommended)
    # listen 443 ssl http2;
    # ssl_certificate /path/to/cert.pem;
    # ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:5001;
        proxy_http_version 1.1;
        
        # Disable buffering for SSE
        proxy_buffering off;
        proxy_cache off;
        
        # Connection settings
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # SSE timeouts
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
        
        # Chunked transfer
        chunked_transfer_encoding on;
        tcp_nopush on;
        tcp_nodelay on;
        keepalive_timeout 120;
    }
}
```

---

## FAQ

### Q: What if account validation fails?

**A**: Check the following:
1. Confirm the DeepSeek account password is correct
2. Ensure the account is not banned or requires verification
3. Log in once in a browser
4. Check logs for detailed errors

### Q: Streaming responses disconnect?

**A**:
1. Check Nginx / reverse proxy config and ensure `proxy_buffering` is off
2. Increase `proxy_read_timeout`
3. Verify network stability

### Q: Configuration lost after Vercel deploy?

**A**:
1. Ensure you clicked "Sync to Vercel"
2. Verify the Vercel token is valid and unexpired
3. Ensure the project ID is correct

### Q: How to update to the latest version?

**Local deployment**:
```bash
git pull origin main
pip install -r requirements.txt
# Restart the service
```

**Docker deployment**:
```bash
# Pull the latest code
git pull origin main

# Rebuild and start (Docker config unchanged)
docker-compose up -d --build
```

**Vercel deployment**:
- The project auto-syncs from GitHub
- Or trigger a redeploy in the Vercel console

### Q: How do I view logs?

**Local dev**:
```bash
# Set log level
export LOG_LEVEL=DEBUG
python dev.py
```

**Vercel**:
- Vercel console → Project → Deployments → Logs

### Q: Token counting is inaccurate?

**A**: DS2API uses a heuristic estimate (characters / 4). The official OpenAI tokenizer may differ, so treat it as a reference only.

---

## Get Help

- **GitHub Issues**: https://github.com/CJackHwang/ds2api/issues
- **Docs**: https://github.com/CJackHwang/ds2api
