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

## Configuration

| Flag / env | Meaning |
|------------|---------|
| `-interval` / `KRAN_INTERVAL` | Poll interval (default `5m`) |
| `-docker-host` / `DOCKER_HOST` | Daemon address (default `unix:///var/run/docker.sock`) |
| `-label-enable` / `KRAN_LABEL_ENABLE` | Only update containers with label `kran.enable=true` |
| `-self-name` / `KRAN_SELF_NAME` | Container name to skip (set this to your `kran` container name to avoid self-updates) |
| `-dry-run` / `KRAN_DRY_RUN` | Pull and detect updates but do not recreate |
| `-cleanup` / `KRAN_CLEANUP` | After a successful recreate, prune dangling images |
| `-stop-timeout` / `KRAN_STOP_TIMEOUT` | Grace period before SIGKILL when stopping (default `10s`) |
| `-log-json` / `KRAN_LOG_JSON` | Emit structured JSON logs |

Containers with label `kran.ignore=true` are never updated.

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
