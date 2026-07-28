package gxsql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrorCategory is a closed vocabulary of machine-facing failure classes.
type ErrorCategory string

const (
	// CategoryInvalidConfig marks expectation or run configuration rejected
	// before or during validation setup.
	CategoryInvalidConfig ErrorCategory = "invalid_config"
	// CategoryUnsupported marks a requested capability the library does not provide.
	CategoryUnsupported ErrorCategory = "unsupported"
	// CategoryRendering marks SQL identifier or fragment rendering failures.
	CategoryRendering ErrorCategory = "rendering"
	// CategoryDatabase marks database/sql execution failures unrelated to scanning.
	CategoryDatabase ErrorCategory = "database"
	// CategoryScan marks row iteration or column scan failures.
	CategoryScan ErrorCategory = "scan"
	// CategoryContext marks context cancellation or deadline exceeded.
	CategoryContext ErrorCategory = "context"
	// CategoryObserver marks export redaction or normalization failures.
	CategoryObserver ErrorCategory = "observer"
)

// ErrCategory* values are category markers for errors.Is against a typed
// ErrorCategory. For example, errors.Is(err, ErrCategoryInvalidConfig) reports
// whether err is or wraps a CategorizedError with that category.
var (
	ErrCategoryInvalidConfig = &categoryMarker{CategoryInvalidConfig}
	ErrCategoryUnsupported   = &categoryMarker{CategoryUnsupported}
	ErrCategoryRendering     = &categoryMarker{CategoryRendering}
	ErrCategoryDatabase      = &categoryMarker{CategoryDatabase}
	ErrCategoryScan          = &categoryMarker{CategoryScan}
	ErrCategoryContext       = &categoryMarker{CategoryContext}
	ErrCategoryObserver      = &categoryMarker{CategoryObserver}
)

type categoryMarker struct {
	ErrorCategory
}

func (m *categoryMarker) Error() string { return string(m.ErrorCategory) }

// CategorizedError attaches a stable category to an underlying failure.
type CategorizedError struct {
	// Category is the machine-facing failure class.
	Category ErrorCategory
	// Err is the underlying cause exposed through Unwrap.
	Err error
}

// Error returns a diagnostic message including the category and wrapped error.
func (e *CategorizedError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("gxsql: %s", e.Category)
	}
	return fmt.Sprintf("gxsql: %s: %v", e.Category, e.Err)
}

// Unwrap returns the underlying error for errors.Is and errors.As traversal.
func (e *CategorizedError) Unwrap() error { return e.Err }

// Is reports whether target matches the category marker or unwrap chain.
// Category markers match by ErrorCategory; other targets delegate to Err.
func (e *CategorizedError) Is(target error) bool {
	if target == nil {
		return e.Err == nil
	}
	if m, ok := target.(*categoryMarker); ok {
		return e.Category == m.ErrorCategory
	}
	if e.Err != nil {
		return errors.Is(e.Err, target)
	}
	return false
}

// PreflightErrors collects every configuration issue found before SQL starts.
// Returned by ValidateTable when ContinueOnError is not set. Use errors.As to
// inspect Issues; errors.Is matches ErrCategoryInvalidConfig on each issue.
type PreflightErrors struct {
	// Issues lists every configuration failure in declaration order.
	Issues []PreflightIssue
}

// PreflightIssue records one expectation configuration failure.
type PreflightIssue struct {
	// Index is the expectation position in the suite.
	Index int
	// ID is the caller-supplied expectation identifier when present.
	ID string
	// Err is the categorized configuration error for this slot.
	Err error
}

// Error summarizes the collected configuration failures.
func (e *PreflightErrors) Error() string {
	parts := make([]string, len(e.Issues))
	for i, iss := range e.Issues {
		parts[i] = iss.Err.Error()
	}
	return fmt.Sprintf("gxsql: %d configuration error(s): %s", len(e.Issues), strings.Join(parts, "; "))
}

// Unwrap returns each issue error for multi-error inspection.
func (e *PreflightErrors) Unwrap() []error {
	out := make([]error, len(e.Issues))
	for i, iss := range e.Issues {
		out[i] = iss.Err
	}
	return out
}

func newConfigError(err error) error {
	if err == nil {
		return nil
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return err
	}
	return &CategorizedError{Category: CategoryInvalidConfig, Err: err}
}

func categorizeExecutionError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return err
	}
	if ctx.Err() != nil {
		return &CategorizedError{Category: CategoryContext, Err: fmt.Errorf("%w: %v", ctx.Err(), err)}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &CategorizedError{Category: CategoryContext, Err: err}
	}
	return &CategorizedError{Category: CategoryDatabase, Err: err}
}

func categorizeScanError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return err
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &CategorizedError{Category: CategoryContext, Err: err}
	}
	return &CategorizedError{Category: CategoryScan, Err: err}
}

func categorizeRenderError(err error) error {
	if err == nil {
		return nil
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return err
	}
	return &CategorizedError{Category: CategoryRendering, Err: err}
}

