package gxsql

import (
	"context"
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
