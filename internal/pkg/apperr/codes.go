package apperr

import "fmt"

// AppError is a domain error with HTTP mapping.
type AppError struct {
	Code    int
	HTTP    int
	Message string
}

func (e *AppError) Error() string {
	return e.Message
}

func New(code, http int, message string) *AppError {
	return &AppError{Code: code, HTTP: http, Message: message}
}

var (
	ErrBadRequest       = New(40001, 400, "参数校验失败")
	ErrUnauthorized     = New(40101, 401, "未认证")
	ErrForbidden        = New(40301, 403, "无权限")
	ErrNotFound         = New(40401, 404, "资源不存在")
	ErrRateLimited      = New(42901, 429, "请求过于频繁")
	ErrInternal         = New(50001, 500, "内部错误")
	ErrAgentFailed      = New(50002, 500, "Agent 执行失败")
	ErrToolFailed       = New(50003, 500, "工具调用失败")
	ErrRAGFailed        = New(50004, 500, "RAG 检索/索引失败")
	ErrModelUnavailable = New(50301, 503, "模型服务不可用")
)

func Wrap(err error, fallback *AppError) error {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*AppError); ok {
		return ae
	}
	return fmt.Errorf("%s: %w", fallback.Message, err)
}
