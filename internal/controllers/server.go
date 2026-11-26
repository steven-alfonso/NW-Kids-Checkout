package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"kids-checkin/internal/controllers/admin"
	"kids-checkin/internal/controllers/login"
	"kids-checkin/internal/controllers/middleware"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kids-checkin/internal/controllers/checkinv1"
	"kids-checkin/internal/controllers/eventv1"
	"kids-checkin/internal/controllers/locationgroupv1"
	"kids-checkin/internal/controllers/locationv1"
	"kids-checkin/internal/controllers/manualcheckinv1"
	"kids-checkin/internal/controllers/metricsv1"
	"kids-checkin/internal/controllers/planningcenterv1"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/metrics"
	sessionrepo "kids-checkin/internal/repo/session"
	"kids-checkin/internal/web/menu"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fibersession "github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/storage/sqlite3"
)

// errorHandler renders errors as JSON, keeping the "sorry" key the frontend
// reads. A 5xx response carries a generic message; the detail goes to the log
// so internal errors are not echoed to the client.
func errorHandler(ctx *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError

	message := ""
	if e, ok := errors.AsType[*fiber.Error](err); ok {
		message = e.Message
		code = e.Code
	}

	acceptsHTML := ctx.Accepts("html") != ""
	wantsHTML := acceptsHTML || strings.HasSuffix(ctx.Path(), ".html")
	if code == fiber.StatusNotFound && wantsHTML {
		f, openErr := static.EmbeddedFS.Open("pages/errors/404.html")
		if openErr != nil {
			middleware.GetLogger(ctx).ErrorContext(ctx.Context(), "failed to open 404 page",
				slog.String("error", openErr.Error()),
				slog.String("method", ctx.Method()),
				slog.String("path", ctx.Path()),
			)
			return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"sorry": "Internal Server Error"})
		}
		defer f.Close()

		ctx.Status(code)
		ctx.Type("html")
		return ctx.SendStream(f)
	}

	if code >= fiber.StatusInternalServerError {
		middleware.GetLogger(ctx).ErrorContext(ctx.Context(), "internal server error",
			slog.String("error", err.Error()),
			slog.String("method", ctx.Method()),
			slog.String("path", ctx.Path()),
			slog.Int("status", code),
		)
		message = "Internal Server Error"
	}

	if writeErr := ctx.Status(code).JSON(fiber.Map{"sorry": message}); writeErr != nil {
		// Defensive only: a single-key map cannot fail to marshal.
		_ = ctx.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
	}
	return nil
}

func newServerApp(database *sql.DB, store *fibersession.Store, paginationStore planningcenterv1.PaginationStore) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: errorHandler,
	})
	app.Use(middleware.RequestLogger())
	app.Use(middleware.HTTPAccessLogger())
	app.Use(recover.New())
	// Registered before the routes and the /static mount so every response,
	// including the chunked HTML pages, is compressed on the way out.
	app.Use(middleware.Compress())
	app.Use(middleware.NoIndex())

	registerRoutes(app, database, store, paginationStore)

	app.Get("manifest.webmanifest", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("manifest.webmanifest")
		if err != nil {
			middleware.GetLogger(c).WarnContext(c.Context(), "failed to open manifest.webmanifest", slog.String("error", err.Error()))
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		c.Type("application/manifest+json")
		return c.SendStream(f)
	})

	app.Get("apple-touch-icon.png", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("img/apple-touch-icon.png")
		if err != nil {
			middleware.GetLogger(c).WarnContext(c.Context(), "failed to open apple-touch-icon.png", slog.String("error", err.Error()))
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		c.Type("image/png")
		return c.SendStream(f)
	})

	app.Get("apple-touch-icon-precomposed.png", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("img/apple-touch-icon.png")
		if err != nil {
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		c.Type("image/png")
		return c.SendStream(f)
	})

	app.Get("favicon.ico", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("img/favicon.ico")
		if err != nil {
			middleware.GetLogger(c).WarnContext(c.Context(), "failed to open favicon.ico", slog.String("error", err.Error()))
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		c.Type("image/x-icon")
		return c.SendStream(f)
	})

	// Serve static pages. Should be the last of all registered routes.
	// Files under /static/dev/* are dev-only assets served from the
	// dev-assets directory when running in a dev environment. See
	// internal/web/dev-assets/README.md.
	app.Use("/static", func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/static/dev/") {
			if !static.IsDev() {
				return fiber.ErrNotFound
			}
			data, err := static.ReadDevAsset(strings.TrimPrefix(c.Path(), "/static/dev/"))
			if err != nil {
				return fiber.ErrNotFound
			}
			c.Set("Cache-Control", "no-store")
			c.Type(filepath.Ext(c.Path()))
			return c.Send(data)
		}
		c.Set("Cache-Control", staticCacheControl())
		return c.Next()
	})
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(static.NewFilteredFS()),
		PathPrefix: "",
		Browse:     true,
	}))

	return app
}

