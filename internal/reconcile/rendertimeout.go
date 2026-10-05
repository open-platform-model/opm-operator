package reconcile

import (
	"errors"
	"fmt"
	"time"

	"github.com/open-platform-model/opm-operator/internal/render"
)

// renderKey names one object for the render pool, so the pool can refuse a
// second render of an object whose timed-out render is still running.
func renderKey(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}

// renderTimeoutMessage returns the RenderTimedOut message for an error from
// render.Slots.Run, and false when the error is not a render timeout.
func renderTimeoutMessage(err error, timeout time.Duration) (string, bool) {
	switch {
	case errors.Is(err, render.ErrRenderTimedOut):
		return fmt.Sprintf("render did not finish within %s; its render slot stays held until the render returns", timeout), true
	case errors.Is(err, render.ErrRenderStillRunning):
		return fmt.Sprintf("the previous render of this object did not finish within %s and is still running; no new render was started", timeout), true
	}
	return "", false
}
