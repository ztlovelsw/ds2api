# DS2API 部署指南

语言 / Language: [中文](DEPLOY.md) | [English](DEPLOY.en.md)

本文档详细介绍 DS2API 的各种部署方式。

---

## 目录

- [Vercel 部署（推荐）](#vercel-部署推荐)
- [Docker 部署（推荐）](#docker-部署推荐)
- [本地开发](#本地开发)
- [生产环境部署](#生产环境部署)
- [常见问题](#常见问题)

---

## Vercel 部署（推荐）

### 一键部署

[![Deploy with Vercel](https://vercel.com/button)](https://vercel.com/new/clone?repository-url=https%3A%2F%2Fgithub.com%2FCJackHwang%2Fds2api&env=DS2API_ADMIN_KEY&envDescription=管理面板访问密码（必填）&envLink=https%3A%2F%2Fgithub.com%2FCJackHwang%2Fds2api%23环境变量&project-name=ds2api&repository-name=ds2api)

### 部署步骤

1. **点击部署按钮**
   - 登录你的 GitHub 账号
   - 授权 Vercel 访问

2. **设置环境变量**
   - `DS2API_ADMIN_KEY`: 管理面板密码（**必填**）

3. **等待部署完成**
   - Vercel 会自动构建并部署项目
   - 部署完成后获得访问 URL

4. **配置账号**
   - 访问 `https://your-project.vercel.app/admin`
   - 输入管理密码登录
   - 添加 DeepSeek 账号
   - 设置自定义 API Key

5. **同步配置**
   - 点击「同步到 Vercel」按钮
   - 首次需要输入 Vercel Token 和 Project ID
   - 同步成功后配置会持久化

### 获取 Vercel 凭证

**Vercel Token**:
1. 访问 https://vercel.com/account/tokens
2. 点击 "Create Token"
3. 设置名称和有效期
4. 复制生成的 Token

**Project ID**:
1. 进入 Vercel 项目页面
2. 点击 Settings -> General
3. 复制 "Project ID"

---

## 本地开发

### 环境要求

- Python 3.9+
- Node.js 18+ (WebUI 开发)
- pip

### 快速开始

```bash
# 1. 克隆项目
git clone https://github.com/CJackHwang/ds2api.git
cd ds2api

# 2. 安装 Python 依赖
pip install -r requirements.txt

# 3. 配置账号
cp config.example.json config.json
# 编辑 config.json，填入 DeepSeek 账号信息

# 4. 启动服务
python dev.py
```

### 配置文件示例

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

**说明**：
- `keys`: 自定义 API Key，用于调用本服务的接口
- `accounts`: DeepSeek 网页版账号
  - 支持 `email` 或 `mobile` 登录
  - `token` 留空，系统会自动获取

### WebUI 开发

```bash
# 进入 WebUI 目录
cd webui

# 安装依赖
npm install

# 启动开发服务器
npm run dev
```

WebUI 开发服务器会启动在 `http://localhost:5173`，并自动代理 API 请求到后端 `http://localhost:5001`。

### WebUI 构建

WebUI 构建产物位于 `static/admin/` 目录。

**自动构建（推荐）**：
- 当前由 Vercel 在部署时执行 WebUI 构建（见 `vercel.json` 的 `buildCommand`）
- GitHub Actions 的 WebUI 自动构建流程已关闭
- `static/admin/` 构建产物不再提交到仓库

**手动构建**：
```bash
# 方式1：使用脚本
./scripts/build-webui.sh

# 方式2：直接执行
cd webui
npm install
npm run build
```

> **贡献者注意**：修改 WebUI 后无需手动构建，Vercel 部署会自动构建。

---

## Docker 部署（推荐）

Docker 部署采用**零侵入、解耦设计**：
- Dockerfile 仅执行标准 Python 项目操作，不硬编码任何项目特定配置
- 构建镜像时会一并构建 WebUI（便于非 Vercel 部署直接访问管理面板）
- 所有配置通过环境变量和 `.env` 文件管理
- **主代码更新时只需重新构建镜像，无需修改 Docker 配置**

### 快速开始（Docker Compose）

```bash
# 1. 复制环境变量模板
cp .env.example .env
# 编辑 .env，填写 DS2API_ADMIN_KEY 和 DS2API_CONFIG_JSON

# 2. 启动服务
docker-compose up -d

# 3. 查看日志
docker-compose logs -f

# 4. 主代码更新后重新构建
docker-compose up -d --build
```

### 配置文件挂载方式

如需使用 `config.json` 而非环境变量：

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

### Docker 命令行部署

```bash
# 构建镜像
docker build -t ds2api:latest .

# 使用环境变量运行
docker run -d \
  --name ds2api \
  -p 5001:5001 \
  -e DS2API_ADMIN_KEY=your-admin-key \
  -e DS2API_CONFIG_JSON='{"keys":["api-key"],"accounts":[...]}' \
  --restart unless-stopped \
  ds2api:latest

# 或使用配置文件挂载
docker run -d \
  --name ds2api \
  -p 5001:5001 \
  -e DS2API_ADMIN_KEY=your-admin-key \
  -v $(pwd)/config.json:/app/config.json:ro \
  --restart unless-stopped \
  ds2api:latest
```

### 开发模式（热重载）

```bash
# 使用开发配置启动，代码修改实时生效
docker-compose -f docker-compose.dev.yml up
```

开发模式特性：
- 源代码挂载到容器，修改即时生效
- 日志级别设为 DEBUG
- 自动读取本地 `config.json`

### 维护命令

```bash
# 查看容器状态
docker-compose ps

# 查看日志
docker-compose logs -f ds2api

# 重启服务
docker-compose restart

# 停止服务
docker-compose down

# 完全重建（清除缓存）
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

---

## 生产环境部署

### 使用 systemd (Linux)

1. **创建服务文件**

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

2. **启动服务**

```bash
sudo systemctl daemon-reload
sudo systemctl enable ds2api
sudo systemctl start ds2api
```

3. **查看状态**

```bash
sudo systemctl status ds2api
sudo journalctl -u ds2api -f
```

### Nginx 反向代理

```nginx
server {
    listen 80;
    server_name api.yourdomain.com;

    # SSL 配置（推荐）
    # listen 443 ssl http2;
    # ssl_certificate /path/to/cert.pem;
    # ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:5001;
        proxy_http_version 1.1;
        
        # 关闭缓冲，支持 SSE
        proxy_buffering off;
        proxy_cache off;
        
        # 连接设置
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # SSE 超时设置
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
        
        # 分块传输
        chunked_transfer_encoding on;
        tcp_nopush on;
        tcp_nodelay on;
        keepalive_timeout 120;
    }
}
```

---

## 常见问题

### Q: 账号验证失败怎么办？

**A**: 检查以下几点：
1. 确认 DeepSeek 账号密码正确
2. 检查账号是否被封禁或需要验证
3. 尝试在浏览器中手动登录一次
4. 查看日志获取详细错误信息

### Q: 流式响应断开怎么办？

**A**: 
1. 检查 Nginx/反向代理配置，确保关闭了 `proxy_buffering`
2. 增加 `proxy_read_timeout` 超时时间
3. 检查网络连接稳定性

### Q: Vercel 部署后配置丢失？

**A**: 
1. 确保点击了「同步到 Vercel」按钮
2. 检查 Vercel Token 是否正确且未过期
3. 确认 Project ID 正确

### Q: 如何更新到新版本？

**本地部署**:
```bash
git pull origin main
pip install -r requirements.txt
# 重启服务
```

**Docker 部署**:
```bash
# 拉取最新代码
git pull origin main

# 重新构建并启动（无需修改 Docker 配置）
docker-compose up -d --build
```

**Vercel 部署**:
- 项目会自动从 GitHub 同步更新
- 或在 Vercel 控制台手动触发重新部署

### Q: 如何查看日志？

**本地开发**:
```bash
# 设置日志级别
export LOG_LEVEL=DEBUG
python dev.py
```

**Vercel**:
- 访问 Vercel 控制台 -> 项目 -> Deployments -> Logs

### Q: Token 计数不准确？

**A**: DS2API 使用估算方式计算 token 数量（字符数 / 4），与 OpenAI 官方的 tokenizer 可能有差异，仅供参考。

---

## 获取帮助

- **GitHub Issues**: https://github.com/CJackHwang/ds2api/issues
- **文档**: https://github.com/CJackHwang/ds2api
