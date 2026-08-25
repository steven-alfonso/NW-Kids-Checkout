package login

import (
	"fmt"
	"kids-checkin/internal/web/static"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/repo/location"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

type Controller struct {
	repo         location.Repo
	sessionStore session.Storer
}

func NewController(sessionStore session.Storer) *Controller {
	return &Controller{
		sessionStore: sessionStore,
	}
}

func (controller *Controller) RegisterRoutes(app *fiber.App) {
	app.Get("/login", controller.GetLogin)
	app.Post("/login", controller.PostLogin)
	app.Get("/logout", controller.GetLogout)
	slog.Info("registering login routes")
}

func (controller *Controller) GetLogin(c *fiber.Ctx) error {
	f, err := static.EmbeddedFS.Open("pages/login/index.html")
	if err != nil {
		return fiber.ErrInternalServerError
	}
	defer f.Close()

	c.Type("html")
	return c.SendStream(f)
}

func (controller *Controller) PostLogin(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	var role string

	passwordHash := os.Getenv(fmt.Sprintf("LOGIN_PASSWORD_%s", strings.ToUpper(username)))

	redirectTo := c.Query("next")

	// 2. Compare Bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		middleware.GetLogger(c).WarnContext(c.UserContext(), "login failed: invalid credentials", slog.String("username", username))
		if redirectTo != "" {
			return c.Redirect(fmt.Sprintf("/login?error=invalid&next=%s", url.QueryEscape(redirectTo)), http.StatusSeeOther)
		}
		return c.Redirect("/login?error=invalid", http.StatusSeeOther)
	}

	if username == "admin" {
		role = "admin"
	}

	// 3. Create Session
	sess, _ := controller.sessionStore.Get(c)
	sess.Set("authenticated", true)
	sess.Set("username", username)
	sess.Set("role", role)
	err := sess.Save()
	if err != nil {
		middleware.GetLogger(c).ErrorContext(c.UserContext(), "login failed: could not save session", slog.String("username", username), slog.String("error", err.Error()))
		return c.Status(http.StatusInternalServerError).SendString("Could not create user session")
	}

	if redirectTo != "" {
		if !strings.HasPrefix(redirectTo, "/") {
			redirectTo = "/"
		}
		return c.Redirect(redirectTo)
	}

	return c.Redirect("/")
}

func (controller *Controller) GetLogout(c *fiber.Ctx) error {
	sess, err := controller.sessionStore.Get(c)
	if err != nil {
		middleware.GetLogger(c).ErrorContext(c.UserContext(), "logout failed: could not load session", slog.String("error", err.Error()))
		return c.Status(http.StatusInternalServerError).SendString("Session error")
	}

	// This destroys the session in SQLite and clears the cookie on the client
	if err := sess.Destroy(); err != nil {
		middleware.GetLogger(c).ErrorContext(c.UserContext(), "logout failed: could not destroy session", slog.String("error", err.Error()))
		return c.Status(http.StatusInternalServerError).SendString("Could not log out")
	}

	return c.Redirect("/login")
}
