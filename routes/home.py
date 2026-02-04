# -*- coding: utf-8 -*-
"""首页和 WebUI 路由"""
import os
from fastapi import APIRouter, Request
from fastapi.responses import HTMLResponse, FileResponse

from core.config import STATIC_ADMIN_DIR

router = APIRouter()

# 首页 HTML（内嵌避免依赖模板目录）
WELCOME_HTML = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>DS2API - DeepSeek to OpenAI API</title>
    <meta name="description" content="DS2API - 将 DeepSeek 网页版转换为 OpenAI 兼容 API">
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Orbitron:wght@700&display=swap" rel="stylesheet">
    <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0%25' y1='0%25' x2='100%25' y2='100%25'%3E%3Cstop offset='0%25' stop-color='%23f59e0b'/%3E%3Cstop offset='100%25' stop-color='%23ef4444'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect rx='20' width='100' height='100' fill='url(%23g)'/%3E%3Ctext x='50' y='68' font-family='Arial,sans-serif' font-size='48' font-weight='bold' fill='white' text-anchor='middle'%3EDS%3C/text%3E%3C/svg%3E">
    <style>
        :root {
            --primary: #f59e0b;
            --primary-glow: rgba(245, 158, 11, 0.4);
            --secondary: #ef4444;
            --bg: #030712;
            --card-bg: rgba(255, 255, 255, 0.03);
            --card-border: rgba(255, 255, 255, 0.08);
            --text-main: #f9fafb;
            --text-dim: #9ca3af;
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }
        
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
            background-color: var(--bg);
            color: var(--text-main);
            min-height: 100vh;
            overflow-x: hidden;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            position: relative;
        }

        /* Animated Background */
        .bg-glow {
            position: fixed;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            z-index: -1;
            background: 
                radial-gradient(circle at 20% 30%, rgba(245, 158, 11, 0.05) 0%, transparent 40%),
                radial-gradient(circle at 80% 70%, rgba(239, 68, 68, 0.05) 0%, transparent 40%);
        }

        .blob {
            position: absolute;
            width: 400px;
            height: 400px;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            filter: blur(80px);
            opacity: 0.15;
            border-radius: 50%;
            z-index: -1;
            animation: move 20s infinite alternate;
        }

        @keyframes move {
            from { transform: translate(-10%, -10%) scale(1); }
            to { transform: translate(10%, 10%) scale(1.1); }
        }

        .container {
            width: 100%;
            max-width: 900px;
            padding: 2rem;
            text-align: center;
        }

        .logo-section {
            margin-bottom: 3rem;
            animation: fadeInUp 0.8s ease-out;
        }

        .logo {
            font-family: 'Orbitron', sans-serif;
            font-size: clamp(3rem, 10vw, 5rem);
            font-weight: 700;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
            letter-spacing: -2px;
            margin-bottom: 0.5rem;
            display: inline-block;
        }

        .subtitle {
            color: var(--text-dim);
            font-size: 1.25rem;
            max-width: 600px;
            margin: 0 auto;
            line-height: 1.6;
        }

        .actions {
            display: flex;
            gap: 1rem;
            justify-content: center;
            margin-bottom: 4rem;
            flex-wrap: wrap;
            animation: fadeInUp 0.8s ease-out 0.2s backwards;
        }

        .btn {
            padding: 0.8rem 2rem;
            border-radius: 12px;
            text-decoration: none;
            font-weight: 600;
            font-size: 1rem;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .btn-primary {
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            color: white;
            box-shadow: 0 4px 15px var(--primary-glow);
        }

        .btn-primary:hover {
            transform: translateY(-3px) scale(1.02);
            box-shadow: 0 8px 25px var(--primary-glow);
        }

        .btn-secondary {
            background: var(--card-bg);
            color: var(--text-main);
            border: 1px solid var(--card-border);
            backdrop-filter: blur(10px);
        }

        .btn-secondary:hover {
            background: rgba(255, 255, 255, 0.08);
            border-color: rgba(255, 255, 255, 0.2);
            transform: translateY(-2px);
        }

        .features-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1.5rem;
            margin-top: 1rem;
            animation: fadeInUp 0.8s ease-out 0.4s backwards;
        }

        .feature-card {
            background: var(--card-bg);
            border: 1px solid var(--card-border);
            border-radius: 16px;
            padding: 1.5rem;
            text-align: left;
            transition: all 0.3s ease;
            backdrop-filter: blur(8px);
        }

        .feature-card:hover {
            border-color: rgba(245, 158, 11, 0.3);
            background: rgba(255, 255, 255, 0.05);
            transform: translateY(-5px);
        }

        .feature-icon {
            font-size: 1.5rem;
            margin-bottom: 1rem;
            display: block;
        }

        .feature-card h3 {
            font-size: 1.1rem;
            margin-bottom: 0.5rem;
            font-weight: 600;
        }

        .feature-card p {
            color: var(--text-dim);
            font-size: 0.9rem;
            line-height: 1.5;
        }

        footer {
            margin-top: 4rem;
            padding: 2rem;
            color: var(--text-dim);
            font-size: 0.875rem;
            animation: fadeInUp 0.8s ease-out 0.6s backwards;
        }

        @keyframes fadeInUp {
            from { opacity: 0; transform: translateY(20px); }
            to { opacity: 1; transform: translateY(0); }
        }

        @media (max-width: 640px) {
            .logo { font-size: 3.5rem; }
            .container { padding: 1.5rem; }
        }
    </style>
