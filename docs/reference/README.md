# API Reference

This reference groups the public `github.com/busyminds/gxsql` API by task. Use
`go doc` or pkg.go.dev with these guides for full Go doc comments and
signatures.

| Page                                                 | Covers                                                                                                                                                                                                                                                |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [Suites, Options, and SQL Integration](suite.md)     | `Suite`, `Option`, `ValidateTable`, scope vs parent/secondary filter isolation, `gxsqltest`, `DB`, `Dialect`, `TableRef`, and sealed `Expectation`                                                                                                    |
| [Expectation Builders](expectations.md)              | Row-count, structural columns, column, composite unique/reference with parent filters, reconcile counts, numeric aggregates including sum/stddev, completeness/duplicate/frequency metrics, string, timestamp, custom-count, and `WithMaxFailedCount` |
| [Reports, Errors, Rendering, and Limits](results.md) | `Report`, `Result`, key/reference/reconcile/temporal/structural/broader-metric facts, custom-count and reconcile semantics, errors, display text, and caps                                                                                            |
| [Stable IDs and Report Export](export.md)            | `WithID`, `ExpectationKind`, `ExportReport`, privacy-safe reference/reconcile/broader-metric facts, export DTOs, verdicts, and normalized values                                                                                                      |

## Related Guides

- [Validate a table](../tutorial/getting-started.md)
- [Validation behavior](../concepts/validation.md)
- [Results and remediation](../concepts/results.md)
- [Operational limits and privacy](../concepts/operations.md)
- [Compatibility and support](../concepts/compatibility.md)
