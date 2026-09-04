package platform

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
)

const (
	// generationPrefix names a completed generation's directory:
	// <root>/gen-<generation>.
	generationPrefix = "gen-"

	// stagingPrefix names an in-progress write: <root>/.staging-<generation>-<random>.
	// Hidden so a directory listing reads current state first, and never a
	// generation directory's name so a crash mid-write cannot be mistaken for
	// a complete module.
	stagingPrefix = ".staging-"

	// asidePrefix names a superseded copy of a generation directory moved
	// out of the way during a same-generation swap.
	asidePrefix = ".aside-"
)

// Layout owns the on-disk lifecycle of generated platform modules under Root
// (the manager's --platform-dir): one directory per CR generation, written
// by staging plus rename so a module directory is either absent or complete,
// pruned to the current and every leased generation after each successful
// build, and emptied at manager start. The module content itself comes from
// the library's generator (opm/helper/platformmodule); the lifecycle is
// operator process policy, which is why it lives here beside the store that
// records the directories.
type Layout struct {
	Root string
}

// Dir returns the directory a generation's module lives in, whether or not
// it exists.
func (l Layout) Dir(generation int64) string {
	return filepath.Join(l.Root, generationPrefix+strconv.FormatInt(generation, 10))
}

// Write materialises files as generation's module directory and returns its
// path. The files are written into a staging directory first and renamed
// into place, so no reader observes a partially written module: a failure
// at any point leaves the staging directory (cleaned by the next Prune or
// Reset) and never a generation directory. An existing directory for the
// same generation (a re-reconcile after a build failure, or a container
// restart on the same volume) is moved aside before the swap and removed
// after it.
func (l Layout) Write(generation int64, files platformmodule.Files) (string, error) {
	if l.Root == "" {
		return "", errors.New("platform layout has no root directory")
	}
	if len(files) == 0 {
		return "", errors.New("no files to write")
	}
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return "", fmt.Errorf("creating platform directory %s: %w", l.Root, err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	staging := filepath.Join(l.Root, stagingPrefix+strconv.FormatInt(generation, 10)+"-"+suffix)
	if err := os.Mkdir(staging, 0o755); err != nil {
		return "", fmt.Errorf("creating staging directory %s: %w", staging, err)
	}
	if err := files.WriteTo(staging); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("writing platform module files: %w", err)
	}

	dir := l.Dir(generation)
	aside := filepath.Join(l.Root, asidePrefix+strconv.FormatInt(generation, 10)+"-"+suffix)
	if _, statErr := os.Stat(dir); statErr == nil {
		if err := os.Rename(dir, aside); err != nil {
			_ = os.RemoveAll(staging)
			return "", fmt.Errorf("moving aside existing module directory %s: %w", dir, err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("inspecting module directory %s: %w", dir, statErr)
	}
	if err := os.Rename(staging, dir); err != nil {
		_ = os.RemoveAll(staging)
		// Best effort: put the previous copy back so a same-generation
		// re-run is not left with nothing.
		_ = os.Rename(aside, dir)
		return "", fmt.Errorf("swapping module directory %s into place: %w", dir, err)
	}
	_ = os.RemoveAll(aside)
	return dir, nil
}

// Prune removes every entry under Root except the generation directories
// listed in keep: superseded generations, staging leftovers and moved-aside
// copies. A missing Root is not an error.
func (l Layout) Prune(keep ...int64) error {
	entries, err := os.ReadDir(l.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("listing platform directory %s: %w", l.Root, err)
	}
	kept := make(map[string]bool, len(keep))
	for _, g := range keep {
		kept[generationPrefix+strconv.FormatInt(g, 10)] = true
	}
	var errs []error
	for _, e := range entries {
		if kept[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(l.Root, e.Name())); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", e.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// Reset empties Root entirely (the boot path: disk is ephemeral, the CR is
// the source of truth, and the initial reconcile regenerates) and makes
// sure Root exists afterwards.
func (l Layout) Reset() error {
	if l.Root == "" {
		return errors.New("platform layout has no root directory")
	}
	if err := l.Prune(); err != nil {
		return err
	}
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return fmt.Errorf("creating platform directory %s: %w", l.Root, err)
	}
	return nil
}

// Generations lists the complete generation directories under Root, in
// ascending generation order. Staging and moved-aside entries are ignored.
func (l Layout) Generations() ([]int64, error) {
	entries, err := os.ReadDir(l.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing platform directory %s: %w", l.Root, err)
	}
	var out []int64
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), generationPrefix) {
			continue
		}
		g, err := strconv.ParseInt(strings.TrimPrefix(e.Name(), generationPrefix), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, g)
	}
	slices.Sort(out)
	return out, nil
}

func randomSuffix() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating staging suffix: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
