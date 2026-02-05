# Contributing Guide

Language: [中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

Thank you for contributing to DS2API!

## Development Setup

### Backend

```bash
# 1. Clone the repo
git clone https://github.com/CJackHwang/ds2api.git
cd ds2api

# 2. Create a virtual environment (recommended)
python -m venv venv
source venv/bin/activate  # Windows: venv\Scripts\activate

# 3. Install dependencies
pip install -r requirements.txt

# 4. Configure
cp config.example.json config.json
# Edit config.json

# 5. Run
python dev.py
```

### Frontend (WebUI)

```bash
cd webui
npm install
npm run dev
```

WebUI language packs live in `webui/src/locales/`. Add new locale JSON files there.

## Code Standards

- **Python**: Follow PEP 8, use 4-space indentation
- **JavaScript/React**: Use 4-space indentation and function components
- **Commit messages**: Use semantic prefixes (e.g. `feat:`, `fix:`, `docs:`)

## Submitting a PR

1. Fork this repo
2. Create a feature branch (`git checkout -b feature/xxx`)
3. Commit your changes (`git commit -m 'feat: add xxx'`)
4. Push your branch (`git push origin feature/xxx`)
5. Open a Pull Request

## WebUI Build

> **Important**: After modifying `webui/`, **no manual build is required**.

When a PR is merged into `main`, GitHub Actions will automatically:
1. Build the WebUI
2. Commit build artifacts to `static/admin/`

If you need a local build (for testing):
```bash
./scripts/build-webui.sh
```

## Project Structure

```
ds2api/
├── app.py              # FastAPI entrypoint
├── dev.py              # Development server
├── core/               # Core modules
│   ├── auth.py         # Account auth & rotation
│   ├── config.py       # Configuration management
│   ├── deepseek.py     # DeepSeek API calls
│   ├── models.py       # Model definitions
│   ├── pow.py          # PoW calculations
│   └── sse_parser.py   # SSE parsing
├── routes/             # API routes
│   ├── openai.py       # OpenAI-compatible endpoints
│   ├── claude.py       # Claude-compatible endpoints
│   ├── home.py         # Landing page routes
│   └── admin/          # Admin endpoints
├── webui/              # React WebUI source
├── static/admin/       # WebUI build output (auto-generated)
└── scripts/            # Helper scripts
```

## Reporting Issues

- Use [GitHub Issues](https://github.com/CJackHwang/ds2api/issues)
- Provide detailed reproduction steps and logs
