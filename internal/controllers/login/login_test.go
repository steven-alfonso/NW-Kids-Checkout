package login

import (
	"bytes"
	"encoding/gob"
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
	app.Post("/login", controller.PostLogin)

	return app, store
}

func postLogin(t *testing.T, app *fiber.App, password string) *http.Response {
	t.Helper()

	form := url.Values{
		"username": {testUsername},
		"password": {password},
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "session_id="+fixatedID)

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
