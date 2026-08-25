package manualcheckinv1

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"mime"
	"sort"
	"strconv"
	"time"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/repo"
	"kids-checkin/internal/repo/manualcheckin"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
)

const defaultCheckedOutAfterDelta = -12 * time.Hour

type Controller struct {
	manualRepo   manualcheckin.Repo
	sessionStore session.Storer
}

func NewController(db *sql.DB, sessionStore session.Storer) *Controller {
	return &Controller{
		manualRepo:   manualcheckin.NewRepo(db),
		sessionStore: sessionStore,
	}
}

func (controller *Controller) RegisterRoutes(app *fiber.App) {
	manualGroup := app.Group("/v1/checkins")
	manualGroup.Use(middleware.AuthRequired(controller.sessionStore, ""))

	manualGroup.Get("/manual-checkins", controller.GetManualCheckins)
	manualGroup.Patch("/manual-checkins/:public_id/checked_out", controller.PatchManualCheckedOut)
	manualGroup.Patch("/manual-checkins/:public_id/checked_out_confirmed", controller.PatchManualCheckedOutConfirmed)

	app.Get("/manual-checkins", middleware.AuthRequired(controller.sessionStore, ""), controller.ManualCheckinsPage)
}

func (controller *Controller) GetManualCheckins(c *fiber.Ctx) error {
	filter, err := buildManualFilter(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	if filter.CheckedOutAtAfter.IsZero() {
		filter.CheckedOutAtAfter = time.Now().Add(defaultCheckedOutAfterDelta)
	}

	filter.Recent = true

	manualCheckins, err := controller.manualRepo.ListManualCheckins(c.Context(), filter)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if c.Query("sort") == "created" {
		manualCheckins = sortManualCheckinsByCreated(manualCheckins)
	} else {
		manualCheckins = sortManualCheckins(manualCheckins)
	}
	return c.JSON(repoManualCheckinSliceToOutput(manualCheckins))
}

func (controller *Controller) PatchManualCheckedOut(c *fiber.Ctx) error {
	publicID := c.Params("public_id")
	if publicID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "public_id is required")
	}

	contentType := c.Get("Content-Type")
	if contentType == "" {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported media type")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != fiber.MIMEApplicationJSON {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported media type")
	}

	type checkedOutPayload struct {
		CheckedOut *bool `json:"checked_out"`
	}

	var payload checkedOutPayload
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	if payload.CheckedOut == nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	checkedOut := *payload.CheckedOut

	manualCheckins, err := controller.manualRepo.ListManualCheckins(c.Context(), manualcheckin.Filter{
		PublicID: publicID,
		Limit:    1,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if len(manualCheckins) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "manual checkin not found")
	}

	updated, err := controller.manualRepo.SetManualCheckedOutAt(c.Context(), manualCheckins[0].ID, checkedOut)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "manual checkin not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(repoManualCheckinToOutput(updated))
}

func (controller *Controller) PatchManualCheckedOutConfirmed(c *fiber.Ctx) error {
	publicID := c.Params("public_id")
	if publicID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "public_id is required")
	}

	contentType := c.Get("Content-Type")
	if contentType == "" {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported media type")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != fiber.MIMEApplicationJSON {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported media type")
	}

	type confirmedPayload struct {
		Confirmed *bool `json:"confirmed"`
	}

	var payload confirmedPayload
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	if payload.Confirmed == nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	confirmed := *payload.Confirmed

	manualCheckins, err := controller.manualRepo.ListManualCheckins(c.Context(), manualcheckin.Filter{
		PublicID: publicID,
		Limit:    1,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if len(manualCheckins) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "manual checkin not found")
	}

	updated, err := controller.manualRepo.SetManualCheckedOutConfirmedAt(c.Context(), manualCheckins[0].ID, confirmed)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "manual checkin not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(repoManualCheckinToOutput(updated))
}

func (controller *Controller) ManualCheckinsPage(c *fiber.Ctx) error {
	f, err := static.EmbeddedFS.Open("pages/manual-checkins/index.html")
	if err != nil {
		return fiber.ErrInternalServerError
	}
	defer f.Close()

	c.Type("html")
	return c.SendStream(f)
}

func repoManualCheckinToOutput(manualCheckin manualcheckin.ManualCheckin) Checkin {
	var coa *time.Time
	if !manualCheckin.CheckedOutAt.IsZero() {
		coa = &manualCheckin.CheckedOutAt
	}
	var coc *time.Time
	if !manualCheckin.CheckedOutConfirmedAt.IsZero() {
		coc = &manualCheckin.CheckedOutConfirmedAt
	}
	var ca *time.Time
	if !manualCheckin.CreatedAt.IsZero() {
		ca = &manualCheckin.CreatedAt
	}
	return Checkin{
		PublicID:              manualCheckin.PublicID,
		FirstName:             manualCheckin.FirstName,
		LastName:              manualCheckin.LastName,
		CreatedAt:             ca,
		CheckedOutAt:          coa,
		CheckedOutConfirmedAt: coc,
		Source:                "manual",
	}
}

