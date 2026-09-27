package login

import (
	"fmt"
	"kids-checkin/internal/web/static"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

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
		return c.Status(http.StatusInternalServerError).SendString("Could not create user session")
	}

	if redirectTo != "" {
		// Reject anything that would send the browser off this origin. A plain
		// "/" prefix check is not enough: "//evil.example.com" passes it, and
		// browsers read a protocol-relative Location as an absolute URL. They
		// also fold a backslash into a slash, so "/\evil.example.com" is the
		// same attack wearing a different character.
		if !isSameOriginPath(redirectTo) {
			redirectTo = "/"
		}
		return c.Redirect(redirectTo)
	}

	return c.Redirect("/")
}

// isSameOriginPath reports whether target is a path on this origin that is
// safe to redirect to. It must start with a single "/" followed by neither a
// slash nor a backslash, which rules out absolute URLs, protocol-relative
// URLs, and the backslash variants browsers normalize to "//".
func isSameOriginPath(target string) bool {
	if len(target) == 0 || target[0] != '/' {
		return false
	}
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return false
	}
	return true
}

func (controller *Controller) GetLogout(c *fiber.Ctx) error {
	sess, err := controller.sessionStore.Get(c)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("Session error")
	}

	// This destroys the session in SQLite and clears the cookie on the client
	if err := sess.Destroy(); err != nil {
		return c.Status(http.StatusInternalServerError).SendString("Could not log out")
	}

	return c.Redirect("/login")
}
