# kran

`kran` is a small service, similar in spirit to [Watchtower](https://containrrr.dev/watchtower/), that periodically checks running Docker containers, pulls newer images when their tags move, and **recreates** those containers with the same configuration.

It is meant to run **only as a Docker container** with the host daemon socket mounted:

```bash
docker run -d --name kran \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/glaslos/kran:latest
```

See [`docker-compose.example.yaml`](docker-compose.example.yaml) for a Compose-based setup.

## Security

Mounting `docker.sock` grants control over the host’s Docker daemon (and effectively root on the host). Run `kran` only on hosts where that trade-off is acceptable.

## Private registry authentication

Image pulls run inside the `kran` process and use the Docker client’s default config path (`/root/.docker/config.json` in the published image, which runs as root). To pull from a private registry, mount your host `config.json` read-only:

```bash
docker run -d --name kran \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${HOME}/.docker/config.json:/root/.docker/config.json:ro" \
  ghcr.io/glaslos/kran:latest
```

In Compose:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
  - ${HOME}/.docker/config.json:/root/.docker/config.json:ro
```

That file holds credentials: keep the mount read-only and restrict permissions on the host. If your config uses `credsStore` or other helpers that run host binaries, they may not work inside the minimal image; entries under `auths` (base64-encoded `username:token`) are the reliable approach for in-container pulls.

## Configuration

You can pass a **mounted YAML or JSON file** with `-config /path/to/kran.yaml` or `KRAN_CONFIG=/path/to/kran.yaml`. CLI flags and environment variables override values from the file (same names as below, using `snake_case` keys in the file: `docker_host`, `label_enable`, `self_name`, `dry_run`, `cleanup`, `stop_timeout`, `log_json`, `log_level`, `notify_url`, `http_addr`, `webhook_api_key`).

Example:

```yaml
interval: 10m
label_enable: true
self_name: kran
```

| Flag / env | Meaning |
|------------|---------|
| `-config` / `KRAN_CONFIG` | Path to YAML or JSON settings file |
| `-interval` / `KRAN_INTERVAL` | Poll interval (default `5m`) |
| `-docker-host` / `DOCKER_HOST` | Daemon address (default `unix:///var/run/docker.sock`) |
| `-label-enable` / `KRAN_LABEL_ENABLE` | Only update containers with label `kran.enable=true` |
| `-self-name` / `KRAN_SELF_NAME` | Container name to skip (set this to your `kran` container name to avoid self-updates) |
| `-dry-run` / `KRAN_DRY_RUN` | Pull and detect updates but do not recreate |
| `-cleanup` / `KRAN_CLEANUP` | After a successful recreate: remove **anonymous** volumes from the old container (named volumes are unchanged), then prune dangling images |
| `-stop-timeout` / `KRAN_STOP_TIMEOUT` | Grace period before SIGKILL when stopping (default `10s`) |
| `-log-json` / `KRAN_LOG_JSON` | Emit structured JSON logs |
| `-log-level` / `KRAN_LOG_LEVEL` | Minimum log level: `debug`, `info`, `warn`, `error` (default `info`) |
| `-notify-url` / `KRAN_NOTIFY_URL` | Comma-separated [Shoutrrr](https://containrrr.dev/shoutrrr/) URLs for notifications after a recreate |
| `-http-addr` / `KRAN_HTTP_ADDR` | HTTP listen address (e.g. `:9090`) for `/healthz` and Prometheus `/metrics`; empty disables |
| `-webhook-api-key` / `KRAN_WEBHOOK_API_KEY` | Shared secret for **`POST /webhook/update`**; requires `-http-addr` to be set. Empty disables the webhook. Authenticate with header `X-API-Key` or `Authorization: Bearer …` |

Containers with label `kran.ignore=true` are never updated.

## HTTP / metrics

When `-http-addr` (or `KRAN_HTTP_ADDR` / `http_addr` in the config file) is set, kran serves a small HTTP API on that address:

- **`GET /healthz`** — returns `200 OK` (liveness).
- **`GET /metrics`** — Prometheus exposition format.
- **`POST /webhook/update`** — optional; enabled only when `-webhook-api-key` / `KRAN_WEBHOOK_API_KEY` / `webhook_api_key` is set. Triggers the same update pass as a scheduled poll (pull, compare digests, recreate when needed) and resets the poll timer. Returns **`202 Accepted`** on success, **`401 Unauthorized`** if the key is missing or wrong. Send the secret as **`X-API-Key: <key>`** or **`Authorization: Bearer <key>`**.

`/healthz` and `/metrics` are not authenticated. The webhook is protected by the shared key, but there is still **no TLS** on this listener; bind to localhost or terminate TLS at a reverse proxy if the port is reachable from untrusted networks. Treat the webhook key like a password: use a long random value and avoid logging it.

### Exported `kran_*` metrics

| Metric | Type | Description |
|--------|------|-------------|
| `kran_build_info` | gauge (labels `version`, `commit`) | Build metadata |
| `kran_tick_total` | counter | Poll ticks completed |
| `kran_tick_errors_total` | counter | Ticks that failed (e.g. list containers error) |
| `kran_tick_duration_seconds` | histogram | Wall time per tick |
| `kran_last_tick_timestamp_seconds` | gauge | Unix time of last successful tick |
| `kran_containers_scanned` | gauge | Running containers seen on last successful tick |
| `kran_containers_managed` | gauge | Containers eligible for updates after the `Managed` filter |
| `kran_image_pulls_total` | counter (`result`) | Image pulls by outcome |
| `kran_image_pull_duration_seconds` | histogram | Docker pull duration |
| `kran_updates_total` | counter (`result`) | Updates: `success`, `failure`, or `dry_run` |
| `kran_update_duration_seconds` | histogram | Recreate duration (stop through start) |
| `kran_notify_notifications_total` | counter (`result`) | Shoutrrr notify attempts |

Standard Go and process metrics are also registered on the same registry.

## Docker Compose labels

By default (`-label-enable` off), kran considers **every running container** except those labeled `kran.ignore=true` and the container named by `-self-name` (if set).

With **opt-in mode** (`--label-enable` or `KRAN_LABEL_ENABLE=1`), only containers that include `kran.enable=true` are updated. Use this when several stacks share one daemon and you want explicit control.

Compose merges `labels` onto the container; values are strings, so use `"true"` for booleans.

**Enable updates for one service** (when kran runs with `--label-enable`):

```yaml
services:
  app:
    image: my/app:latest
    labels:
      kran.enable: "true"
```

**Never recreate a service** (for example a database or the kran container itself via compose):

```yaml
services:
  db:
    image: postgres:16
    labels:
      kran.ignore: "true"
```

For wiring kran itself (socket mount, `--self-name`, optional `--label-enable`), see [`docker-compose.example.yaml`](docker-compose.example.yaml).

## Linked container groups

When several containers form one application (for example a database and an app), updating **one** image can require restarting **all** of them in a safe order. Use **`kran.link_group`** with the same value on every container that should roll out together, and **`kran.depends_on`** on each **dependent** container listing the **container names** it needs started first (comma-separated, as shown by `docker ps`, with or without a leading `/`).

On each poll, kran pulls every group member’s image. If **any** member’s image digest changed, kran **recreates every managed member** of that group: **stop** in reverse dependency order (dependents before dependencies), then **create and start** in dependency order (dependencies before dependents). If **no** member changed, the group is left untouched.

**Rules:**

- Every container in the stack that should move together must share the same `kran.link_group` value and satisfy the usual `kran.enable` / `kran.ignore` rules.
- Put `kran.depends_on` on the service that **waits on** others (same idea as Watchtower’s `depends-on` label). Only dependencies that are **also in the same link group** affect ordering; other names are ignored with a warning (often a typo or a dependency outside the group).
- A **cycle** in `kran.depends_on` (within the group) causes the whole group update to be **skipped** for that tick with an error log.
- If the group has **more than one** container but **no** usable dependency edges, kran uses a **deterministic name-based** order and logs a warning; prefer explicit `kran.depends_on` for real stacks.

**Example** (Compose; use your real container names in `kran.depends_on`, for example `project_db_1` and `project_app_1`):

```yaml
services:
  db:
    image: postgres:16
    labels:
      kran.enable: "true"
      kran.link_group: "myapp"
  app:
    image: my/app:latest
    labels:
      kran.enable: "true"
      kran.link_group: "myapp"
      kran.depends_on: "db"
```

Linked stacks that use **`network_mode: service:…`** (shared network namespace) are still subject to the same recreate limitation as single containers (see [Limitations](#limitations)).

## GitHub Container Registry

CI publishes **`ghcr.io/glaslos/kran`** on pushes to `main` and on version tags.

For anonymous `docker pull` from a **public** GitHub repo, open **Packages → kran → Package settings** and set visibility to **Public** (GitHub sometimes defaults new packages to private).

## Build locally

```bash
go build -o kran ./app
./kran -h
```

```bash
docker build -t kran:local .
```

## Limitations

- **`NetworkMode=container:…`** (shared network stack) is not supported for recreate (including grouped rollouts).
- Very exotic `docker run` options may not round-trip perfectly through inspect; common Compose-style apps are the target.
