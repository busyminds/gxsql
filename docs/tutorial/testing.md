# Use gxsql in Go Tests

Use the `gxsqltest` subpackage to turn a suite result into a Go test assertion.
The helpers call `Suite.ValidateTable` with the same options. You own the
`database/sql` driver. Pass `WithDialect` for the engine behind `db`.

```go
import (
    "context"
    "testing"

    "github.com/busyminds/gxsql"
    "github.com/busyminds/gxsql/gxsqltest"
)

func TestUsers(t *testing.T) {
    ctx := context.Background()
    // db and suite are set up by the test.

    gxsqltest.Require(t, ctx, suite, db, gxsql.Table("users"),
        gxsql.WithDialect(gxsql.SQLite()),
    )
}
```

## Choose an Assertion Helper

| Helper              | Failure behavior                                                                             | Return value                                      |
| ------------------- | -------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `gxsqltest.Check`   | Calls `t.Errorf` for an execution/config error or a hard-gating policy failure and continues | `true` when validation has no hard-gating failure |
| `gxsqltest.Require` | Calls `t.Fatalf` for an execution/config error or a hard-gating policy failure and stops     | None                                              |

Both helpers report the two `ValidateTable` outcomes:

- a non-nil returned `error` as a configuration or execution failure
- a completed report with hard-gating policy failures; warning and info failures
  remain queryable without failing the helper

Use `Check` when later assertions remain useful after a quality gate fails. Use
`Require` when a failed gate makes the rest of the test meaningless.

Pass the same retention and scope options you use in application code
(`WithKey`, `SummaryOnly`, `WithScope`, and related caps). See the
[test helpers reference](../reference/suite.md#test-helpers).

## Next

- [Validate a Table](getting-started.md)
- [Test helpers reference](../reference/suite.md#test-helpers)
