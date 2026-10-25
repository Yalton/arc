package initcmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/Yalton/arc/adapters/opencode"
)

// opencodeShimPath is where opencode auto-loads project plugins (see the
// contract in internal/hook/opencode.go).
const opencodeShimPath = ".opencode/plugins/arc.js"

// ensureOpencode installs arc's plugin shim. An existing file is replaced
// only when it is arc's own (its first line carries the marker); anything
// else is left alone with a notice.
func ensureOpencode(root string, res *Result) error {
	full := filepath.Join(root, filepath.FromSlash(opencodeShimPath))
	data, err := os.ReadFile(full)
	switch {
	case err == nil:
		if bytes.Equal(data, opencode.Shim) {
			return nil
		}
		if !bytes.HasPrefix(data, []byte(opencode.Marker)) {
			res.notice("%s exists and is not arc's; left unchanged (move it aside and rerun to install arc's shim)", opencodeShimPath)
			return nil
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, opencode.Shim, 0o644); err != nil {
		return err
	}
	if data == nil {
		res.created(opencodeShimPath)
		res.notice("opencode loads %s at startup; 'arc' must be on PATH", opencodeShimPath)
	} else {
		res.updated(opencodeShimPath)
	}
	return nil
}
