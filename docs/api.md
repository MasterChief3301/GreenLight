# API reference

[← Docs index](README.md)

All `/api/*` routes require an `X-API-Key` header. Manage keys under
**Settings → API keys**; on first run a bootstrap key is printed to the logs.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/requests` | Create an approval request. |
| `GET` | `/api/requests/{id}` | Fetch a request's current state. |
| `GET` | `/api/requests?status=&source=&category=` | List requests (max 200, newest first). |
| `POST` | `/api/requests/{id}/cancel` | Withdraw a pending request (no callback sent). |
| `GET` | `/healthz` | Liveness/DB check (no auth). |

## `POST /api/requests`

Creates a request and (if ntfy is configured) publishes a notification.

### Request body

| Field | Required | Type | Notes |
|---|:---:|---|---|
| `title` | ✅ | string | Short summary shown everywhere. |
| `resume_url` | ✅ | string | n8n Wait-node callback; the decision is POSTed here. |
| `description` | | string | Longer detail shown in the UI. |
| `source` | | string | Which automation asked (rule matching + filtering). |
| `category` | | string | Free-form grouping (rule matching + filtering). |
| `priority` | | string | `low` \| `normal` \| `high` (maps to ntfy priority). |
| `default_action` | | string | `approve` \| `reject`. Omit to resolve from [rules](configuration.md#default-rules). |
| `timeout_seconds` | | int | Omit to resolve from rules/config. |
| `resume_payload_extra` | | object | JSON merged into the callback body. |
| `metadata` | | object | JSON rendered on the detail page. |

### Example

```bash
curl -X POST https://greenlight.example.com/api/requests \
  -H "X-API-Key: $GREENLIGHT_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
        "title": "Deploy v2.1 to prod",
        "description": "Ship the release branch.",
        "source": "n8n-ci",
        "category": "deploy",
        "priority": "high",
        "timeout_seconds": 300,
        "default_action": "reject",
        "resume_url": "{{ $execution.resumeUrl }}",
        "metadata": { "commit": "abc123" }
      }'
```

### Response — `201 Created`

```json
{
  "id": "fddae6579fe175ff60",
  "url": "https://greenlight.example.com/requests/fddae6579fe175ff60",
  "title": "Deploy v2.1 to prod",
  "status": "pending",
  "priority": "high",
  "default_action": "reject",
  "timeout_seconds": 300,
  "deadline": "2026-07-11T13:35:55Z",
  "created_at": "2026-07-11T13:30:55Z"
}
```

Validation failures return `400` with `{ "error": "…" }`. A missing/invalid key
returns `401`.

## Callback payload

When a request resolves (by you or by timeout), Greenlight POSTs to its
`resume_url`:

```json
{
  "id": "fddae6579fe175ff60",
  "decision": "approved",
  "status": "approved",
  "decided_by": "user",
  "comment": "looks good",
  "decided_at": "2026-07-11T13:32:10Z"
}
```

| Field | Values |
|---|---|
| `decision` | `approved` \| `rejected` |
| `status` | `approved` \| `rejected` \| `expired-approved` \| `expired-rejected` |
| `decided_by` | `user` \| `timeout` |
| `comment` | present only if you left one |

Any fields from `resume_payload_extra` are merged in and **never overwrite** the
core fields above. Failed deliveries are retried with exponential backoff and, if
they ultimately fail, flagged in the UI with a **“callback failed”** badge.
Cancelled requests do **not** trigger a callback.

## Other endpoints

- **`GET /api/requests/{id}`** → the same object shape as the create response.
  `404` if unknown.
- **`GET /api/requests`** → `{ "requests": [ … ], "count": N }`. Filter with
  `status`, `source`, `category` query params.
- **`POST /api/requests/{id}/cancel`** → resolves a pending request to
  `cancelled`. `409` if it was already resolved, `404` if unknown.
