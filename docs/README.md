# gxsql Documentation

`gxsql` validates database table data through `database/sql`. It runs SQL in the
database. Each completed validation returns one report with results in
declaration order.

A returned `error` from `ValidateTable` is a configuration or execution failure.
Use `report.Err()` to gate on data-quality (policy) failures after a completed
run. You own the `database/sql` driver and must select the dialect explicitly.

## Start Here

1. [Validate a Table](tutorial/getting-started.md) — install `gxsql`, open a
   database, build a suite, and handle validation outcomes.
2. [Use gxsql in Go Tests](tutorial/testing.md) — assert on table quality with
   the `gxsqltest` helpers.

## Learn How Validation Works

| Topic                                            | Use this page to                                                                   |
| ------------------------------------------------ | ---------------------------------------------------------------------------------- |
| [Validation behavior](concepts/validation.md)    | choose a dialect, define a suite, and understand execution and errors              |
| [Results and remediation](concepts/results.md)   | inspect failures, retain row keys, and understand vacuous passes                   |
| [Operational limits](concepts/operations.md)     | plan query cost, control retained data, and protect sensitive values               |
| [Stable identity and export](concepts/export.md) | join results across runs and export a privacy-preserving JSON data transfer object |
| [Compatibility](concepts/compatibility.md)       | select a built-in dialect and check the supported database matrix                  |

For the opt-in `WithSharedScalarEvaluation()` performance option, see
[operational limits](concepts/operations.md#use-shared-scalar-evaluation).

## Look Up an API

The [API reference](reference/) is organized by task:

- [Suites, options, SQL integration, and test helpers](reference/suite.md)
- [Expectation builders](reference/expectations.md)
- [Reports, errors, rendering, and limits](reference/results.md)
- [Stable IDs and report export](reference/export.md)
