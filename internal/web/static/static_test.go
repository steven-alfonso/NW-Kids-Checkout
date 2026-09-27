package static

import (
	"bytes"
	"image"
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
		{"NWKids-logo-320.webp", "image/webp", 30_000},
		{"NWKids-logo-320.png", "image/png", 35_000},
	} {
		t.Run(tc.file, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/static/img/"+tc.file, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, http.StatusOK, resp.StatusCode,
				"%s must pass the extension allowlist or every page 404s its logo", tc.file)

			ct := resp.Header.Get("Content-Type")
			require.Equal(t, tc.wantType, ct)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
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
//
// The band widths are derived from the image rather than hardcoded, so this
// still holds if a rung is re-rendered at a different width. It reads the PNG
// rung only: decoding AVIF or WebP would need a new dependency, and all three
// rungs are generated from the same SVG in one pass, so a defect in the
// artwork shows up in all three together.
func TestLogoRungsHaveOuterRings(t *testing.T) {
	img := decodeLogoRung(t, "img/NWKids-logo-320.png")

	b := img.Bounds()
	width := b.Dx()
	midY := b.Min.Y + b.Dy()/2

	// The two rings measure 15px and 12px at a 320px rung width. Scale that
	// with the image rather than hardcoding, then subtract a pixel of slack:
	// the SVG strokes do not scale exactly linearly with the rasterised width,
	// so at 640px the orange ring measures 29px rather than the 30px a strict
	// ratio predicts. Without the slack a correct re-render fails this guard
	// and the natural "fix" is to loosen it until it detects nothing.
	//
	// This has a floor: below roughly 240px wide the rings are only a few
	// pixels across and the white ring stops resolving into a contiguous run,
	// so the guard cannot see it. The rungs are 320px; a smaller one would need
	// a different check, and this is the signal to write it.
	orangeBand := max(width*15/320-1, 8)
	whiteBand := max(width*12/320-1, 6)

	// Walk inward along the horizontal centre line. Correct artwork reads, in
	// order and without interruption: transparent edge, a contiguous orange
	// ring, a contiguous white ring, then the teal disc.
	//
	// Order and contiguity matter. A ring-less render still has a pale edge and
	// stray orange halftone dots further in, so merely finding "an orange pixel
	// somewhere" and "a white pixel somewhere" is not enough to prove the rings
	// are there.
	firstContent := -1
	orangeRun, whiteRun := 0, 0
	sawTeal := false
	for x := b.Min.X; x < b.Max.X; x++ {
		c := straightRGBA(img, x, midY)
		if firstContent == -1 {
			if c.A < 200 {
				continue // still in the antialiased transparent border
			}
			firstContent = x
		}
		switch {
		case isOrange(c):
			orangeRun++
		case isWhite(c):
			// White only counts if the orange band has already ended, otherwise
			// the pale outer edge of a ring-less render satisfies it.
			if orangeRun >= orangeBand {
				whiteRun++
			}
		default:
			// The teal disc marks the end of both rings.
			if isTeal(c) {
				sawTeal = true
			}
			orangeRun, whiteRun = 0, 0
		}
	}

	require.NotEqual(t, -1, firstContent,
		"the logo has no opaque pixels at all; the asset was flattened or emptied")

	droppedHint := "Regenerate the rungs from NWKids-logo.svg, not from its embedded PNG"

	// Longest contiguous run of each colour across the whole scan.
	bestOrange, bestWhite := 0, 0
	run := 0
	prev := ""
	for x := firstContent; x < b.Max.X; x++ {
		cur := bandOf(straightRGBA(img, x, midY))
		if cur == prev && cur != "" {
			run++
		} else {
			run = 1
		}
		prev = cur
		switch cur {
		case "orange":
			bestOrange = max(bestOrange, run)
		case "white":
			bestWhite = max(bestWhite, run)
		}
	}

	require.GreaterOrEqual(t, bestOrange, orangeBand,
		"longest contiguous orange run is %dpx, want at least %dpx. "+
			"The outer ring is missing. %s", bestOrange, orangeBand, droppedHint)
	require.GreaterOrEqual(t, bestWhite, whiteBand,
		"longest contiguous white run is %dpx, want at least %dpx. "+
			"The inner ring is missing. %s", bestWhite, whiteBand, droppedHint)
	require.True(t, sawTeal,
		"never reached the teal disc; the scan did not cross the logo")
}

// TestLogoRungsKeepAlpha guards the transparency. The logo is RGBA and is drawn
// on white and slate backgrounds, so a flattened or premultiplied regeneration
// would show as a dark halo or a visible box. The ring test above discards
// alpha, so it cannot catch that on its own.
func TestLogoRungsKeepAlpha(t *testing.T) {
	img := decodeLogoRung(t, "img/NWKids-logo-320.png")
	b := img.Bounds()

	// Corners sit outside the circular artwork and must be fully transparent.
	for _, c := range []struct {
		name string
		x, y int
	}{
		{"top-left", b.Min.X, b.Min.Y},
		{"top-right", b.Max.X - 1, b.Min.Y},
		{"bottom-left", b.Min.X, b.Max.Y - 1},
		{"bottom-right", b.Max.X - 1, b.Max.Y - 1},
	} {
		got := straightRGBA(img, c.x, c.y)
		require.Less(t, got.A, uint8(16),
			"%s corner is opaque (alpha %d); the asset was flattened onto a background. "+
				"It would render as a box on the slate pages", c.name, got.A)
	}

	// The centre of the disc is fully opaque, and there must be a real gradient
	// of partial alpha along the antialiased edge rather than a hard cut.
	opaque, partial := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			switch a := straightRGBA(img, x, y).A; {
			case a == 255:
				opaque++
			case a > 0:
				partial++
			}
		}
	}
	require.Greater(t, opaque, 0, "no fully opaque pixels; the asset is uniformly translucent")
	require.Greater(t, partial, 100,
		"almost no partially transparent pixels; the alpha channel was thresholded or replaced")
}

