# Use gxsql in Go Tests

The `gxsqltest` subpackage turns a suite result into a Go test assertion. It
accepts the same options as `Suite.ValidateTable`, so test SQL is rendered for
the correct database engine.

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

## Choose an assertion helper

| Helper              | Failure behavior                             | Return value                                                     |
| ------------------- | -------------------------------------------- | ---------------------------------------------------------------- |
| `gxsqltest.Check`   | Calls `t.Errorf` and lets the test continue. | `true` only if validation executed and every expectation passed. |
| `gxsqltest.Require` | Calls `t.Fatalf` and stops the test.         | None.                                                            |

Both helpers report execution errors and failed expectations. Use `Check` when
later assertions remain useful after a validation failure; use `Require` when a
failed quality gate makes the rest of the test meaningless.

## Next

- [Validate a table](getting-started.md)
- [Test helpers reference](../reference/suite.md#test-helpers)
