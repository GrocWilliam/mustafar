# URL shortener service

An HTTP service that shortens URLs. Python 3 standard library only (no pip packages), data kept in memory.

Behavior:
- `POST /shorten` with the JSON body `{"url": "https://example.com/a"}` answers `201` and `{"code": "<code>", "short_url": "http://localhost:8080/<code>"}`. A code is 6 characters from `[A-Za-z0-9]`.
- Shortening the same URL twice returns the same code.
- A body that is not JSON, has no `url`, or whose `url` does not start with `http://` or `https://` answers `400` and `{"error": "<reason>"}`.
- `GET /<code>` answers `302` with `Location: <original url>`; an unknown code answers `404`.
- `GET /stats/<code>` answers `200` and `{"url": "<original url>", "hits": <n>}`, where `hits` counts the redirects served for that code; an unknown code answers `404`.
- `GET /health` answers `200` and `{"status": "ok"}`.

The server listens on port 8080, or on `$PORT` when set, and starts with `python3 -m shortener`.
