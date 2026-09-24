package engine

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/fserrors"
)

// classifyError maps any error from rclone, the OS or the engine to an
// error code (spec §5 table). Engine errors keep their code; a cancelled
// context reports its cause.
func classifyError(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var ee *Error
	if errors.As(err, &ee) {
		return ee.Code
	}
	switch {
	case errors.Is(err, context.Canceled):
		return CodeCancelledByUser
	case errors.Is(err, context.DeadlineExceeded):
		return CodeMaxDuration
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return CodeNoSpace
	case errors.Is(err, syscall.EFBIG):
		return CodeFat32FileTooLarge
	case errors.Is(err, fs.ErrorNotFoundInConfigFile):
		return CodeEndpointUnknown
	case errors.Is(err, fs.ErrorOverlapping):
		return CodeDestInsideSource
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "max-delete threshold reached"), strings.Contains(msg, "max-delete-size threshold reached"):
		return CodeDeleteGuard
	case strings.Contains(msg, "no space left"), strings.Contains(msg, "storagequotaexceeded"), strings.Contains(msg, "insufficient storage"),
		strings.Contains(msg, "quota exceeded") && !strings.Contains(msg, "rate"):
		return CodeNoSpace
	case strings.Contains(msg, "userratelimitexceeded") && fserrors.IsFatalError(err),
		strings.Contains(msg, "upload limit"), strings.Contains(msg, "uploadlimitexceeded"):
		// Google Drive's 750 GB per day; stop_on_upload_limit makes it fatal.
		return CodeCloudQuotaDaily
	case strings.Contains(msg, "ratelimit"), strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"),
		hasHTTPStatus(msg, "429"):
		return CodeCloudRateLimited
	case isAuthMessage(msg):
		return CodeCloudAuth
	case isNetworkError(err, msg):
		return CodeNetworkUnreachable
	case strings.Contains(msg, "can't copy directory into itself"):
		return CodeDestInsideSource
	}
	return CodeIOError
}

func isAuthMessage(msg string) bool {
	if hasHTTPStatus(msg, "401") {
		return true
	}
	for _, s := range []string{
		"invalid_grant", "unauthorized", "403 forbidden", "token expired", "token has been expired",
		"couldn't fetch token", "cannot fetch token", "oauth2:", "authentication failed", "logon failure",
		"status_logon_failure", "access denied", "invalid credentials", "invalid_client", "login failed",
		"permission denied (publickey", "ssh: unable to authenticate", "wrong password", "bad credentials",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func isNetworkError(err error, msg string) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// Timeouts of HTTP clients; syscall.Errno also satisfies net.Error
	// (ENOENT is not a network problem), so it doesn't count here.
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		if _, isErrno := nerr.(syscall.Errno); !isErrno {
			return true
		}
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	for _, s := range []string{"connection refused", "no route to host", "network is unreachable", "i/o timeout",
		"tls handshake timeout", "no such host", "connection reset", "broken pipe", "server misbehaving"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// hasHTTPStatus finds an HTTP status code the way rclone's backends
// print it ("HTTP error 429", "status 401", "401 Unauthorized"), never a
// bare number inside a file name.
func hasHTTPStatus(msg, code string) bool {
	for _, p := range []string{"http error " + code, "http " + code, "status " + code, "statuscode " + code,
		"status code " + code, "error " + code, "(" + code + ")", code + " unauthorized", code + " too many"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// engineErr wraps err with its classified code and detail, keeping an
// existing *Error as is.
func engineErr(err error, detail string) *Error {
	var ee *Error
	if errors.As(err, &ee) {
		return ee
	}
	if detail != "" {
		detail += ": "
	}
	return &Error{Code: classifyError(err), Detail: detail + err.Error(), Err: err}
}
