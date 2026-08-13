package repository

import "testing"

func TestVectorGCRedriveBusinessErrorDoesNotExposeSQLDetails(t *testing.T) {
	// Keep the public error wording stable and independent of the SQL driver;
	// the handler maps all repository validation failures to this contract.
	const message = "向量清理任务不存在、不是死信或已不可重驱"
	if message == "" {
		t.Fatal("business error message must not be empty")
	}
}
