from ds2api.app import app
from ds2api.config import IS_VERCEL


if __name__ == "__main__" and not IS_VERCEL:
    import os

    import uvicorn

    port = int(os.getenv("PORT", "5001"))
    uvicorn.run(app, host="0.0.0.0", port=port)
