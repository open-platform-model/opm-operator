package render

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

// The acquisition mark changes no message: the status condition and event
// read exactly as the plain wrap did, while errors.Is finds ErrAcquire and
// errors.AsType still reaches the typed cause underneath.
func TestAcquireFailed_MarksWithoutRewording(t *testing.T) {
	identity := &oerrors.IdentityError{
		Field:      "path",
		Declared:   "opmodel.dev/modules/other",
		Fetched:    "opmodel.dev/modules/demo",
		Coordinate: "opmodel.dev/modules/demo v1.0.0",
	}
	for _, msg := range []string{"acquiring module", "loading package"} {
		t.Run(msg, func(t *testing.T) {
			cause := fmt.Errorf("acquiring module %q: %w", "demo", identity)
			err := acquireFailed(msg, cause)

			assert.Equal(t, fmt.Errorf("%s: %w", msg, cause).Error(), err.Error())
			assert.ErrorIs(t, err, ErrAcquire)
			got, ok := errors.AsType[*oerrors.IdentityError](err)
			assert.True(t, ok, "the typed cause stays reachable")
			assert.Equal(t, identity, got)
		})
	}
}

func TestAcquireFailed_SentinelOnlyWhenMarked(t *testing.T) {
	assert.NotErrorIs(t, fmt.Errorf("loading package: %w", errors.New("boom")), ErrAcquire)
	assert.ErrorIs(t, acquireFailed("loading package", errors.New("boom")), ErrAcquire)
}
