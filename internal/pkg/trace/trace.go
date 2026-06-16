package trace

import (
	"strings"

	"github.com/google/uuid"
)

// NewID generates a trace identifier.
func NewID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}
