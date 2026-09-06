package static

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/stretchr/testify/require"
)

func TestIsDev(t *testing.T) {
	t.Setenv("ENVIRONMENT", "")
	if IsDev() {
		t.Fatal("expected IsDev()=false when ENVIRONMENT is empty")
	}

	t.Setenv("ENVIRONMENT", "dev")
	if !IsDev() {
		t.Fatal("expected IsDev()=true when ENVIRONMENT=dev")
	}

	t.Setenv("ENVIRONMENT", "DEV")
	if !IsDev() {
		t.Fatal("expected IsDev()=true when ENVIRONMENT=DEV (case-insensitive)")
	}

	t.Setenv("ENVIRONMENT", "production")
	if IsDev() {
		t.Fatal("expected IsDev()=false when ENVIRONMENT=production")
	}
}

func TestReadDevAsset(t *testing.T) {
	data, err := ReadDevAsset("preview.js")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	t.Run("rejects empty filename", func(t *testing.T) {
		_, err := ReadDevAsset("")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		_, err := ReadDevAsset("../static.go")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("rejects nested paths", func(t *testing.T) {
		_, err := ReadDevAsset("sub/preview.js")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("rejects missing file", func(t *testing.T) {
		_, err := ReadDevAsset("does-not-exist.js")
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestPreviewJSIsNotEmbedded(t *testing.T) {
	if _, err := EmbeddedFS.Open("pages/checkoutsv1/preview.js"); err == nil {
		t.Fatal("preview.js should not be embedded in production assets")
	}
}

func TestCheckoutsHTMLDoesNotReferencePreview(t *testing.T) {
	f, err := EmbeddedFS.Open("pages/checkoutsv1/checkouts.html")
	if err != nil {
		t.Fatalf("open checkouts.html: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read checkouts.html: %v", err)
	}
	if bytes.Contains(content, []byte("preview.js")) {
		t.Fatal("checkouts.html must not reference preview.js; the tag is injected only in dev")
	}
}

func TestFilteredFSBlocksHTML(t *testing.T) {
	fsys := NewFilteredFS()

	t.Run("blocks pages html via filtered FS", func(t *testing.T) {
		_, err := fsys.Open("pages/admin/index.html")
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("blocks admin-guest-entries html via filtered FS", func(t *testing.T) {
		_, err := fsys.Open("pages/admin-guest-entries/index.html")
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("blocks with leading slash", func(t *testing.T) {
		_, err := fsys.Open("/pages/admin/index.html")
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("still allows js", func(t *testing.T) {
		f, err := fsys.Open("pages/guest-checkin/guest-checkin.js")
		require.NoError(t, err)
		_ = f.Close()
	})

	t.Run("still allows css", func(t *testing.T) {
		f, err := fsys.Open("css/tailwind.css")
		require.NoError(t, err)
		_ = f.Close()
	})

	t.Run("GET /static/pages/admin/index.html returns 404", func(t *testing.T) {
		app := fiber.New()
		app.Use("/static", filesystem.New(filesystem.Config{
			Root:       http.FS(NewFilteredFS()),
			PathPrefix: "",
			Browse:     true,
		}))
		req := httptest.NewRequest(http.MethodGet, "/static/pages/admin/index.html", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("GET /static/pages/guest-checkin/guest-checkin.js still serves", func(t *testing.T) {
		app := fiber.New()
		app.Use("/static", filesystem.New(filesystem.Config{
			Root:       http.FS(NewFilteredFS()),
			PathPrefix: "",
			Browse:     true,
		}))
		req := httptest.NewRequest(http.MethodGet, "/static/pages/guest-checkin/guest-checkin.js", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
