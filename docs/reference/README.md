# API Reference

This reference groups the public `github.com/busyminds/gxsql` API by the job it
performs. For complete Go doc comments and signatures, use `go doc` or
pkg.go.dev alongside these guides.

| Page                                                 | Covers                                                                                                 |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| [Suites, options, and SQL integration](suite.md)     | `Suite`, `Option`, `ValidateTable`, `gxsqltest`, `DB`, `Dialect`, `TableRef`, and sealed `Expectation` |
| [Expectation builders](expectations.md)              | row-count, generic-column, numeric, and string policies                                                |
| [Reports, errors, rendering, and limits](results.md) | `Report`, `Result`, facts, error taxonomy, display text, and caps                                      |
| [Stable IDs and report export](export.md)            | `WithID`, `ExpectationKind`, `ExportReport`, export DTOs, verdicts, and normalized values              |

## Related guides

- [Validate a table](../tutorial/getting-started.md)
- [Validation behavior](../concepts/validation.md)
- [Results and remediation](../concepts/results.md)
- [Operational limits and privacy](../concepts/operations.md)
