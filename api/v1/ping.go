package v1

import "github.com/gogf/gf/v2/frame/g"

// PingReq is a lightweight platform ping.
type PingReq struct {
	g.Meta `path:"/ping" method:"get" tags:"System" summary:"平台连通性检查"`
}

// PingRes returns platform metadata.
type PingRes map[string]interface{}
