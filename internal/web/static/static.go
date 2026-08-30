package static

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed *
var EmbeddedFS embed.FS

func IsDev() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT"))) == "dev"
}

// DevAssetsDir is the absolute path to the directory of dev-only asset files
// (see internal/web/dev-assets/README.md). It is resolved from this source
// file's location so it works regardless of the process working directory,
// and it deliberately lives outside internal/web/static so it is NOT embedded
// into the production binary by //go:embed *.
var DevAssetsDir = func() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "internal/web/dev-assets"
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "dev-assets")
}()

// ReadDevAsset returns the contents of a single dev-asset file, guarding
// against path traversal. It returns os.ErrNotExist for empty, nested, or
// missing filenames. Callers must gate access on IsDev(); see
// internal/web/dev-assets/README.md for how this is served.
func ReadDevAsset(filename string) ([]byte, error) {
	if filename == "" || strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\`) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(DevAssetsDir, filename))
}

type filteredFS struct {
	fs      fs.FS
	allowed map[string]struct{}
}

func NewFilteredFS() filteredFS {
	return filteredFS{
		fs:      EmbeddedFS,
		allowed: allowedExt,
	}
}

func (f filteredFS) Open(name string) (fs.File, error) {
	// Normalize path (Fiber may pass leading slash)
	name = strings.TrimPrefix(path.Clean(name), "/")

	// Block directory access
	if strings.HasSuffix(name, "/") || name == "." {
		return nil, fs.ErrNotExist
	}

	ext := strings.ToLower(path.Ext(name))
	if _, ok := f.allowed[ext]; !ok {
		return nil, fs.ErrNotExist
	}

	return f.fs.Open(name)
}

var allowedExt = map[string]struct{}{
	".css":  {},
	".js":   {},
	".svg":  {},
	".jpg":  {},
	".png":  {},
	".webp": {},
	".ico":  {},
}