func StartServer(ctx context.Context, port int, dbFilepath string) error {
	// One spelling for both connections: InitDB only logs the absolute path
	// and opens the given spelling, so trim once here. Otherwise a padded
	// path would open the trimmed file for the app database and a spaced
	// filename for the session store.
	dbFilepath = strings.TrimSpace(dbFilepath)

	database, err := db.InitDB(dbFilepath)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}

	// The session store points at the same database file, using the same
	// spelling so both connections name one file.
	storage := sqlite3.New(sqlite3.Config{
		Database: dbFilepath,
		Table:    sessionrepo.Table,
		Reset:    false, // Don't clear sessions on start
	})

	// 2. Setup Session Middleware with 2-week TTL
	store := fibersession.New(fibersession.Config{
		Storage:        storage,
		Expiration:     180 * 24 * time.Hour, // 180-day TTL
		CookieHTTPOnly: true,                 // Security: prevents JS from reading cookie
		CookieSameSite: "Lax",
		CookieSecure:   cookieSecure(),
	})
	slog.Info("session cookie", slog.Bool("secure", cookieSecure()))

	app := newServerApp(database, store, storage)

	slog.Info("server listening", slog.Int("port", port))

	// Without this, a signal-aware root context would be swallowed: signal
	// delivery disables the default kill behavior, so Ctrl-C would hang a
	// server that never watches ctx. Shutting down Fiber lets Listen return.
	// The stop channel keeps this goroutine from leaking when the caller
	// never cancels (e.g. context.Background() in a test).
	if ctx == nil {
		ctx = context.Background()
	}
	stopCh := make(chan struct{})
	defer close(stopCh)
	// shutdownInitiated records that WE called Shutdown in response to ctx.
	// Testing ctx.Err() here instead would swallow any Listen failure that
	// races a cancel (e.g. port already in use + Ctrl-C at the same moment)
	// and exit 0 with no server running. Bind failures bypass the flag
	// entirely (see shouldSuppressListenError).
	var shutdownInitiated atomic.Bool
	go func() {
		select {
		case <-ctx.Done():
			shutdownInitiated.Store(true)
			_ = app.Shutdown()
		case <-stopCh:
		}
	}()

	err = app.Listen(":" + strconv.Itoa(port))
	if err != nil {
		// A context-driven shutdown is a clean exit, not a failure:
		// Listen returns an error after Shutdown, which must not turn
		// every Ctrl-C into exit 1 in main.
		if shouldSuppressListenError(err, shutdownInitiated.Load()) {
			return nil
		}
		return err
	}

	slog.Info("server stopped")
	return nil
}

// cookieSecure reports whether the session cookie carries the Secure flag.
// It defaults to false so the current HTTP deployment keeps working. Set
// COOKIE_SECURE=true only when every browser URL is https://. The environment
// is read once, on the first call.
var cookieSecure = newCookieSecure(os.Getenv)

func newCookieSecure(getenv func(string) string) func() bool {
	return sync.OnceValue(func() bool {
		return parseCookieSecure(getenv("COOKIE_SECURE"))
	})
}

