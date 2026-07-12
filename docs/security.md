# Security model

[← Docs index](README.md)

Greenlight sits between your automations and real actions, so it's built to be
safe to expose to the internet behind HTTPS.

## Two authentication surfaces

![Authentication surfaces](images/diagrams/security-auth.png)

- **API** — every `/api/*` call needs a valid `X-API-Key`. Keys are stored only as
  **SHA-256 hashes**; the plaintext is shown exactly once at creation.
- **UI** — a single admin password establishes a session. Login is
  **rate-limited** with a lockout after repeated failures.

## Sessions & CSRF

- Session and CSRF cookies are **HMAC-signed** with `GREENLIGHT_SESSION_SECRET`;
  tampered or foreign-signed cookies are rejected.
- Every state-changing form (login, decide, settings, key/rule management) carries
  a CSRF token that is validated server-side.
- Cookies are `HttpOnly`, `SameSite=Lax`, and marked `Secure` automatically when
  `GREENLIGHT_PUBLIC_URL` is `https://` (override with `GREENLIGHT_COOKIE_SECURE`).
- Reaching the app directly over plain `http://<ip>:PORT` while `PUBLIC_URL` is
  `https://` makes the browser drop the `Secure` cookies, so forms fail with a
  "CSRF token" 403. The fix is `GREENLIGHT_COOKIE_SECURE=false` on a trusted LAN —
  **not** disabling CSRF, which stays enforced on every mutating route.

## Notifications are not a trust channel

Deep links in notifications point at the **login-protected** request page. No auth
token and no approve/reject action link is ever placed in a notification body, so a
leaked, cached, or shoulder-surfed push **cannot** approve anything — a human still
has to log in and decide. See [Notifications](notifications.md#security).

## Decisions resolve exactly once

Approvals, rejections, cancellations, and timeout defaults all pass through a
transactional compare-and-set that only fires **if the request is still pending**.
A user click racing the timeout engine can't double-resolve or send two callbacks.

## Hardening checklist

- [ ] Serve behind HTTPS (`GREENLIGHT_PUBLIC_URL` = `https://…`).
- [ ] Use a long, random `GREENLIGHT_SESSION_SECRET` (`openssl rand -hex 32`).
- [ ] Use a strong `GREENLIGHT_ADMIN_PASSWORD`.
- [ ] Give each caller its own API key; revoke unused ones under Settings.
- [ ] Lock down your ntfy topic (token/basic-auth, not world-readable).
- [ ] Restrict who can reach the host/tunnel if you don't need it public.

## Reporting issues

This is a small self-hosted project. If you find a security problem, open an issue
describing the impact (avoid posting a working exploit against a live instance).
