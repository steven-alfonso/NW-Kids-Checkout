package locationgroupv1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
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
	app.Get("/v1/location_groups", controller.GetListLocationGroups)

	adminGroup := app.Group("/v1/admin/location_groups")
	adminGroup.Use(middleware.AuthRequired(controller.sessionStore, "admin"))
	adminGroup.Post("", controller.PostCreateLocationGroup)
	adminGroup.Patch("/:id", controller.PatchUpdateLocationGroup)
	adminGroup.Delete("/:id", controller.DeleteLocationGroup)
}

type LocationGroupInput struct {
	Name string `json:"name"`
}

func (controller *Controller) PostCreateLocationGroup(c *fiber.Ctx) error {
	var input LocationGroupInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	created, err := controller.repo.CreateLocationGroup(c.UserContext(), location.LocationGroup{Name: input.Name})
	if err != nil {
		return fmt.Errorf("creating location group: %w", err)
	}
	return c.Status(fiber.StatusCreated).JSON(repoLocationGroupToOutput(created))
}

func (controller *Controller) PatchUpdateLocationGroup(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid location group id")
	}
	var input LocationGroupInput
	if err := json.Unmarshal(c.Body(), &input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	if err := controller.repo.UpdateLocationGroup(c.UserContext(), location.LocationGroup{ID: int64(id), Name: input.Name}); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "location group not found")
		}
		return fmt.Errorf("updating location group: %w", err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (controller *Controller) DeleteLocationGroup(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid location group id")
	}
	if err := controller.repo.DeleteLocationGroup(c.UserContext(), int64(id)); err != nil {
		if errors.Is(err, location.ErrLocationGroupInUse) {
			return fiber.NewError(fiber.StatusBadRequest, "location group is in use")
		}
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "location group not found")
		}
		return fmt.Errorf("deleting location group: %w", err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (controller *Controller) GetListLocationGroups(c *fiber.Ctx) error {
	var id int64

	cleanedStr := strings.TrimSpace(c.Params("id", ""))
	if cleanedStr != "" {
		var err error
		id, err = strconv.ParseInt(cleanedStr, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid id")
		}
	}

	locationGroups, err := controller.repo.ListLocationGroups(c.UserContext(), location.LocationGroupFilter{
		ID:   id,
		Name: c.Query("name"),
	})
	if err != nil {
		return err
	}

	return c.JSON(repoLocationGroupSliceToOutput(locationGroups))
}

type LocationGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func repoLocationGroupToOutput(location location.LocationGroup) LocationGroup {
	return LocationGroup{
		ID:   location.ID,
		Name: location.Name,
	}
}

func repoLocationGroupSliceToOutput(locations []location.LocationGroup) []LocationGroup {
	output := make([]LocationGroup, len(locations))
	for i := range locations {
		output[i] = repoLocationGroupToOutput(locations[i])
	}
	return output
}