// parseCookieSecure reports whether value enables the flag. Only the literal
// true, case-insensitive and trimmed, does.
func parseCookieSecure(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// shouldSuppressListenError reports whether a Listen error is a clean
// context-driven shutdown rather than a real failure.
//
// Bind failures ("failed to listen: ...", Fiber listen.go) happen before Serve
// and must always surface: suppressing one that races ctx cancel exits 0 with
// no server running. Clean shutdown returns nil from Serve (fasthttp), so it
// never reaches here as an error.
func shouldSuppressListenError(err error, shutdownInitiated bool) bool {
	if err == nil {
		return false
	}
	if isBindError(err) {
		return false
	}
	return shutdownInitiated
}

// isBindError reports whether err is a listener bind failure, which Fiber
// prefixes with "failed to listen:" (listen.go) before Serve starts.
func isBindError(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "failed to listen")
}

// staticCacheControl returns the Cache-Control header for a file served out of
// /static.
//
// Production is immutable and safe: cmd/assets bakes a content hash into each
// asset's ?v=, so a changed file is a changed URL. Dev has no such guarantee --
// the ?v= stays a constant "dev" -- and the embedded FS serves no ETag or
// Last-Modified, so no-cache would have nothing to revalidate against. Hence
// no-store, which forbids reuse outright.
func staticCacheControl() string {
	if static.IsDev() {
		return "no-store"
	}
	return "public, max-age=31536000, immutable"
}

func homePageHandler(sessionStore session.Storer) fiber.Handler {
	return func(c *fiber.Ctx) error {
		sess, err := sessionStore.Get(c)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "could not fetch session")
		}
		authenticated, _ := sess.Get("authenticated").(bool)
		role, _ := sess.Get("role").(string)

		f, err := static.EmbeddedFS.Open("pages/home/index.html")
		if err != nil {
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		content, err := io.ReadAll(f)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		menuHTML, err := menu.RenderHTML(authenticated, role)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		html := strings.Replace(string(content), menu.Placeholder, menuHTML, 1)

		c.Type("html")
		return c.Send([]byte(html))
	}
}

func registerRoutes(app *fiber.App, db *sql.DB, sessionStore *fibersession.Store, paginationStore planningcenterv1.PaginationStore) {
	app.Get("/", homePageHandler(sessionStore))

	app.Get("/api/session", func(c *fiber.Ctx) error {
		sess, _ := sessionStore.Get(c)
		role, _ := sess.Get("role").(string)
		authenticated, _ := sess.Get("authenticated").(bool)

		return c.JSON(fiber.Map{
			"authenticated": authenticated,
			"role":          role,
		})
	})

	loginController := login.NewController(sessionStore)
	loginController.RegisterRoutes(app)

	checkinController := checkinv1.NewController(db, sessionStore)
	checkinController.RegisterRoutes(app)

	manualCheckinController := manualcheckinv1.NewController(db, sessionStore)
	manualCheckinController.RegisterRoutes(app)

	// Guest check-in is disabled, so /guest-checkin,
	// /v1/checkins/guest-submissions*, and /admin/guest-entries are not
	// registered. Re-enable only with an Access bypass for the public paths and
	// a Turnstile check on the form; see the runbook for the full revival list
	// (docs/expose-the-app-to-the-internet.md).

	locationV1Controller := locationv1.NewController(db, sessionStore)
	locationV1Controller.RegisterRoutes(app)

	locationGroupV1Controller := locationgroupv1.NewController(db, sessionStore)
	locationGroupV1Controller.RegisterRoutes(app)

	eventV1Controller := eventv1.NewController(db, sessionStore)
	eventV1Controller.RegisterRoutes(app)

	planningCenterV1Controller := planningcenterv1.NewController(sessionStore, paginationStore)
	planningCenterV1Controller.RegisterRoutes(app)

	metricsRepo := metrics.NewRepo(db)
	metricsV1Controller := metricsv1.NewController(metricsRepo, sessionStore)
	metricsV1Controller.RegisterRoutes(app)

	adminController := admin.NewController(sessionStore)
	adminController.RegisterRoutes(app)
}
