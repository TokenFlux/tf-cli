package ui

import (
	"errors"
	"strings"
)

// Code 是稳定的英文错误码。无论文案语言如何，它都不变，
// 便于搜索、贴给 AI 排查、以及被脚本消费。
type Code string

const (
	CodeUsage           Code = "TF_USAGE"
	CodeUnknownCommand  Code = "TF_UNKNOWN_COMMAND"
	CodeUnknownFlag     Code = "TF_UNKNOWN_FLAG"
	CodeMissingValue    Code = "TF_FLAG_MISSING_VALUE"
	CodeConfigRead      Code = "TF_CONFIG_READ"
	CodeConfigWrite     Code = "TF_CONFIG_WRITE"
	CodeCredentialsRead Code = "TF_CREDENTIALS_READ"
	CodeNotLoggedIn     Code = "TF_NOT_LOGGED_IN"

	CodeHarnessNotFound     Code = "TF_HARNESS_NOT_FOUND"
	CodeHarnessNotInstalled Code = "TF_HARNESS_NOT_INSTALLED"
	CodeInstallFailed       Code = "TF_INSTALL_FAILED"
	CodeInstallIncomplete   Code = "TF_INSTALL_INCOMPLETE"
	CodeCancelled           Code = "TF_CANCELLED"

	CodeKeyNotFound      Code = "TF_KEY_NOT_FOUND"
	CodeProtocolMismatch Code = "TF_PROTOCOL_MISMATCH"
	CodeNetwork          Code = "TF_NETWORK"
	CodeInternal         Code = "TF_INTERNAL"
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

// causeText 返回底层原因的单行摘要。
//
// 与 Message 相同则不重复输出 —— 同一句话说两遍只会稀释信息。
func causeText(e *Error) string {
	if e.Cause == nil {
		return ""
	}
	s := e.Cause.Error()
	if s == e.Message || strings.Contains(e.Message, s) {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 160
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}
