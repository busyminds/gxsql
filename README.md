# gxsql

`gxsql` is a SQL-native data-quality assertion framework for Go. It renders
expectations as SQL and validates tables through `database/sql`, without loading
whole tables into application memory. A completed validation collects results in
declaration order.

## Install

```bash
go get github.com/busyminds/gxsql
```

`gxsql` requires Go 1.24 or newer. The core package is driver-neutral: open the
database with your `database/sql` driver and select the matching
`gxsql.WithDialect(...)`.

## Quick Start

```go
package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/busyminds/gxsql"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", "postgres://localhost/mydb?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	suite := gxsql.NewSuite(
		gxsql.RowCount().GreaterOrEqual(1),
		gxsql.Int("age").Between(0, 120),
		gxsql.String("email").NotEmpty(),
		gxsql.Column("id").Unique(),
	)

	report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
		gxsql.WithDialect(gxsql.Postgres()),
		gxsql.WithKey("id"),
	)
	if err != nil {
		log.Fatalf("gxsql execution error: %v", err)
	}
	if err := report.Err(); err != nil {
		log.Fatalf("data-quality check failed: %v", err)
	}
}
```

`ValidateTable` returns an error for configuration or execution failures. Policy
failures are recorded in the completed report; use `report.Err()` to gate on
error-severity data-quality failures.

## Documentation

- [Getting started](docs/tutorial/getting-started.md)
- [Core concepts](docs/concepts/)
- [API reference](docs/reference/)
- [Compatibility and supported databases](docs/concepts/compatibility.md)
