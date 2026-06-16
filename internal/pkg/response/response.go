package response

// Body is the unified HTTP response envelope.
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func OK(data interface{}) Body {
	return Body{Code: 0, Message: "OK", Data: data}
}

func Fail(code int, message string) Body {
	return Body{Code: code, Message: message}
}
