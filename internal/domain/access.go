package domain

// Secret level values stored in ws_document.metadata and Milvus metadata.
const (
	SecretLevelInternal  = 1
	SecretLevelSensitive = 2
)

// MaxSecretLevelForRoles returns the highest document secret level visible to roles.
func MaxSecretLevelForRoles(roles []string) int {
	for _, role := range roles {
		switch Role(role) {
		case RoleSREAdmin, RolePlatformAdmin:
			return SecretLevelSensitive
		}
	}
	return SecretLevelInternal
}
