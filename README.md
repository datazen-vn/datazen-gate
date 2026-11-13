<!-- dz-repo-type: service -->

# datazen-gate

Standalone paywall gate in Go that sits in front of an existing app and checks DataZen subscription entitlements.

**Status:** experimental · **Owner:** datazen-vn admins · **License:** MIT

Under construction (`0.x`, strict semantic versioning: it runs on other people's machines). `dz-gate` stands in front of a developer's app, identifies the customer, asks the hub what the customer may use, and lets the request through or sends the customer to the checkout page. The app needs no code change.

## Status

Experimental and private. It goes public only after the slice-2 E2E passes: the gate in front of an app without the SDK, on the platform `deploy/saas-e2e` stack.

Not yet available: `forward-auth` mode, an option to turn off path logging.

## Quick start

Requirements: Docker. Go (see `go.mod`) to build from source.

```bash
cp deploy/compose.example.yml compose.yml
cp deploy/gate.example.yaml gate.yaml          # set upstream, identity, routes, paywall
printf 'DZ_API_KEY=dz_test_<key>\n' > .env && chmod 0600 .env
docker build -f deploy/Dockerfile --build-arg PHIEN_BAN=0.1.0 -t dz-gate:0.1.0 .
docker run --rm --env-file .env -v "$PWD/gate.yaml:/c.yaml:ro" dz-gate:0.1.0 --check /c.yaml
docker compose up -d
```

File permissions, the mandatory deployment rules and the compose details: [docs/trien-khai.md](docs/trien-khai.md).

## Configuration

One YAML file (`gate.yaml`; unknown fields are errors) plus environment variables for secrets. The file reference: [docs/cau-hinh.md](docs/cau-hinh.md); an annotated sample: [deploy/gate.example.yaml](deploy/gate.example.yaml).

| Setting                       | Required                        | Default          | Description                                                                                                                 |
| ----------------------------- | ------------------------------- | ---------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `DZ_API_KEY` (environment)    | yes                             |                  | Application key of the hub (`dz_test_…` for tests, `dz_live_…` for real money); never put it in the YAML, the image or logs |
| `DZ_JWT_SECRET` (environment) | if `identity` uses `secret_env` |                  | Secret for the JWT identity resolver                                                                                        |
| `gate.listen`                 | yes                             |                  | Public listen address                                                                                                       |
| `gate.upstream`               | yes                             |                  | URL of the app behind the gate; must not point at the gate itself                                                           |
| `routes`                      | yes                             |                  | One to 128 rules, first match wins: `allow`, `require` or `soft`                                                            |
| `ops.listen`                  | no                              | `127.0.0.1:9090` | Operations port (health, readiness, metrics); never expose it to the Internet                                               |

## Architecture

A reverse proxy. A request is either bypassed (`/api/dz/*`, `/_dz/*`, the login page, the success path) or matched against `routes`, then identified (JWT with JWKS, or a session hook), then checked for entitlement. A browser navigation without entitlement gets a 302 to the checkout page; an API call gets a 402 JSON. If the customer cannot be identified the gate answers 503 and never lets an anonymous request through. Behaviour in full: [docs/hanh-vi-va-trien-khai-an-toan.md](docs/hanh-vi-va-trien-khai-an-toan.md). Code layout and boundaries: [AGENTS.md](AGENTS.md).

## API surface

| Path                                          | Purpose                                                                                            |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Everything else                               | Proxied to the upstream after the paywall decision, with `X-DZ-*` headers injected by the gate     |
| `/_dz/healthz`, `/_dz/readyz`, `/_dz/metrics` | Health, readiness and metrics on the operations port only; on the public port `/_dz/*` answers 404 |

Responses, error codes and metrics: [docs/ma-loi.md](docs/ma-loi.md).

## Verify

```bash
bash cong/kiem-kho.sh          # gofmt, vet, build, tests with -race (no skips)
bash cong/kiem-conformance.sh  # the 63 gate-v1 cases of the contracts repository, pinned by SHA
bash cong/kiem-docker.sh       # builds the real image, runs it and sends requests through the gate (needs Docker)
bash cong/kiem-govulncheck.sh
```

## Deploy & rollback

Runbook: [docs/trien-khai.md](docs/trien-khai.md). Deploy as a container image; roll back by running the previous image tag. Check the configuration of the new image with `--check` before switching.

## Contracts & docs

- Contracts: the `gate-v1` conformance suite lives in the internal contracts repository `datazen-vn/datazen-hub-meta`; `cong/kiem-conformance.sh` needs a checkout of it next to this one (`../meta` or `DZ_META_DIR`) and is run by the maintainers on every change
- Docs: [configuration](docs/cau-hinh.md), [deployment](docs/trien-khai.md), [responses and metrics](docs/ma-loi.md), [changelog](CHANGELOG.md)

## License

MIT, see [LICENSE](LICENSE).

Last updated: 2026-11-11
