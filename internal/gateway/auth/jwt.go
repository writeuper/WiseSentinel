package auth

import (
	"context"
	"fmt"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/configx"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents JWT payload fields used by WiseSentinel.
type Claims struct {
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// IssueToken creates a signed JWT for the given user.
func IssueToken(ctx context.Context, userID string, roles []string, tenantID string) (string, int, error) {
	if tenantID == "" {
		tenantID = domain.DefaultTenantID
	}
	expireHours := g.Cfg().MustGet(ctx, "auth.jwt_expire_hours", 24).Int()
	secret := configx.String(ctx, "auth.jwt_secret", "JWT_SECRET")
	if secret == "" {
		return "", 0, fmt.Errorf("auth.jwt_secret is required")
	}

	now := time.Now()
	claims := Claims{
		TenantID: tenantID,
		Roles:    roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHours) * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", 0, err
	}
	return signed, expireHours * 3600, nil
}

// ParseToken validates and parses a JWT string.
func ParseToken(ctx context.Context, tokenString string) (*Claims, error) {
	secret := configx.String(ctx, "auth.jwt_secret", "JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("auth.jwt_secret is required")
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
