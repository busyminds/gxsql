# gxsql Documentation

`gxsql` validates database table data through `database/sql`. It runs SQL in the
database. Each validation run returns one report with results in declaration
order.

## Start here

1. [Validate a table](tutorial/getting-started.md) — install `gxsql`, open a
   database, build a suite, and handle validation outcomes.
2. [Use gxsql in Go tests](tutorial/testing.md) — assert on table quality with
   the `gxsqltest` helpers.

## Learn how validation works

| Topic                                            | Use this page to                                                                   |
| ------------------------------------------------ | ---------------------------------------------------------------------------------- |
| [Validation behavior](concepts/validation.md)    | choose a dialect, define a suite, and understand execution and errors              |
| [Results and remediation](concepts/results.md)   | inspect failures, retain row keys, and understand vacuous passes                   |
| [Operational limits](concepts/operations.md)     | plan query cost, control retained data, and protect sensitive values               |
| [Stable identity and export](concepts/export.md) | join results across runs and export a privacy-preserving JSON data transfer object |

## Look up an API

The [API reference](reference/) is organized by task:

- [Suites, options, SQL integration, and test helpers](reference/suite.md)
- [Expectation builders](reference/expectations.md)
- [Reports, errors, rendering, and limits](reference/results.md)
- [Stable IDs and report export](reference/export.md)
