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

// devAssetsRelative is where dev-only assets live, relative to both this source
// file and the repository root.
const devAssetsRelative = "internal/web/dev-assets"

// DevAssetsDir is the absolute path to the directory of dev-only asset files
// (see internal/web/dev-assets/README.md). It deliberately lives outside
// internal/web/static so it is NOT embedded into the production binary by
// //go:embed *.
//
// Resolution prefers this source file's compile-time location, so it works
// regardless of the process working directory. But runtime.Caller reports the
// path as recorded when the package was compiled, and a -trimpath build rewrites
// that to a module-relative path such as kids-checkin/internal/web/static/static.go.
// Joining ".." onto it yields kids-checkin/internal/web/dev-assets, which only
// resolves if the working directory happens to be the module root -- so under
// -trimpath the preferred candidate silently points at nothing.
//
// That is why the candidate is verified before it is trusted, and why a walk up
// from the working directory backs it up. Dropping either reintroduces
// TestReadDevAsset failing under -trimpath with
// "open kids-checkin/internal/web/dev-assets/preview.js: no such file or
// directory", which is exactly how this looked before it was fixed.
var DevAssetsDir = resolveDevAssetsDir()

func resolveDevAssetsDir() string {
	if _, thisFile, _, ok := runtime.Caller(0); ok {
		candidate := filepath.Join(filepath.Dir(thisFile), "..", "dev-assets")
		if dirExists(candidate) {
			return candidate
		}
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; {
			candidate := filepath.Join(dir, devAssetsRelative)
			if dirExists(candidate) {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	// Nothing found. Return the relative path so the resulting error names
	// something recognisable rather than an empty string.
	return devAssetsRelative
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

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
	".avif": {},
	".ico":  {},
}
