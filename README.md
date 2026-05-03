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

You can pass a **mounted YAML or JSON file** with `-config /path/to/kran.yaml` or `KRAN_CONFIG=/path/to/kran.yaml`. CLI flags and environment variables override values from the file (same names as below, using `snake_case` keys in the file: `docker_host`, `label_enable`, `self_name`, `dry_run`, `cleanup`, `stop_timeout`, `log_json`, `log_level`).

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

Containers with label `kran.ignore=true` are never updated.

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

- **`NetworkMode=container:…`** (shared network stack) is not supported for recreate.
- Very exotic `docker run` options may not round-trip perfectly through inspect; common Compose-style apps are the target.