// decodeLogoRung reads a logo rung out of the embedded FS.
func decodeLogoRung(t *testing.T, name string) image.Image {
	t.Helper()
	src, err := EmbeddedFS.Open(name)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	raw, err := io.ReadAll(src)
	require.NoError(t, err)

	img, err := png.Decode(bytes.NewReader(raw))
	require.NoError(t, err)
	return img
}

// straightRGBA returns a pixel's colour and alpha without premultiplication.
// image.NRGBA.At would do this directly, but the rungs may decode as paletted
// or RGB images, and NRGBA.RGBA premultiplies, which darkens semi-transparent
// edge pixels and makes colour assertions unreliable there.
func straightRGBA(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	if a == 0 {
		return color.RGBA{}
	}
	// Undo the premultiplication so the colour is comparable to the source.
	scale := func(v uint32) uint8 {
		if a == 0xffff {
			return uint8(v >> 8)
		}
		return uint8(min(255, int(v>>8)*0xffff/int(a)))
	}
	return color.RGBA{R: scale(r), G: scale(g), B: scale(b), A: uint8(a >> 8)}
}

func isOrange(c color.RGBA) bool {
	return c.A > 200 && near(c.R, 245, 24) && near(c.G, 146, 24) && near(c.B, 30, 24)
}

func isWhite(c color.RGBA) bool {
	return c.A > 200 && c.R > 235 && c.G > 235 && c.B > 235
}

// isTeal matches the disc the rings enclose, rgb(34,101,127), with enough
// slack for the halftone dither that overlays it.
func isTeal(c color.RGBA) bool {
	return c.A > 200 && near(c.R, 34, 30) && near(c.G, 101, 30) && near(c.B, 127, 30)
}

// bandOf classifies a pixel into the ring band it belongs to.
func bandOf(c color.RGBA) string {
	switch {
	case isOrange(c):
		return "orange"
	case isWhite(c):
		return "white"
	default:
		return ""
	}
}

func near(a, b, tol uint8) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= int(tol)
}
