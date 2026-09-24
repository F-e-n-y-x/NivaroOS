package engine

import (
	"context"
	"errors"
	"fmt"
)

// ErrorCode is a stable, machine-readable failure reason. The UI never
// parses English; it explains a code with backup.err.<code>.{title,cause,fix}.
//
// The complete enum - these engine codes plus the job-side ones, each with
// its retry class and UI actions - is jobs/errors.go (jobs.ErrorCode is an
// alias of this type). The codes below are the ones the engine itself may
// return; jobs' tests fail if one of them has no class there.
type ErrorCode string

// Codes the engine returns (spec §5 table and §6).
const (
	// transient
	CodeEngineUnavailable  ErrorCode = "engine_unavailable" // still starting, shutting down
	CodeNetworkUnreachable ErrorCode = "network_unreachable"
	CodeCloudRateLimited   ErrorCode = "cloud_rate_limited"
	CodeDestOffline        ErrorCode = "dest_offline"
	CodeSourceOffline      ErrorCode = "source_offline"
	CodeIOError            ErrorCode = "io_error"
	// deferred
	CodeCloudQuotaDaily ErrorCode = "cloud_quota_daily"
	// guard
	CodeDeleteGuard        ErrorCode = "delete_guard"
	CodeChangeGuard        ErrorCode = "change_guard"
	CodeEmptySource        ErrorCode = "empty_source"
	CodeDestMarkerMismatch ErrorCode = "dest_marker_mismatch"
	// config
	CodeDestInsideSource  ErrorCode = "dest_inside_source"
	CodePathNotAllowed    ErrorCode = "path_not_allowed"
	CodeFat32FileTooLarge ErrorCode = "fat32_file_too_large"
	CodeCaseCollision     ErrorCode = "case_collision"
	CodeEndpointUnknown   ErrorCode = "endpoint_unknown"
	CodeAmbiguousDevice   ErrorCode = "ambiguous_device"
	CodeCloudAuth         ErrorCode = "cloud_auth"
	CodeNoSpace           ErrorCode = "no_space"
	CodeInvalidFilter     ErrorCode = "invalid_filter"
	// lifecycle
	CodeCancelledByUser    ErrorCode = "cancelled_by_user"
	CodeCancelledUnmounted ErrorCode = "cancelled_unmounted"
	CodeMaxDuration        ErrorCode = "max_duration"
	// lookups
	CodeNotFound ErrorCode = "not_found" // engine job id, version or path
	CodeInternal ErrorCode = "internal"  // a bug; the detail says where
)

// EngineCodes lists every code above, for jobs' completeness test.
var EngineCodes = []ErrorCode{
	CodeEngineUnavailable, CodeNetworkUnreachable, CodeCloudRateLimited, CodeDestOffline, CodeSourceOffline, CodeIOError,
	CodeCloudQuotaDaily,
	CodeDeleteGuard, CodeChangeGuard, CodeEmptySource, CodeDestMarkerMismatch,
	CodeDestInsideSource, CodePathNotAllowed, CodeFat32FileTooLarge, CodeCaseCollision, CodeEndpointUnknown,
	CodeAmbiguousDevice, CodeCloudAuth, CodeNoSpace, CodeInvalidFilter,
	CodeCancelledByUser, CodeCancelledUnmounted, CodeMaxDuration,
	CodeNotFound, CodeInternal,
}

// Error is the error type every API method returns. Detail is technical
// English for logs and "Show technical output" - never shown as the
// message itself.
type Error struct {
	Code   ErrorCode
	Detail string
	Guard  *GuardInfo // set for guard codes
	Err    error      // underlying cause, if any
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Detail
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an *Error with a formatted detail. A %w verb is kept as
// the underlying cause.
func Errorf(code ErrorCode, format string, args ...interface{}) *Error {
	err := fmt.Errorf(format, args...)
	return &Error{Code: code, Detail: err.Error(), Err: errors.Unwrap(err)}
}

// CodeOf extracts the code from any error: "" for nil, the *Error's code
// when there is one in the chain, cancelled_by_user for a cancelled
// context, max_duration for an expired deadline, io_error otherwise.
func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	switch {
	case errors.Is(err, context.Canceled):
		return CodeCancelledByUser
	case errors.Is(err, context.DeadlineExceeded):
		return CodeMaxDuration
	}
	return CodeIOError
}