func repoManualCheckinSliceToOutput(checkins []manualcheckin.ManualCheckin) []Checkin {
	output := make([]Checkin, len(checkins))
	for i := range checkins {
		output[i] = repoManualCheckinToOutput(checkins[i])
	}
	return output
}

func sortManualCheckins(checkins []manualcheckin.ManualCheckin) []manualcheckin.ManualCheckin {
	sort.Slice(checkins, func(i, j int) bool {
		if !checkins[i].CheckedOutAt.Equal(checkins[j].CheckedOutAt) {
			return checkins[i].CheckedOutAt.After(checkins[j].CheckedOutAt)
		}

		if checkins[i].LastName != checkins[j].LastName {
			return checkins[i].LastName < checkins[j].LastName
		}

		return checkins[i].FirstName < checkins[j].FirstName
	})
	return checkins
}

func sortManualCheckinsByCreated(checkins []manualcheckin.ManualCheckin) []manualcheckin.ManualCheckin {
	sort.Slice(checkins, func(i, j int) bool {
		if checkins[i].ID != checkins[j].ID {
			return checkins[i].ID > checkins[j].ID
		}
		if checkins[i].LastName != checkins[j].LastName {
			return checkins[i].LastName < checkins[j].LastName
		}
		return checkins[i].FirstName < checkins[j].FirstName
	})
	return checkins
}

func buildManualFilter(c *fiber.Ctx) (manualcheckin.Filter, error) {
	filter := manualcheckin.Filter{
		FirstName: c.Query("first_name"),
		LastName:  c.Query("last_name"),
	}

	if includeUnchecked := c.Query("include_unchecked"); includeUnchecked != "" {
		include, err := strconv.ParseBool(includeUnchecked)
		if err != nil {
			return manualcheckin.Filter{}, errors.New("cannot parse include_unchecked")
		}
		filter.IncludeUnchecked = include
	}

	if idStr := c.Query("id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return manualcheckin.Filter{}, errors.New("cannot parse id")
		}
		if id < 0 {
			return manualcheckin.Filter{}, errors.New("id must be positive")
		}
		filter.ID = id
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		limitInt, err := strconv.Atoi(limitStr)
		if err != nil {
			return manualcheckin.Filter{}, errors.New("cannot parse limit")
		}
		if limitInt < 0 {
			return manualcheckin.Filter{}, errors.New("limit must be positive")
		}
		filter.Limit = limitInt
	}

	if cobStr := c.Query("checked_out_before"); cobStr != "" {
		ago, err := time.ParseDuration(cobStr)
		if err == nil {
			filter.CheckedOutAtBefore = time.Now().Add(ago)
		} else {
			filter.CheckedOutAtBefore, err = time.ParseInLocation(time.RFC3339, cobStr, time.UTC)
			if err != nil {
				return manualcheckin.Filter{}, errors.New("cannot parse checked_out_before")
			}
		}
	}

	if coaStr := c.Query("checked_out_after"); coaStr != "" {
		ago, err := time.ParseDuration(coaStr)
		if err == nil {
			filter.CheckedOutAtAfter = time.Now().Add(ago)
		} else {
			filter.CheckedOutAtAfter, err = time.ParseInLocation(time.RFC3339, coaStr, time.UTC)
			if err != nil {
				return manualcheckin.Filter{}, errors.New("cannot parse checked_out_after")
			}
		}
	}

	if !filter.CheckedOutAtAfter.IsZero() && !filter.CheckedOutAtBefore.IsZero() && filter.CheckedOutAtAfter.After(filter.CheckedOutAtBefore) {
		return manualcheckin.Filter{}, errors.New("checked_out_after must be before checked_out_before")
	}

	return filter, nil
}

type Checkin struct {
	PlanningCenterID      string     `json:"planning_center_id"`
	LocationID            int64      `json:"location_id"`
	PublicID              string     `json:"public_id"`
	FirstName             string     `json:"first_name"`
	LastName              string     `json:"last_name"`
	SecurityCode          string     `json:"security_code"`
	CreatedAt             *time.Time `json:"created_at"`
	CheckedOutAt          *time.Time `json:"checked_out_at"`
	CheckedOutConfirmedAt *time.Time `json:"checked_out_confirmed_at"`
	Source                string     `json:"source"`
}
