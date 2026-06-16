package v1

import "github.com/gogf/gf/v2/frame/g"

// AuthTokenReq is the login request for Phase 1 development.
type AuthTokenReq struct {
	g.Meta   `path:"/auth/token" method:"post" tags:"Auth" summary:"获取访问令牌（开发用）"`
	Username string `json:"username" v:"required"`
	Password string `json:"password" v:"required"`
}

// AuthTokenRes returns a JWT access token.
type AuthTokenRes struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}
