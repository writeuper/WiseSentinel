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
	ErrBadRequest           = New(40001, 400, "参数校验失败")
	ErrUnauthorized         = New(40101, 401, "未认证")
	ErrForbidden            = New(40301, 403, "无权限")
	ErrNotFound             = New(40401, 404, "资源不存在")
	ErrConflict             = New(40901, 409, "请求状态冲突，请使用新的请求标识重试")
	ErrRateLimited          = New(42901, 429, "请求过于频繁")
	ErrInternal             = New(50001, 500, "内部错误")
	ErrAgentFailed          = New(50002, 500, "Agent 执行失败")
	ErrToolFailed           = New(50003, 500, "工具调用失败")
	ErrRAGFailed            = New(50004, 500, "RAG 检索/索引失败")
	ErrModelUnavailable     = New(50301, 503, "模型服务不可用")
	ErrModelTimeout         = New(50401, 504, "模型服务响应超时")
	ErrModelOverloaded      = New(50304, 503, "模型服务当前繁忙，请稍后重试")
	ErrRateLimitUnavailable = New(50302, 503, "限流服务暂不可用")
	// ErrHighRiskWorkflowUnavailable prevents an L2 action from being
	// represented as approved when no durable, idempotent execution workflow
	// is configured for it.
	ErrHighRiskWorkflowUnavailable = New(50303, 503, "高风险工具执行工作流尚未启用")
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
