package static

import (
	"bytes"
	"image/color"
	"image/png"
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

// TestLogoRungsServe guards the picture ladder used on every page. The
// extension allowlist silently 404s anything unlisted, so a new rung that is
// not added there would fail only in the browser.
func TestLogoRungsServe(t *testing.T) {
	app := fiber.New()
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(NewFilteredFS()),
		PathPrefix: "",
		Browse:     true,
	}))

	for _, tc := range []struct {
		file      string
		wantType  string
		sizeLimit int64
	}{
		{"NWKids-logo-320.avif", "image/avif", 20_000},
		{"NWKids-logo-320.webp", "image/webp", 35_000},
		{"NWKids-logo-320.png", "image/png", 60_000},
	} {
		t.Run(tc.file, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/static/img/"+tc.file, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode,
				"%s must pass the extension allowlist or every page 404s its logo", tc.file)

			ct := resp.Header.Get("Content-Type")
			require.Equal(t, tc.wantType, ct)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Less(t, int64(len(body)), tc.sizeLimit,
				"%s regressed in size; the original SVG this replaced was 265042 bytes", tc.file)
		})
	}
}

// TestLogoRungsHaveOuterRings guards the artwork itself. NWKids-logo.svg
// layers two vector circles (an orange outer ring, a white inner ring) on top
// of its embedded raster. Regenerating the rungs from the embedded PNG instead
// of the whole SVG silently drops both rings, which is a visual regression no
// size or content-type assertion would catch.
func TestLogoRungsHaveOuterRings(t *testing.T) {
	const (
		orangeR = 245
		orangeG = 146
		orangeB = 30
	)

	src, err := EmbeddedFS.Open("img/NWKids-logo-320.png")
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	raw, err := io.ReadAll(src)
	require.NoError(t, err)

	img, err := png.Decode(bytes.NewReader(raw))
	require.NoError(t, err)

	b := img.Bounds()
	midY := b.Min.Y + (b.Max.Y-b.Min.Y)/2

	// Walk inward along the horizontal centre line. Correct artwork reads
	// orange, then white, then the teal disc. Rings dropped read teal almost
	// immediately. The outermost pixel is a semi-transparent antialiased edge,
	// and Go reports it premultiplied, so start one pixel in.
	const (
		edgeSkip   = 1  // antialiased transparent border
		orangeBand = 13 // px of orange ring
		whiteBand  = 12 // px of white ring inside it
	)
	orangeFrom := b.Min.X + edgeSkip
	orangeTo := orangeFrom + orangeBand

	isOrange := func(c color.RGBA) bool {
		return near(c.R, orangeR, 24) && near(c.G, orangeG, 24) && near(c.B, orangeB, 24)
	}
	isWhite := func(c color.RGBA) bool {
		return c.R > 235 && c.G > 235 && c.B > 235
	}
	at := func(x int) color.RGBA {
		r, g, bl, _ := img.At(x, midY).RGBA()
		return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: 255}
	}

	orangeOK, whiteOK := true, false
	for x := orangeFrom; x < orangeTo && x < b.Max.X; x++ {
		c := at(x)
		if !isOrange(c) {
			orangeOK = false
			break
		}
	}
	require.True(t, orangeOK,
		"the band just inside the logo edge is not the orange ring; the rings were dropped. "+
			"Regenerate the rungs from NWKids-logo.svg, not from its embedded PNG")

	for x := orangeTo; x < orangeTo+whiteBand && x < b.Max.X; x++ {
		if isWhite(at(x)) {
			whiteOK = true
			break
		}
	}
	require.True(t, whiteOK,
		"no white ring directly inside the orange ring; the rings were dropped. "+
			"Regenerate the rungs from NWKids-logo.svg, not from its embedded PNG")
}

func near(a, b, tol uint8) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= int(tol)
}
