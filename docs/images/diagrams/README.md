# Diagram sources

The docs embed rendered PNGs; each one is generated from the Mermaid `.mmd`
source next to it (same basename). Edit the `.mmd`, then re-render.

## Regenerate

Requires [`@mermaid-js/mermaid-cli`](https://github.com/mermaid-js/mermaid-cli)
and a Chromium/Chrome binary.

```bash
# from the repo root
npm install -g @mermaid-js/mermaid-cli   # or use npx below

for f in docs/images/diagrams/*.mmd; do
  mmdc -i "$f" -o "${f%.mmd}.png" -t dark -b "#0f1115" -s 3
done
```

If Chromium is sandboxed (e.g. a snap package) and can't read the default temp
paths, pass a puppeteer config that pins `executablePath` and a `userDataDir`
inside your home directory, and add `--no-sandbox`.

| Source | Used in |
|---|---|
| `approval-loop` | [README](../../../README.md) |
| `api-auth` | [api.md](../../api.md) |
| `architecture-components`, `architecture-sequence`, `request-lifecycle`, `timeout-engine` | [architecture.md](../../architecture.md) |
| `rule-precedence` | [configuration.md](../../configuration.md) |
| `deployment-topology` | [deployment.md](../../deployment.md) |
| `n8n-pattern` | [n8n.md](../../n8n.md) |
| `notification-flow` | [notifications.md](../../notifications.md) |
| `security-auth` | [security.md](../../security.md) |
