# Compatibility

`gxsql` is driver-neutral. Open a `*sql.DB` with the driver for your database,
then pass the dialect that renders SQL for that engine. The core module does not
import or bundle database drivers.

```go
report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
)
```

`ValidateTable` defaults to `Postgres()` when no dialect is supplied. Pass the
dialect explicitly in application code and tests so rendered SQL remains coupled
to the selected driver.

## Supported Matrix

A **supported** engine is covered by release documentation and CI conformance.
The version below is the active CI baseline, not a restriction on compatible
newer patch releases.

| Area         | Support   | Active coverage                                              | Notes                                                         |
| ------------ | --------- | ------------------------------------------------------------ | ------------------------------------------------------------- |
| Go toolchain | Supported | Go 1.25.x and 1.27.x                                         | Go 1.25 or newer is required.                                 |
| Ubuntu       | Supported | `ubuntu-24.04`                                               | CI target.                                                    |
| PostgreSQL   | Supported | PostgreSQL 16 via `github.com/jackc/pgx/v5/stdlib`           | Use `gxsql.Postgres()`.                                       |
| SQLite       | Supported | SQLite 3.50.4 via `modernc.org/sqlite` v1.39.1               | Use `gxsql.SQLite()`.                                         |
| DuckDB       | Supported | DuckDB 1.5.4 via `github.com/duckdb/duckdb-go/v2` v2.10504.0 | Use `gxsql.DuckDB()`.                                         |
| MySQL        | Supported | MySQL 8.4 via `github.com/go-sql-driver/mysql` v1.10.0       | Use `gxsql.MySQL()`. MariaDB is outside the supported matrix. |

The listed drivers are integration and conformance dependencies, not runtime
dependencies of the core package.

## Built-In Dialects

| Dialect      | Identifier quoting | Placeholders  | String length       |
| ------------ | ------------------ | ------------- | ------------------- |
| `Postgres()` | `"name"`           | `$1`, `$2`, … | `CHAR_LENGTH(expr)` |
| `SQLite()`   | `"name"`           | `?`           | `LENGTH(expr)`      |
| `DuckDB()`   | `"name"`           | `$1`, `$2`, … | `LENGTH(expr)`      |
| `MySQL()`    | `` `name` ``       | `?`           | `CHAR_LENGTH(expr)` |

Built-in dialects accept identifiers that match `^[A-Za-z_][A-Za-z0-9_]*$`.
Unsupported engines and driver combinations are not part of the published
conformance matrix.

## Conformance Scope

The shared CI kit exercises all supported engines for identifier qualification,
bound placeholders, null and text/byte scans, single and composite keys,
diagnostic caps, empty targets, cancellation, database and scan errors,
`ContinueOnError()`, and transaction-compatible `gxsql.DB` handles. Exact SQL
shape and deterministic failure paths use the unit-test fake driver.
