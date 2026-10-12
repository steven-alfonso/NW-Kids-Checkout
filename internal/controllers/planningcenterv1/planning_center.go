package planningcenterv1

import (
	"kids-checkin/internal/client/planningcenter"
	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"log/slog"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type Controller struct {
	client          planningcenter.Client
	sessionStore    session.Storer
	paginationStore PaginationStore
}

type PaginationStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, exp time.Duration) error
}

const (
	eventCursorPrefix = "planningcenter:events:cursor:"
	eventCursorTTL    = 12 * time.Hour
)

func NewController(sessionStore session.Storer, paginationStore PaginationStore) *Controller {
	return &Controller{
		client:          planningcenter.NewClient(),
		sessionStore:    sessionStore,
		paginationStore: paginationStore,
	}
}

func (controller *Controller) RegisterRoutes(app *fiber.App) {
	planningCenterGroup := app.Group("/v1/admin/planningcenter")
	planningCenterGroup.Use(middleware.AuthRequired(controller.sessionStore, "admin"))

	planningCenterGroup.Get("/events", controller.GetEvents)
	planningCenterGroup.Get("/events/:id/locations", controller.GetLocationsForEvent)
}

func (controller *Controller) GetEvents(c *fiber.Ctx) error {
	log := middleware.GetLogger(c)

	cursor := c.Query("cursor")
	log.InfoContext(c.UserContext(), "fetching planning center events", slog.Bool("has_cursor", cursor != ""))
	var (
		events  []planningcenter.Event
		nextURL string
		err     error
	)
	if cursor != "" {
		storedURL, fetchErr := controller.getNextURL(cursor)
		if fetchErr != nil {
			log.ErrorContext(c.UserContext(), "failed to resolve planning center cursor", slog.String("cursor", cursor), slog.String("error", fetchErr.Error()), slog.Any("err", fetchErr))
			return fetchErr
		}
		events, nextURL, err = controller.client.GetEventsFromNextURL(c.UserContext(), storedURL)
	} else {
		events, nextURL, err = controller.client.GetEvents(c.UserContext())
	}
	if err != nil {
		log.ErrorContext(c.UserContext(), "failed to fetch planning center events", slog.String("error", err.Error()), slog.Any("err", err))
		return err
	}

	responseNext := ""
	if nextURL != "" {
		cursorToken, storeErr := controller.storeNextURL(nextURL)
		if storeErr != nil {
			log.ErrorContext(c.UserContext(), "failed to store planning center cursor", slog.String("error", storeErr.Error()), slog.Any("err", storeErr))
			return storeErr
		}
		responseNext = "/v1/admin/planningcenter/events?cursor=" + url.QueryEscape(cursorToken)
	}

	log.InfoContext(c.UserContext(), "fetched planning center events", slog.Int("events_count", len(events)), slog.Bool("has_next", nextURL != ""))

	return c.JSON(EventListResponse{
		Events: eventsToOutput(events),
		Links: EventLinks{
			Next: responseNext,
		},
	})
}

func (controller *Controller) storeNextURL(nextURL string) (string, error) {
	if nextURL == "" {
		return "", nil
	}

	cursor := uuid.NewString()
	key := eventCursorPrefix + cursor
	if err := controller.paginationStore.Set(key, []byte(nextURL), eventCursorTTL); err != nil {
		return "", err
	}

	return cursor, nil
}

func (controller *Controller) getNextURL(cursor string) (string, error) {
	key := eventCursorPrefix + cursor
	value, err := controller.paginationStore.Get(key)
	if err != nil {
		return "", err
	}
	if len(value) == 0 {
		return "", fiber.NewError(fiber.StatusBadRequest, "invalid cursor")
	}
	return string(value), nil
}

func (controller *Controller) GetLocationsForEvent(c *fiber.Ctx) error {
	log := middleware.GetLogger(c)

	eventID := c.Params("id", "")
	if eventID == "" {
		log.InfoContext(c.UserContext(), "missing planning center event id")
		return fiber.NewError(fiber.StatusBadRequest, "event id is required")
	}

	log.InfoContext(c.UserContext(), "fetching planning center locations", slog.String("event_id", eventID))

	locations, err := controller.client.GetLocationsForEvent(c.UserContext(), eventID)
	if err != nil {
		log.ErrorContext(c.UserContext(), "failed to fetch planning center locations", slog.String("event_id", eventID), slog.String("error", err.Error()), slog.Any("err", err))
		return err
	}

	log.InfoContext(c.UserContext(), "fetched planning center locations", slog.String("event_id", eventID), slog.Int("locations_count", len(locations)))

	return c.JSON(locationsToOutput(locations))
}

type Event struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EventLinks struct {
	Next string `json:"next"`
}

type EventListResponse struct {
	Events []Event    `json:"events"`
	Links  EventLinks `json:"links"`
}

type Location struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
}

func eventsToOutput(events []planningcenter.Event) []Event {
	output := make([]Event, len(events))
	for i, event := range events {
		output[i] = Event{
			ID:   event.ID,
			Name: event.Name,
		}
	}
	return output
}

func locationsToOutput(locations []planningcenter.Location) []Location {
	output := make([]Location, len(locations))
	for i, location := range locations {
		output[i] = Location{
			ID:       location.ID,
			ParentID: location.ParentID,
			Name:     location.Name,
		}
	}
	return output
}