var (
	errScopeIdentityRequired       = errors.New("scope identity is required")
	errScopePredicateRequired      = errors.New("scope predicate is required")
	errScopeValuesWithoutPredicate = errors.New("scope values require a predicate")

	errTrustedCountTargetMarkerRequired         = errors.New("trusted count template requires exactly one {{target}} marker")
	errTrustedCountScopeMarkerRequired          = errors.New("trusted count template requires exactly one {{scope}} marker")
	errTrustedCountDuplicateTargetMarker        = errors.New("trusted count template has duplicate {{target}} marker")
	errTrustedCountDuplicateScopeMarker         = errors.New("trusted count template has duplicate {{scope}} marker")
	errTrustedCountMalformedMarker              = errors.New("trusted count template has malformed marker")
	errTrustedCountUnsupportedMarker            = errors.New("trusted count template has unsupported marker")
	errTrustedCountCustomPlaceholderBeforeScope = errors.New("trusted count template has custom placeholder before {{scope}}")

	errCustomCountQueryFailed      = errors.New("custom count query failed")
	errCustomCountContextFailed    = errors.New("custom count context canceled")
	errCustomCountNoRows           = errors.New("custom count query returned no rows")
	errCustomCountMultipleRows     = errors.New("custom count query returned multiple rows")
	errCustomCountWrongColumnCount = errors.New("custom count query must return exactly one column")
	errCustomCountNull             = errors.New("custom count query returned NULL")
	errCustomCountNonInteger       = errors.New("custom count query returned a non-integer value")
	errCustomCountNegative         = errors.New("custom count query returned a negative value")
	errCustomCountOverflow         = errors.New("custom count query returned a value that does not fit int")

	errCustomCountResultKind           = errors.New("custom count result kind must be custom")
	errCustomCountResultColumn         = errors.New("custom count result column must be blank")
	errCustomCountResultDenominator    = errors.New("custom count result row denominator must be unavailable")
	errCustomCountResultTotal          = errors.New("custom count result total must be zero")
	errCustomCountResultFailedPercent  = errors.New("custom count result failed percent must be zero")
	errCustomCountResultSamples        = errors.New("custom count result samples must be empty")
	errCustomCountResultFailedKeys     = errors.New("custom count result failed keys must be empty")
	errCustomCountResultFailedNegative = errors.New("custom count failed count must be non-negative")
)

func customCountResultContractError(err error) error {
	return &CategorizedError{Category: CategoryInvalidConfig, Err: err}
}

func customCountScanError(err error) error {
	if err == nil {
		return nil
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		switch ce.Category {
		case CategoryContext:
			return &CategorizedError{Category: CategoryContext, Err: ce.Err}
		case CategoryScan:
			return &CategorizedError{Category: CategoryScan, Err: privacyScanSentinel(ce.Err)}
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &CategorizedError{Category: CategoryContext, Err: err}
	}
	return &CategorizedError{Category: CategoryScan, Err: privacyScanSentinel(err)}
}

func privacyScanSentinel(err error) error {
	switch {
	case errors.Is(err, errCustomCountNoRows), errors.Is(err, sql.ErrNoRows):
		return errCustomCountNoRows
	case errors.Is(err, errCustomCountMultipleRows):
		return errCustomCountMultipleRows
	case errors.Is(err, errCustomCountWrongColumnCount):
		return errCustomCountWrongColumnCount
	case errors.Is(err, errCustomCountNull):
		return errCustomCountNull
	case errors.Is(err, errCustomCountNonInteger):
		return errCustomCountNonInteger
	case errors.Is(err, errCustomCountNegative):
		return errCustomCountNegative
	case errors.Is(err, errCustomCountOverflow):
		return errCustomCountOverflow
	default:
		return errCustomCountNonInteger
	}
}

func customCountPrivacyError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &CategorizedError{Category: CategoryContext, Err: errCustomCountContextFailed}
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		switch ce.Category {
		case CategoryContext:
			return &CategorizedError{Category: CategoryContext, Err: errCustomCountContextFailed}
		case CategoryDatabase:
			return &CategorizedError{Category: CategoryDatabase, Err: errCustomCountQueryFailed}
		case CategoryScan:
			return customCountScanError(ce.Err)
		case CategoryRendering, CategoryInvalidConfig:
			return err
		default:
			return &CategorizedError{Category: ce.Category, Err: errCustomCountQueryFailed}
		}
	}
	return &CategorizedError{Category: CategoryDatabase, Err: errCustomCountQueryFailed}
}

func scopeArityError(slots, values int) error {
	return fmt.Errorf("scope predicate has %d placeholders but %d values", slots, values)
}

func trustedCountArityError(slots, values int) error {
	return fmt.Errorf("trusted count template has %d placeholders but %d values", slots, values)
}

func unsupportedScopePredicateError(msg string) error {
	return &CategorizedError{Category: CategoryUnsupported, Err: fmt.Errorf("gxsql: %s", msg)}
}
