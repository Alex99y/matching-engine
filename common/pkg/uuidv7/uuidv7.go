package uuidv7

import (
	"fmt"

	"github.com/google/uuid"
)

var ErrGenerateUUID = fmt.Errorf("failed to generate uuidv7")

// New returns a new UUIDv7. Call .String() on the result to get a
// lowercase hyphenated string suitable as a PostgreSQL UUID parameter.
func New() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("uuidv7: %w: %w", ErrGenerateUUID, err)
	}
	return id, nil
}

func FromString(id string) (uuid.UUID, error) {
	return uuid.Parse(id)
}
