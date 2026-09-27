package planningcenterv1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"kids-checkin/internal/client/planningcenter"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryPaginationStore struct {
	mu    sync.Mutex
	items map[string][]byte
}

func newMemoryPaginationStore() *memoryPaginationStore {
	return &memoryPaginationStore{items: make(map[string][]byte)}
}

func (store *memoryPaginationStore) Get(key string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.items[key]
	if !ok {
		return nil, nil
	}
	copyValue := make([]byte, len(value))
	copy(copyValue, value)
	return copyValue, nil
}

func (store *memoryPaginationStore) Set(key string, value []byte, exp time.Duration) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	copyValue := make([]byte, len(value))
	copy(copyValue, value)
	store.items[key] = copyValue
	return nil
}

func setupAuthedApp() (*fiber.App, *session.Store) {
	app := fiber.New()
	store := session.New()
	app.Use(func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", "admin")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	return app, store
}

func TestController_GetEvents_WithCursor(t *testing.T) {
	app, store := setupAuthedApp()
	paginationStore := newMemoryPaginationStore()

	controller := NewController(store, paginationStore)
	mockClient := &planningcenter.MockClient{}
	controller.client = mockClient
	controller.RegisterRoutes(app)

	cursor := "cursor-123"
	storedURL := "https://planningcenter.example.com/next"
	key := eventCursorPrefix + cursor
	require.NoError(t, paginationStore.Set(key, []byte(storedURL), eventCursorTTL))

	mockClient.GetEventsFromNextURLFunc = func(ctx context.Context, nextURL string) ([]planningcenter.Event, string, error) {
		require.Equal(t, storedURL, nextURL)
		return []planningcenter.Event{{ID: "evt-2", Name: "Second"}}, "", nil
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/planningcenter/events?cursor="+cursor, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload EventListResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload.Events, 1)
	assert.Equal(t, "evt-2", payload.Events[0].ID)
	assert.Equal(t, "Second", payload.Events[0].Name)
	assert.Empty(t, payload.Links.Next)
	assert.Equal(t, uint64(1), mockClient.GetEventsFromNextURLFuncCallCount.Load())
}

func TestController_GetEvents_NewCursor(t *testing.T) {
	app, store := setupAuthedApp()
	paginationStore := newMemoryPaginationStore()

	controller := NewController(store, paginationStore)
	mockClient := &planningcenter.MockClient{}
	controller.client = mockClient
	controller.RegisterRoutes(app)

	nextURL := "https://planningcenter.example.com/next-page"
	mockClient.GetEventsFunc = func(ctx context.Context) ([]planningcenter.Event, string, error) {
		return []planningcenter.Event{{ID: "evt-1", Name: "First"}}, nextURL, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/planningcenter/events", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload EventListResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload.Events, 1)
	assert.Equal(t, "evt-1", payload.Events[0].ID)
	assert.Equal(t, "First", payload.Events[0].Name)
	assert.NotEmpty(t, payload.Links.Next)

	parsed, parseErr := url.Parse(payload.Links.Next)
	require.NoError(t, parseErr)
	assert.Equal(t, "/v1/admin/planningcenter/events", parsed.Path)
	queryCursor := parsed.Query().Get("cursor")
	require.NotEmpty(t, queryCursor)
	stored, getErr := paginationStore.Get(eventCursorPrefix + queryCursor)
	require.NoError(t, getErr)
	assert.Equal(t, nextURL, string(stored))
	assert.Equal(t, uint64(1), mockClient.GetEventsFuncCallCount.Load())
}

func TestController_GetEvents_InvalidCursor(t *testing.T) {
	app, store := setupAuthedApp()
	paginationStore := newMemoryPaginationStore()

	controller := NewController(store, paginationStore)
	controller.client = &planningcenter.MockClient{}
	controller.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/planningcenter/events?cursor=missing", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestController_GetLocationsForEvent(t *testing.T) {
	app, store := setupAuthedApp()
	paginationStore := newMemoryPaginationStore()

	controller := NewController(store, paginationStore)
	mockClient := &planningcenter.MockClient{}
	controller.client = mockClient
	controller.RegisterRoutes(app)

	parentID := "parent-1"
	mockClient.GetLocationsForEventFunc = func(ctx context.Context, eventID string) ([]planningcenter.Location, error) {
		require.Equal(t, "evt-9", eventID)
		return []planningcenter.Location{
			{ID: "loc-1", ParentID: &parentID, Name: "Room A"},
			{ID: "loc-2", Name: "Room B"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/planningcenter/events/evt-9/locations", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload []Location
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload, 2)
	assert.Equal(t, "loc-1", payload[0].ID)
	assert.NotNil(t, payload[0].ParentID)
	assert.Equal(t, parentID, *payload[0].ParentID)
	assert.Equal(t, "Room A", payload[0].Name)
	assert.Equal(t, "loc-2", payload[1].ID)
	assert.Nil(t, payload[1].ParentID)
	assert.Equal(t, "Room B", payload[1].Name)
}
