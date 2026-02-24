# `cf`: cloudflare command line

Register domain names through cloudflare on the command line like vercel does.

> [!warning]
> Cloudflare does not support registering domains through its API (except on enterprise plan). So this tool has a wizard automates what it can and takes you to cloudflare dashboard for parts it can't (e.g. domain registration).

## Install

Download a binary for your OS/arch from:

- https://github.com/rufuspollock/cf/releases/

Then make it executable and put it on your `PATH` (macOS/Linux example):

```bash
chmod +x cf-darwin-arm64
mv cf-darwin-arm64 /usr/local/bin/cf
cf help
```

## Quick Start

Try it out:

```bash
cf help
cf wizard
cf registrar list
cf zones list
```

Goal is “Vercel-like DX”, implemented here as a Go CLI:

Add commands like:

```
cf domains search example.com
cf domains register example.com
cf domains dns add example.com A 1.2.3.4
```

This aligns well with our stack like e.g. FlowerShow, DataHub etc.

## Current CLI

The repo includes a Go CLI (`cmd/cf/main.go`) that supports:

- interactive guided flow to add a domain
- listing Cloudflare Registrar domains
- listing zones in the account
- adding a zone by domain name
- creating DNS records

### Build

```bash
go build -o cf ./cmd/cf
```

Auth fallback behavior:

- `CF_API_TOKEN` or `CLOUDFLARE_API_TOKEN` is accepted.
- `CF_ACCOUNT_ID` or `CLOUDFLARE_ACCOUNT_ID` is accepted.
- If no token env var is set, CLI tries `wrangler auth token --json`.
- If no account env var is set, CLI tries to infer account from `/memberships`:
  - works automatically when token belongs to one account
  - if multiple accounts are available, set `CF_ACCOUNT_ID` explicitly

### Commands

```bash
./cf help
./cf wizard
./cf registrar list
./cf zones list
./cf zones add example.com
./cf dns add --zone example.com --type A --name @ --content 1.2.3.4 --ttl 1 --proxied false
```

The wizard can open the Cloudflare dashboard URL for manual registration steps, then continue with zone + DNS setup.

## Research

- Cloudflare Registrar does not currently expose a public API endpoint to purchase/register a new domain.
- Cloudflare's current Registrar API supports listing domains and updating existing registered domains only.
- The Registrar update operation supports management fields like `auto_renew`, `locked`, and `privacy`.
- A practical CLI fallback is:
  - use API to add an already-registered domain as a Cloudflare zone, and
  - use Registrar API to manage settings for domains already in Cloudflare Registrar.
- Result: a Vercel-like CLI UX is still possible for domain onboarding and management, but domain purchase must still be done in the Cloudflare Dashboard.

Sources:
- https://developers.cloudflare.com/registrar/get-started/register-domain/
- https://developers.cloudflare.com/api/node/resources/registrar/subresources/domains/methods/update/
- https://raw.githubusercontent.com/cloudflare/api-schemas/main/openapi.json

## Inbox

- [ ] Add cross-platform release automation (darwin/linux/windows binaries via GitHub Actions).
