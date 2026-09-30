package controllers

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"kids-checkin/internal/controllers/admin"
	"kids-checkin/internal/controllers/login"
	"kids-checkin/internal/controllers/middleware"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kids-checkin/internal/controllers/checkinv1"
	"kids-checkin/internal/controllers/eventv1"
	"kids-checkin/internal/controllers/guestcheckinv1"
	"kids-checkin/internal/controllers/locationgroupv1"
	"kids-checkin/internal/controllers/locationv1"
	"kids-checkin/internal/controllers/manualcheckinv1"
	"kids-checkin/internal/controllers/metricsv1"
	"kids-checkin/internal/controllers/planningcenterv1"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/metrics"
	"kids-checkin/internal/web/menu"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fibersession "github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/storage/sqlite3"
)

func StartServer(port int, dbFilepath string) error {
	database, err := db.InitDB(dbFilepath)
	if err != nil {
		panic(err)
	}

	storage := sqlite3.New(sqlite3.Config{
		Database: dbFilepath,
		Reset:    false, // Don't clear sessions on start
	})

	// 2. Setup Session Middleware with 2-week TTL
	store := fibersession.New(fibersession.Config{
		Storage:        storage,
		Expiration:     180 * 24 * time.Hour, // 180-day TTL
		CookieHTTPOnly: true,                 // Security: prevents JS from reading cookie
		CookieSameSite: "Lax",
	})

	app := fiber.New(fiber.Config{
		// Override default error handler
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			// Status code defaults to 500
			code := fiber.StatusInternalServerError

			// Retrieve the custom status code if it's a *fiber.Error

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
					return ctx.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
				}
				defer f.Close()

				ctx.Status(code)
				ctx.Type("html")
				return ctx.SendStream(f)
			}

			// Send custom error page
			err = ctx.Status(code).SendString(fmt.Sprintf(`{"sorry":"%s"}`, message))
			if err != nil {
				// In case the SendFile fails
				return ctx.Status(fiber.StatusInternalServerError).SendString("Internal Server Error")
			}

			// Return from handler
			return nil
		},
	})
	app.Use(middleware.RequestLogger())
	app.Use(middleware.HTTPAccessLogger())
	app.Use(recover.New())
	// Registered before the routes and the /static mount so every response,
	// including the chunked HTML pages, is compressed on the way out.
	app.Use(middleware.Compress())

	registerRoutes(app, database, store, storage)

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
		// In dev, cmd/assets skips asset versioning (it returns early when
		// ENVIRONMENT=dev), so the ?v= on every script tag stays a constant
		// "dev". A constant cache-buster cannot bust anything, and pairing it
		// with immutable pinned the previous build's JS in the browser for a
		// year -- the HTML is revalidated on every load while the JS it pulls
		// in is not, so after a `git pull` a page can run new markup against
		// old script. That is a version-skew failure, not a rendering one, and
		// it presents as erratic UI behavior that clears on a hard refresh.
		c.Set("Cache-Control", staticCacheControl())
		return c.Next()
	})
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(static.NewFilteredFS()),
		PathPrefix: "",
		Browse:     true,
	}))

	slog.Info("server listening", slog.Int("port", port))

	err = app.Listen(":" + strconv.Itoa(port))
	if err != nil {
		return err
	}

	slog.Info("server stopped")
	return nil
}

// staticCacheControl returns the Cache-Control header for a file served out of
// /static.
//
// Production serves these immutable, which is correct there because the Docker
// build runs cmd/assets with ENVIRONMENT=production: that bakes a SHA-256 of
// each asset into the ?v= query on every script and link tag, so the URL itself
// changes whenever the file's contents do. A changed file is therefore a
// changed URL, and there is nothing to revalidate.
//
// Dev has no such guarantee. cmd/assets returns early when ENVIRONMENT=dev, so
// the ?v= stays the literal "dev" in the checked-in HTML -- and the Makefile
// defaults ASSET_BUILD to 0, so `make build` never runs it at all. The
// cache-buster is constant and cannot bust anything. Combined with immutable
// that pins the previously-built JS in the browser for a year while the HTML,
// which is revalidated on every load, moves underneath it. After a `git pull`
// a page can run new markup against old script, which surfaces as erratic UI
// behavior that clears on a hard refresh.
//
// no-cache rather than no-store, so an unchanged asset still costs a
// conditional request instead of a full re-download on every reload.
func staticCacheControl() string {
	if static.IsDev() {
		return "no-cache"
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

	guestCheckinController := guestcheckinv1.NewController(db, sessionStore)
	guestCheckinController.RegisterRoutes(app)

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
