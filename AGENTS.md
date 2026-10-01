# Working on service-api-standard

Decisions with no single declaration to sit beside. Per-declaration detail is a comment beside the code; the [README](README.md) orients an operator.

A route is added in the contract first, then given a store method here.

## Storage: whole records, constraint columns

Each signed record is stored whole as protojson (`go/contract.MarshalOptions`) in a `jsonb` column; only the fields a constraint needs are lifted into real columns. Two reasons:

- **Reads return the record exactly as written** (api's `go/store`, "Verification is inherited"), down to the nanosecond. `timestamptz` keeps microseconds; the protojson string keeps all nine digits, and the conformance suite checks it.
- **A field added to the contract needs no migration.** Only a new table, index or constraint does.

Emails are compared byte-exact: folding case here would make this backend disagree with `service-api-chain`.

## Migrations

Forward-only: a released migration is never edited; fix forward with a new one. Hand-rolled rather than goose: churn is low because records are stored whole, and the runner is a few dozen lines. Revisit when a migration must run outside a transaction (`CREATE INDEX CONCURRENTLY`).

## Testing against Postgres

The answer to `dev-testing-go`'s open isolation question, for this repository:

- **A template database**: migrations run once into it, and every database a test touches is cloned from it. `pgtest` owns the container; pick `Shared` or `Fresh` by its doc.
- **Not a transaction per test**: every store method commits its own transaction, so a test cannot wrap it. **Not a schema per test**: it couples the code under test to `search_path` and re-runs migrations each time.

For an assistant writing a new store test: prove a promise of api's `go/store` by adding it to `storetest` in `metacensus/api`, not here. Test here only what is Postgres's — a constraint, a SQLSTATE mapping, a migration — and pick `Shared` unless the assertion reads state the test did not write.

## Not yet

- **Sessions** are `auth.MemorySessions`: a restart logs everyone out and the service runs as one replica. A Postgres `auth.Sessions`, proven by api's `sessiontest`, is next.
- **CORS** is the operator's (see `metacensus/api` README, "Open questions").
