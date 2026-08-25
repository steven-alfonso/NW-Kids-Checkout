package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kids-checkin/internal/controllers/admin"
	"kids-checkin/internal/controllers/login"
	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/telemetry"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kids-checkin/internal/controllers/checkinv1"
	"kids-checkin/internal/controllers/eventv1"
	"kids-checkin/internal/controllers/locationgroupv1"
	"kids-checkin/internal/controllers/locationv1"
	"kids-checkin/internal/controllers/manualcheckinv1"
	"kids-checkin/internal/controllers/metricsv1"
	"kids-checkin/internal/controllers/planningcenterv1"
	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/metrics"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/storage/sqlite3"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

const apiServiceName = "kids-checkin-api"

func StartServer(port int, dbFilepath string) error {
	tel, err := telemetry.Setup(context.Background(), apiServiceName)
	if err != nil {
		return fmt.Errorf("setting up telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := tel.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.Warn("telemetry shutdown failed", slog.String("error", shutdownErr.Error()))
		}
	}()

	if runtimeErr := runtime.Start(); runtimeErr != nil {
		slog.Warn("runtime metrics unavailable", slog.String("error", runtimeErr.Error()))
	}

	database, err := db.InitDB(dbFilepath)
	if err != nil {
		panic(err)
	}

	storage := sqlite3.New(sqlite3.Config{
		Database: dbFilepath,
		Reset:    false, // Don't clear sessions on start
	})

	// 2. Setup Session Middleware with 2-week TTL
	store := session.New(session.Config{
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
			var e *fiber.Error

			message := ""
			if errors.As(err, &e) {
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
	if tel.Enabled() {
		app.Use(otelfiber.Middleware(
			otelfiber.WithTracerProvider(tel.TracerProvider),
			otelfiber.WithSpanNameFormatter(func(c *fiber.Ctx) string {
				return c.Route().Path
			}),
		))
		httpMetrics, metricsErr := middleware.HTTPMetrics(tel.Meter(apiServiceName))
		if metricsErr != nil {
			return fmt.Errorf("creating http metrics middleware: %w", metricsErr)
		}
		app.Use(httpMetrics)
	}
	app.Use(middleware.HTTPAccessLogger())
	app.Use(recover.New())

	registerRoutes(app, database, store, storage)

	app.Get("manifest.webmanifest", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("manifest.webmanifest")
		if err != nil {
			middleware.GetLogger(c).WarnContext(c.UserContext(), "failed to open manifest.webmanifest", slog.String("error", err.Error()))
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		c.Type("application/manifest+json")
		return c.SendStream(f)
	})

	app.Get("apple-touch-icon.png", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("img/apple-touch-icon.png")
		if err != nil {
			middleware.GetLogger(c).WarnContext(c.UserContext(), "failed to open apple-touch-icon.png", slog.String("error", err.Error()))
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
			middleware.GetLogger(c).WarnContext(c.UserContext(), "failed to open favicon.ico", slog.String("error", err.Error()))
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
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
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

func registerRoutes(app *fiber.App, db *sql.DB, sessionStore *session.Store, paginationStore planningcenterv1.PaginationStore) {
	app.Get("/", func(c *fiber.Ctx) error {
		f, err := static.EmbeddedFS.Open("pages/home/index.html")
		if err != nil {
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		c.Type("html")
		return c.SendStream(f)
	})

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
