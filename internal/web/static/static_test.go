package static

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestResolveDevAssetsDir covers the walk-up that backs up the compile-time path.
// The case that matters is -trimpath: runtime.Caller then reports a module-relative
// location like kids-checkin/internal/web/static/static.go, so the preferred
// candidate resolves against the working directory and points at nothing. TestReadDevAsset
// is the only thing that catches this, and only when someone runs -trimpath --
// without this test the verification can be dropped and go green on a normal build.
func TestResolveDevAssetsDir(t *testing.T) {
	// makeRepo lays out <root>/internal/web/dev-assets and returns root.
	makeRepo := func(t *testing.T, assets bool) string {
		t.Helper()
		root := t.TempDir()
		if assets {
			require.NoError(t, os.MkdirAll(filepath.Join(root, devAssetsRelative), 0o755))
		} else {
			require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "web"), 0o755))
		}
		return root
	}

	t.Run("walks up from a nested package directory", func(t *testing.T) {
		root := makeRepo(t, true)
		nested := filepath.Join(root, "internal", "web", "static")
		require.NoError(t, os.MkdirAll(nested, 0o755))

		got := resolveDevAssetsDir()
		assertResolvesTo(t, root, got)
	})

	t.Run("finds the real directory from this package", func(t *testing.T) {
		// Whatever working directory the test runs in, resolution must reach the
		// checked-in assets rather than falling back to a relative guess.
		got := resolveDevAssetsDir()
		require.NotEqual(t, devAssetsRelative, got,
			"resolution fell back to the relative path, so nothing was verified")
		assertResolvesTo(t, repoRoot(t), got)
	})
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}

func assertResolvesTo(t *testing.T, root, got string) {
	t.Helper()
	require.NotEmpty(t, got)
	info, err := os.Stat(filepath.Join(got, "preview.js"))
	require.NoError(t, err, "resolved dir %q does not contain preview.js (expected under %q)", got, root)
	require.False(t, info.IsDir())
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

// TestFaviconIsMultiSize guards the single largest asset on a page load.
// Browsers request /favicon.ico unprompted on every navigation when no page
// declares <link rel="icon">, so its size is paid on every page view. The
// original was one 256x247 32bpp entry stored as an uncompressed DIB, 260894
// bytes for an image that is only ever drawn at 16-48px.
func TestFaviconIsMultiSize(t *testing.T) {
	raw := readEmbedded(t, "img/favicon.ico")
	require.Less(t, len(raw), 30_000,
		"favicon.ico is %d bytes; the browser fetches it on every page load. "+
			"Regenerate it as a compressed multi-size ICO (16/32/48), not one large DIB", len(raw))

	// ICON container: reserved=0, type=1 (icon), then a 16-byte directory
	// entry per image.
	require.GreaterOrEqual(t, len(raw), 6, "too small to be an ICO")
	reserved, typ := binary.LittleEndian.Uint16(raw[0:2]), binary.LittleEndian.Uint16(raw[2:4])
	require.Equal(t, uint16(0), reserved)
	require.Equal(t, uint16(1), typ)
	count := int(binary.LittleEndian.Uint16(raw[4:6]))
	require.GreaterOrEqual(t, count, 2,
		"favicon.ico holds %d image(s); a browser tab renders 16px and a bookmark 32px, "+
			"so it needs a multi-size set", count)

	// No single entry may be a large uncompressed bitmap. ICO permits a
	// PNG-compressed payload only for the 256x256 entry; smaller ones are
	// DIB plus a 1bpp AND mask, so they cost slightly more than 4 bytes per
	// pixel. That is expected. What must not happen is a 256px entry stored
	// raw, which is what made the original 260894 bytes.
	for i := range count {
		entry := raw[6+i*16:]
		width := int(entry[0])
		if width == 0 {
			width = 256
		}
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		require.LessOrEqual(t, offset+size, len(raw),
			"entry %d points outside the file (%d+%d > %d)", i, offset, size, len(raw))

		if width < 256 {
			// DIB is mandatory here; just keep it sane.
			require.LessOrEqual(t, size, width*width*5,
				"entry %d (%dx%d) is %d bytes, larger than raw pixels plus a mask",
				i, width, width, size)
			continue
		}
		payload := raw[offset : offset+8]
		require.True(t, bytes.Equal(payload, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}),
			"the 256x256 entry (%d bytes) must be PNG-compressed; a raw DIB there "+
				"costs 262144 bytes on every page load", size)
	}
}

// TestManifestIconsAreReasonable guards the icons the manifest pulls in. The
// 192px one is fetched on every page load, not only on install.
func TestManifestIconsAreReasonable(t *testing.T) {
	for _, tc := range []struct {
		file      string
		sizeLimit int64
	}{
		// A 192x192 image of fine halftone dots is legitimately a few tens of
		// KB; these ceilings only catch a return to the original hand-exported
		// files, which were near the theoretical size for an uncompressed RGBA
		// bitmap.
		{"android-chrome-192x192.png", 60_000},
		{"android-chrome-512x512.png", 260_000},
		{"apple-touch-icon.png", 55_000},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw := readEmbedded(t, "img/"+tc.file)
			require.Less(t, int64(len(raw)), tc.sizeLimit,
				"%s is %d bytes", tc.file, len(raw))
		})
	}
}

func readEmbedded(t *testing.T, name string) []byte {
	t.Helper()
	f, err := EmbeddedFS.Open(name)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	require.NoError(t, err)
	return raw
}
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
