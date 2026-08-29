package ui

import "errors"

// Code 是稳定的英文错误码。无论文案语言如何，它都不变，
// 便于搜索、贴给 AI 排查、以及被脚本消费。
type Code string

const (
	CodeUsage           Code = "TKR_USAGE"
	CodeUnknownCommand  Code = "TKR_UNKNOWN_COMMAND"
	CodeUnknownFlag     Code = "TKR_UNKNOWN_FLAG"
	CodeMissingValue    Code = "TKR_FLAG_MISSING_VALUE"
	CodeConfigRead      Code = "TKR_CONFIG_READ"
	CodeConfigWrite     Code = "TKR_CONFIG_WRITE"
	CodeCredentialsRead Code = "TKR_CREDENTIALS_READ"
	CodeNotLoggedIn     Code = "TKR_NOT_LOGGED_IN"

	CodeHarnessNotFound     Code = "TKR_HARNESS_NOT_FOUND"
	CodeHarnessNotInstalled Code = "TKR_HARNESS_NOT_INSTALLED"
	CodeInstallFailed       Code = "TKR_INSTALL_FAILED"
	CodeInstallIncomplete   Code = "TKR_INSTALL_INCOMPLETE"
	CodeCancelled           Code = "TKR_CANCELLED"

	CodeKeyNotFound      Code = "TKR_KEY_NOT_FOUND"
	CodeProtocolMismatch Code = "TKR_PROTOCOL_MISMATCH"
	CodeNetwork          Code = "TKR_NETWORK"
	CodeInternal         Code = "TKR_INTERNAL"
)

// Error 是带错误码与修复建议的结构化错误。
// Message 与 Hint 已本地化，Code 永远是英文。
type Error struct {
	Code    Code
	Message string
	Hint    string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

// Errf 构造一个结构化错误。
func Errf(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithHint 附加一条可执行的修复建议。
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// HintPath 把一个路径作为提示，方便用户直接去看。
func (e *Error) HintPath(path string) *Error {
	e.Hint = path
	return e
}

// WithCause 附加底层错误，便于 --log-level=debug 时展开。
func (e *Error) WithCause(err error) *Error {
	e.Cause = err
	return e
}

// AsError 把任意 error 归一化为结构化错误。
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Message: err.Error(), Cause: err}
}
