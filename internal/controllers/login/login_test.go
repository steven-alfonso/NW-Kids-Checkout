package login

import (
	"bytes"
	"encoding/gob"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	fibersession "github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const (
	testUsername = "user"
	testPassword = "correct horse battery staple"
	fixatedID    = "fixated"
)

// newLoginTestApp builds an app around PostLogin with a real session store.
// The fixated ID is seeded in storage so it is a real session, as it would be
// in a fixation attack. Without that, a login that merely mints an ID because
// the supplied one was unknown would look safe even when it never regenerates.
func newLoginTestApp(t *testing.T) (*fiber.App, *fibersession.Store) {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err)
	t.Setenv("LOGIN_PASSWORD_USER", string(hash))

	store := fibersession.New(fibersession.Config{})

	var encoded bytes.Buffer
	require.NoError(t, gob.NewEncoder(&encoded).Encode(map[string]any{}))
	require.NoError(t, store.Storage.Set(fixatedID, encoded.Bytes(), time.Hour))

	controller := NewController(store)

	app := fiber.New()
	controller.RegisterRoutes(app)

	return app, store
}

func postLogin(t *testing.T, app *fiber.App, password string) *http.Response {
	return postLoginFromIP(t, app, password, "")
}

func postLoginFromIP(t *testing.T, app *fiber.App, password, clientIP string) *http.Response {
	t.Helper()

	form := url.Values{
		"username": {testUsername},
		"password": {password},
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "session_id="+fixatedID)
	if clientIP != "" {
		req.Header.Set("CF-Connecting-IP", clientIP)
	}

	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func findSessionCookie(resp *http.Response) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_id" {
			return cookie
		}
	}
	return nil
}

func TestGetLoginDoesNotListUsernames(t *testing.T) {
	controller := NewController(fibersession.New(fibersession.Config{}))

	app := fiber.New()
	app.Get("/login", controller.GetLogin)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `name="username"`)
	assert.NotContains(t, string(body), "<select")
	assert.NotContains(t, string(body), `value="admin"`)
}

func TestPostLogin_regeneratesSessionID(t *testing.T) {
	app, store := newLoginTestApp(t)

	resp := postLogin(t, app, testPassword)
	defer resp.Body.Close()

	// PostLogin's success path calls c.Redirect("/") without a status, which
	// Fiber answers with 302. Only the failure path asks for 303.
	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	assert.Equal(t, "/", resp.Header.Get("Location"))

	cookie := findSessionCookie(resp)
	require.NotNil(t, cookie, "login must set a session cookie")
	assert.NotEqual(t, fixatedID, cookie.Value)

	raw, err := store.Storage.Get(cookie.Value)
	require.NoError(t, err)
	require.NotNil(t, raw, "the new session must be stored")

	var data map[string]any
	require.NoError(t, gob.NewDecoder(bytes.NewReader(raw)).Decode(&data))
	assert.Equal(t, true, data["authenticated"])
	assert.Equal(t, testUsername, data["username"])

	// Regeneration also deletes the attacker-supplied session from storage.
	old, err := store.Storage.Get(fixatedID)
	require.NoError(t, err)
	assert.Nil(t, old)
}

func TestPostLogin_wrongPasswordDoesNotCreateAuthenticatedSession(t *testing.T) {
	app, store := newLoginTestApp(t)

	resp := postLogin(t, app, "not the password")
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/login?error=invalid", resp.Header.Get("Location"))

	assert.Nil(t, findSessionCookie(resp), "a failed login must not issue a session cookie")

	raw, err := store.Storage.Get(fixatedID)
	require.NoError(t, err)
	require.NotNil(t, raw, "the seeded session must remain")

	var data map[string]any
	require.NoError(t, gob.NewDecoder(bytes.NewReader(raw)).Decode(&data))
	assert.NotContains(t, data, "authenticated", "wrong password must not authenticate the session")
}

func TestPostLogin_rateLimitsPerClient(t *testing.T) {
	app, _ := newLoginTestApp(t)

	const clientIP = "198.51.100.10"
	for attempt := 1; attempt <= 20; attempt++ {
		resp := postLoginFromIP(t, app, "not the password", clientIP)
		resp.Body.Close()
		assert.NotEqual(t, fiber.StatusTooManyRequests, resp.StatusCode, "attempt %d should be under the limit", attempt)
	}

	resp := postLoginFromIP(t, app, "not the password", clientIP)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"error":"too many login attempts"}`, string(body))

	other := postLoginFromIP(t, app, "not the password", "198.51.100.11")
	defer other.Body.Close()
	assert.NotEqual(t, fiber.StatusTooManyRequests, other.StatusCode, "a different client key has its own budget")
}

func TestPostLogin_successStillRedirectsUnderLimit(t *testing.T) {
	app, _ := newLoginTestApp(t)

	resp := postLoginFromIP(t, app, testPassword, "198.51.100.20")
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	assert.Equal(t, "/", resp.Header.Get("Location"))
	assert.NotNil(t, findSessionCookie(resp), "a successful login must still set a session cookie")
}
