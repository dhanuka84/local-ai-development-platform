# Security Policy

## Reporting

Do not open a public issue for a suspected vulnerability or accidental credential disclosure. Contact the repository owner through a private security advisory or the organization's security channel. Include affected versions, reproduction steps, impact, and any suggested mitigation without including real secrets or customer data.

## Supported versions

Until the first stable release, only the current `main` branch is supported. Dependency and container updates are reviewed continuously; deployment owners remain responsible for host, database, model-runtime, and network patching.

## Secrets

Never commit `.env`, Kimi/Moonshot keys, OpenAI credentials, OpenClaw auth stores, database passwords, MCP bearer tokens, private repository credentials, or production data. Rotate a secret immediately if it enters Git history or model context; deleting the visible file is not sufficient.
