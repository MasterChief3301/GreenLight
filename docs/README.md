# Greenlight documentation

Guides and reference for running and extending Greenlight. New here? Start with
the [project README](../README.md).

## Contents

| Doc | What's inside |
|---|---|
| [Architecture](architecture.md) | How it works — components, flow, request lifecycle, timeout engine (diagrams). |
| [Configuration](configuration.md) | Every environment variable + how default rules resolve. |
| [API reference](api.md) | Endpoints, request/response shapes, callback payload. |
| [n8n integration](n8n.md) | Wiring the Wait-node callback + the importable workflow. |
| [Notifications](notifications.md) | Pointing Greenlight at your ntfy server. |
| [Deployment](deployment.md) | Docker, reverse proxy/tunnel, backups, health checks. |
| [Security](security.md) | Auth model, sessions/CSRF, the exactly-once guarantee. |
| [Development](development.md) | Build, test, project layout, conventions. |

## Assets

- [`n8n-workflow.json`](n8n-workflow.json) — importable reference workflow.
- [`images/`](images) — UI screenshots used in the docs.
