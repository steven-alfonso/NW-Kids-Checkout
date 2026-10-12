package locationv1

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"strconv"

	"kids-checkin/internal/repo"
	"kids-checkin/internal/repo/location"

	"github.com/gofiber/fiber/v2"
)

type Controller struct {
	repo         location.Repo
	sessionStore session.Storer
}

func NewController(db *sql.DB, sessionStore session.Storer) *Controller {
	return &Controller{
		repo:         location.NewRepo(db),
		sessionStore: sessionStore,
	}
}

func (controller *Controller) RegisterRoutes(app *fiber.App) {
	locationGroup := app.Group("/v1/locations")
	locationGroup.Use(middleware.AuthRequired(controller.sessionStore, ""))

	locationGroup.Get("", controller.GetListLocations)
	locationGroup.Post("", middleware.AuthRequired(controller.sessionStore, "admin"), controller.PostCreateLocation)
	locationGroup.Patch("/:id", middleware.AuthRequired(controller.sessionStore, "admin"), controller.PatchUpdateLocation)
}

func (controller *Controller) GetListLocations(c *fiber.Ctx) error {
	locations, err := controller.repo.ListLocations(c.UserContext(), location.LocationFilter{
		Name:             c.Query("name"),
		PlanningCenterID: c.Query("planning_center_id"),
	})
	if err != nil {
		return err
	}

	return c.JSON(repoLocationSliceToOutput(locations))
}

func (controller *Controller) PostCreateLocation(c *fiber.Ctx) error {
	a := Location{}
	err := json.Unmarshal(c.Body(), &a)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	if a.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}

	_, err = controller.repo.CreateLocation(c.UserContext(), location.Location{
		Name:             a.Name,
		PlanningCenterID: a.PlanningCenterID,
	})

	if err != nil {
		if errors.Is(err, location.ErrLocationExists) {
			return fiber.NewError(fiber.StatusBadRequest, "location already exists")
		}
		return err
	}

	return nil
}

func (controller *Controller) PatchUpdateLocation(c *fiber.Ctx) error {
	locationID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid location id")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	locations, err := controller.repo.ListLocations(c.UserContext(), location.LocationFilter{IDs: []int64{locationID}})
	if err != nil {
		return err
	}
	if len(locations) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "location not found")
	}

	current := locations[0]

	if raw, ok := payload["location_group_id"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			current.LocationGroupID = nil
		} else {
			var groupID int64
			if err := json.Unmarshal(raw, &groupID); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "invalid location_group_id")
			}
			current.LocationGroupID = &groupID
		}
	}

	if err := controller.repo.UpdateLocation(c.UserContext(), current); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "location not found")
		}
		return err
	}

	return c.JSON(repoLocationToOutput(current))
}

type Location struct {
	ID                     int64   `json:"id"`
	Name                   string  `json:"name"`
	PlanningCenterID       string  `json:"planning_center_id"`
	PlanningCenterParentID *string `json:"planning_center_parent_id"`
	EventID                int64   `json:"event_id"`
	LocationGroupID        *int64  `json:"location_group_id"`
}

func repoLocationToOutput(location location.Location) Location {
	return Location{
		ID:                     location.ID,
		Name:                   location.Name,
		PlanningCenterID:       location.PlanningCenterID,
		PlanningCenterParentID: location.PlanningCenterParentID,
		EventID:                location.EventID,
		LocationGroupID:        location.LocationGroupID,
	}
}

func repoLocationSliceToOutput(locations []location.Location) []Location {
	output := make([]Location, len(locations))
	for i := range locations {
		output[i] = repoLocationToOutput(locations[i])
	}
	return output
}
