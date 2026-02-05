# 贡献指南

语言 / Language: [中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

感谢你对 DS2API 的贡献！

## 开发环境设置

### 后端

```bash
# 1. 克隆仓库
git clone https://github.com/CJackHwang/ds2api.git
cd ds2api

# 2. 创建虚拟环境（推荐）
python -m venv venv
source venv/bin/activate  # Windows: venv\Scripts\activate

# 3. 安装依赖
pip install -r requirements.txt

# 4. 配置
cp config.example.json config.json
# 编辑 config.json

# 5. 启动
python dev.py
```

### 前端 (WebUI)

```bash
cd webui
npm install
npm run dev
```

WebUI 语言包位于 `webui/src/locales/`，新增语言请在此处添加对应 JSON 文件。

## 代码规范

- **Python**: 遵循 PEP 8，使用 4 空格缩进
- **JavaScript/React**: 使用 4 空格缩进，使用函数组件
- **提交信息**: 使用语义化提交格式（如 `feat:`, `fix:`, `docs:`）

## 提交 PR

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/xxx`)
3. 提交更改 (`git commit -m 'feat: 添加xxx功能'`)
4. 推送分支 (`git push origin feature/xxx`)
5. 创建 Pull Request

## WebUI 构建

> **重要**: 修改 `webui/` 目录后 **无需手动构建**！

当 PR 合并到 `main` 分支后，GitHub Actions 会自动：
1. 构建 WebUI
2. 提交构建产物到 `static/admin/`

如果需要本地构建（测试用）：
```bash
./scripts/build-webui.sh
```

## 项目结构

```
ds2api/
├── app.py              # FastAPI 应用入口
├── dev.py              # 开发服务器
├── core/               # 核心模块
│   ├── auth.py         # 账号认证与轮询
│   ├── config.py       # 配置管理
│   ├── deepseek.py     # DeepSeek API 调用
│   ├── models.py       # 模型定义
│   ├── pow.py          # PoW 计算
│   └── sse_parser.py   # SSE 解析
├── routes/             # API 路由
│   ├── openai.py       # OpenAI 兼容接口
│   ├── claude.py       # Claude 兼容接口
│   ├── home.py         # 首页路由
│   └── admin/          # 管理接口
├── webui/              # React WebUI 源码
├── static/admin/       # WebUI 构建产物（自动生成）
└── scripts/            # 辅助脚本
```

## 问题反馈

- 使用 [GitHub Issues](https://github.com/CJackHwang/ds2api/issues) 报告问题
- 提供详细的复现步骤和日志信息
