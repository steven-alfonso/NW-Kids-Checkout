package login

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
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

// testConnWithRemoteAddr mirrors Fiber's test connection but reports the
// request's RemoteAddr. app.Test hard-codes 0.0.0.0, which would put every
// request into one limiter bucket.
type testConnWithRemoteAddr struct {
	r    bytes.Buffer
	w    bytes.Buffer
	addr net.Addr
}

func (c *testConnWithRemoteAddr) Read(b []byte) (int, error)       { return c.r.Read(b) }
func (c *testConnWithRemoteAddr) Write(b []byte) (int, error)      { return c.w.Write(b) }
func (c *testConnWithRemoteAddr) Close() error                     { return nil }
func (c *testConnWithRemoteAddr) LocalAddr() net.Addr              { return &net.TCPAddr{IP: net.IPv4zero} }
func (c *testConnWithRemoteAddr) RemoteAddr() net.Addr             { return c.addr }
func (c *testConnWithRemoteAddr) SetDeadline(time.Time) error      { return nil }
func (c *testConnWithRemoteAddr) SetReadDeadline(time.Time) error  { return nil }
func (c *testConnWithRemoteAddr) SetWriteDeadline(time.Time) error { return nil }

// testAppRequest serves req through app while preserving its RemoteAddr, so
// the limiter sees the intended connection IP.
func testAppRequest(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()

	if req.Body != http.NoBody && req.Header.Get("Content-Length") == "" {
		req.Header.Set("Content-Length", strconv.Itoa(int(req.ContentLength)))
	}
	dump, err := httputil.DumpRequest(req, true)
	require.NoError(t, err)

	addr, err := net.ResolveTCPAddr("tcp", req.RemoteAddr)
	require.NoError(t, err)
	conn := &testConnWithRemoteAddr{addr: addr}
	_, err = conn.r.Write(dump)
	require.NoError(t, err)

	app.Handler() // build the route tree before serving the connection
	require.NoError(t, app.Server().ServeConn(conn))

	resp, err := http.ReadResponse(bufio.NewReader(&conn.w), req)
	require.NoError(t, err)
	return resp
}

func postLogin(t *testing.T, app *fiber.App, password string) *http.Response {
	return postLoginFromIP(t, app, password, "")
}

func postLoginFromIP(t *testing.T, app *fiber.App, password, clientIP string) *http.Response {
	return postLoginWithAccept(t, app, password, clientIP, "application/json")
}

func postLoginWithAccept(t *testing.T, app *fiber.App, password, clientIP, accept string) *http.Response {
	t.Helper()

	form := url.Values{
		"username": {testUsername},
		"password": {password},
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", accept)
	req.Header.Set("Cookie", "session_id="+fixatedID)
	if clientIP != "" {
		req.RemoteAddr = clientIP + ":12345"
	}

	return testAppRequest(t, app, req)
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
	for attempt := 1; attempt <= loginAttemptLimit; attempt++ {
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

func TestPostLogin_rateLimitRedirectsBrowser(t *testing.T) {
	app, _ := newLoginTestApp(t)

	const clientIP = "203.0.113.10"
	for attempt := 1; attempt <= loginAttemptLimit; attempt++ {
		resp := postLoginWithAccept(t, app, "not the password", clientIP, "text/html")
		resp.Body.Close()
		assert.Equal(t, "/login?error=invalid", resp.Header.Get("Location"), "attempt %d should still reach the handler", attempt)
	}

	form := url.Values{
		"username": {testUsername},
		"password": {"not the password"},
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login?next=/manual-checkins", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.RemoteAddr = clientIP + ":12345"

	resp := testAppRequest(t, app, req)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/login?error=ratelimited&next="+url.QueryEscape("/manual-checkins"), resp.Header.Get("Location"))
}

func TestGetLoginShowsRateLimitMessage(t *testing.T) {
	controller := NewController(fibersession.New(fibersession.Config{}))
	app := fiber.New()
	controller.RegisterRoutes(app)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login?error=ratelimited", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Too many login attempts. Please wait a few minutes and try again.")
	assert.Contains(t, string(body), `error === "ratelimited"`)
}

func TestPostLogin_successStillRedirectsUnderLimit(t *testing.T) {
	app, _ := newLoginTestApp(t)

	resp := postLoginFromIP(t, app, testPassword, "198.51.100.20")
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	assert.Equal(t, "/", resp.Header.Get("Location"))
	assert.NotNil(t, findSessionCookie(resp), "a successful login must still set a session cookie")
}

func TestGetLogoutClearsCachedResponses(t *testing.T) {
	app, _ := newLoginTestApp(t)

	login := postLoginFromIP(t, app, testPassword, "198.51.100.30")
	login.Body.Close()
	require.Equal(t, fiber.StatusFound, login.StatusCode)
	cookie := findSessionCookie(login)
	require.NotNil(t, cookie)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logout", nil)
	req.AddCookie(cookie)
	req.RemoteAddr = "198.51.100.30:12345"
	resp := testAppRequest(t, app, req)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusFound, resp.StatusCode)
	assert.Equal(t, "/login", resp.Header.Get("Location"))
	assert.Equal(t, `"cache"`, resp.Header.Get("Clear-Site-Data"),
		"logout must ask the browser to evict bodies cached by older builds")
}