</head>
<body>
    <div class="bg-glow"></div>
    <div class="blob" style="top: 10%; left: 15%;"></div>
    <div class="blob" style="bottom: 10%; right: 15%; animation-delay: -5s;"></div>

    <div class="container">
        <header class="logo-section">
            <div class="logo">DS2API</div>
            <p class="subtitle">DeepSeek to OpenAI & Claude Compatible API Interface</p>
        </header>

        <div class="actions">
            <a href="/admin" class="btn btn-primary">
                <span>🎛️</span> 管理面板
            </a>
            <a href="/v1/models" class="btn btn-secondary">
                <span>📡</span> API 状态
            </a>
            <a href="https://github.com/CJackHwang/ds2api" class="btn btn-secondary" target="_blank">
                <span>📦</span> GitHub
            </a>
        </div>

        <div class="features-grid">
            <div class="feature-card">
                <span class="feature-icon">🚀</span>
                <h3>全面兼容</h3>
                <p>完美适配 OpenAI 与 Claude API 格式，无缝集成现有工具。</p>
            </div>
            <div class="feature-card">
                <span class="feature-icon">⚖️</span>
                <h3>负载均衡</h3>
                <p>内置智能轮询机制，支持多账号并发，稳定高效。</p>
            </div>
            <div class="feature-card">
                <span class="feature-icon">🧠</span>
                <h3>深度思考</h3>
                <p>完整支持 推理过程输出，让思考可见。</p>
            </div>
            <div class="feature-card">
                <span class="feature-icon">🔍</span>
                <h3>联网搜索</h3>
                <p>集成 DeepSeek 原生搜索能力，获取最新实时资讯。</p>
            </div>
        </div>

        <footer>
            <p>&copy; 2026 DS2API Project. Designed for flexibility & performance.</p>
        </footer>
    </div>
</body>
</html>"""


@router.get("/")
def index(request: Request):
    return HTMLResponse(content=WELCOME_HTML)


@router.get("/admin")
@router.get("/admin/{path:path}")
async def webui(request: Request, path: str = ""):
    """提供 WebUI 静态文件"""
    # 检查 static/admin 目录是否存在
    if not os.path.isdir(STATIC_ADMIN_DIR):
        return HTMLResponse(
            content="<h1>WebUI not built</h1><p>Run <code>cd webui && npm run build</code> first.</p>",
            status_code=404
        )
    
    # 如果请求的是具体文件（如 js, css）
    if path and "." in path:
        file_path = os.path.join(STATIC_ADMIN_DIR, path)
        if os.path.isfile(file_path):
            cache_control = "public, max-age=31536000, immutable"
            if path.startswith("assets/"):
                headers = {"Cache-Control": cache_control}
            else:
                headers = {"Cache-Control": "no-store, must-revalidate"}
            return FileResponse(file_path, headers=headers)
        return HTMLResponse(content="Not Found", status_code=404)
    
    # 否则返回 index.html（SPA 路由）
    index_path = os.path.join(STATIC_ADMIN_DIR, "index.html")
    if os.path.isfile(index_path):
        headers = {"Cache-Control": "no-store, must-revalidate"}
        return FileResponse(index_path, headers=headers)
    
    return HTMLResponse(content="index.html not found", status_code=404)

