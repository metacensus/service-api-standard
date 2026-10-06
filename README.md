# service-api-standard

The MetaCensus API over Postgres. It implements [`metacensus/api`](https://github.com/metacensus/api)'s persistence port, `store.Store`, and serves it through that module's shared server, `go/service`. Its sibling, `service-api-chain`, serves the same contract over Hyperledger Fabric.

`go/service` owns the handlers, the auth flow and the status codes; this repository owns persistence, configuration, migrations, the process that serves the handlers, and the image. Design decisions are in [AGENTS.md](AGENTS.md).

## Endpoints

- **`/metacensus/api/v1/…`** — the contract's authenticated surface; its route manifest is `go/server/routes` in `metacensus/api`.
- **`GET /healthz`** — liveness for the container runtime, outside the prefix; it does not touch Postgres.

## Configuration

| Variable | Default | |
|---|---|---|
| `DATABASE_URL` | — | Required. A Postgres connection string; it carries the password. |
| `PORT` | `3001` | |

The service refuses to start on invalid configuration, an unreachable database or a failed migration. Migrations run at boot.

## Local development

```bash
docker run -d --name mc-postgres -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:18-alpine
```

```bash
DATABASE_URL=postgres://postgres:dev@localhost:5432/postgres make run
```

`make help` lists every target; `make check` runs what CI runs.

## Releasing

`make release-patch` / `release-minor` / `release-major` tag and push; `release.yml` re-runs CI at the tag and publishes `docker.io/metacensus/service-api-standard` for amd64 and arm64. It needs the `DOCKERHUB_TOKEN` secret, a Docker Hub organization access token with write access to that repository.

## License

AGPL-3.0-only ([LICENSE](LICENSE)).
