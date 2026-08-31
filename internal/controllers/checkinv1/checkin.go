package checkinv1

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/repo"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/location"
	"kids-checkin/internal/repo/manualcheckin"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

const defaultCheckedOutAfterDelta = -12 * time.Hour

type Controller struct {
	checkinRepo  checkin.Repo
	manualRepo   manualcheckin.Repo
	locationRepo location.Repo
	sessionStore session.Storer
	wsClients    map[*websocket.Conn]*wsClient
}

type wsClient struct {
	checkedOutAfterDelta time.Duration
	location             string
}

func NewController(db *sql.DB, sessionStore session.Storer) *Controller {
	return &Controller{
		checkinRepo:  checkin.NewRepo(db),
		manualRepo:   manualcheckin.NewRepo(db),
		locationRepo: location.NewRepo(db),
		sessionStore: sessionStore,
		wsClients:    make(map[*websocket.Conn]*wsClient),
	}
}

func (controller *Controller) RegisterRoutes(app *fiber.App) {
	// Setup
	checkinGroup := app.Group("/v1/checkins")
	checkinGroup.Use(middleware.AuthRequired(controller.sessionStore, ""))

	checkinGroup.Get("/checkouts", controller.Checkouts)
	checkinGroup.Patch("/:planning_center_id/checked_out_confirmed", controller.PatchCheckedOutConfirmed)
}

func (controller *Controller) Checkouts(c *fiber.Ctx) error {
	sess, err := controller.sessionStore.Get(c)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not fetch session")
	}

	c.Locals("allowed", sess.Get("allowed"))
	if websocket.IsWebSocketUpgrade(c) {
		return controller.checkoutsWebsocket(c)
	}

	return controller.checkoutsWeb(c)
}

func (controller *Controller) checkoutsWeb(c *fiber.Ctx) error {
	accepts := c.Accepts(fiber.MIMEApplicationJSON, fiber.MIMETextHTML)
	if accepts == "" {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported media type")
	}

	if accepts == fiber.MIMETextHTML {
		f, err := static.EmbeddedFS.Open("pages/checkoutsv1/checkouts.html")
		if err != nil {
			return fiber.ErrInternalServerError
		}
		defer f.Close()

		var htmlStream io.Reader = f
		if static.IsDev() {
			content, readErr := io.ReadAll(f)
			if readErr != nil {
				return fiber.ErrInternalServerError
			}
			htmlStream = bytes.NewReader([]byte(strings.Replace(
				string(content),
				"</body>",
				`<script src="/static/dev/preview.js"></script></body>`,
				1,
			)))
		}

		c.Type("html")
		return c.SendStream(htmlStream)
	}

	filter, err := buildFilter(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	if filter.CheckedOutAtAfter.IsZero() {
		filter.CheckedOutAtAfter = time.Now().Add(defaultCheckedOutAfterDelta)
	}

	filter.Recent = true
	manualFilter := manualFilterFromCheckinFilter(filter)
	if manualFilter.CheckedOutAtAfter.IsZero() {
		manualFilter.CheckedOutAtAfter = time.Now().Add(defaultCheckedOutAfterDelta)
	}
	manualFilter.Recent = true

	checkins, err := controller.checkinRepo.ListCheckins(c.Context(), filter)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	// Manual checkins have no location, so they can't match a location-group
	// filter; skip them to avoid over-fetching rows the client discards.
	var manualCheckins []manualcheckin.ManualCheckin
	if len(filter.LocationGroupIDs) == 0 && filter.LocationGroupName == "" {
		manualCheckins, err = controller.manualRepo.ListManualCheckins(c.Context(), manualFilter)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		manualCheckins = sortManualCheckins(manualCheckins)
	}

	checkins = sortCheckins(checkins)

	_, err = controller.locationRepo.ListLocations(c.Context(), location.LocationFilter{})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(CheckoutsResponse{
		Checkins:       repoCheckinSliceToOutput(checkins),
		ManualCheckins: repoManualCheckinSliceToOutput(manualCheckins),
	})
}

func (controller *Controller) checkoutsWebsocket(c *fiber.Ctx) error {
	log := middleware.GetLogger(c)

	return websocket.New(func(webscocketConn *websocket.Conn) {
		defer webscocketConn.Close()
		controller.wsClients[webscocketConn] = &wsClient{
			checkedOutAfterDelta: defaultCheckedOutAfterDelta,
		}
		// c.Locals is added to the *websocket.Conn
		slog.InfoContext(
			c.Context(),
			"client connected to websocket",
			slog.String("allowed", fmt.Sprintf("%v", webscocketConn.Locals("allowed"))),
			slog.String("id", webscocketConn.Params("id")),
			slog.String("v", webscocketConn.Query("v")),
			slog.String("session", webscocketConn.Cookies("session")),
		)

		// websocket.Conn bindings https://pkg.go.dev/github.com/fasthttp/websocket?tab=doc#pkg-index
		var (
			mt  int
			msg []byte
			err error
		)

		for {
			if mt, msg, err = webscocketConn.ReadMessage(); err != nil {
				log.WarnContext(c.Context(), "error reading from websocket", slog.String("error", err.Error()), slog.Any("err", err))
				delete(controller.wsClients, webscocketConn)
				break
			}
			filter := CheckinFilter{}
			err = json.Unmarshal(msg, &filter)
			if err != nil {
				log.WarnContext(c.Context(), "cannot unmarshal filter", slog.String("error", err.Error()), slog.Any("err", err))
				continue
			}

			log.InfoContext(c.Context(), "recv filter", slog.String("filter", fmt.Sprintf("%+v", filter)))

			checkins, err := controller.checkinRepo.ListCheckins(context.Background(), checkin.Filter{
				LocationName:      controller.wsClients[webscocketConn].location,
				CheckedOutAtAfter: time.Now().Add(controller.wsClients[webscocketConn].checkedOutAfterDelta),
			})
			if err != nil {
				log.ErrorContext(c.Context(), "cannot list checkins", slog.String("error", err.Error()), slog.Any("err", err))
				continue
			}

			manualCheckins, err := controller.manualRepo.ListManualCheckins(context.Background(), manualcheckin.Filter{
				CheckedOutAtAfter: time.Now().Add(controller.wsClients[webscocketConn].checkedOutAfterDelta),
				Recent:            true,
			})
			if err != nil {
				log.ErrorContext(c.Context(), "cannot list manual checkins", slog.String("error", err.Error()), slog.Any("err", err))
				continue
			}

			checkins = sortCheckins(checkins)
			manualCheckins = sortManualCheckins(manualCheckins)
			msg, err = json.Marshal(CheckoutsResponse{
				Checkins:       repoCheckinSliceToOutput(checkins),
				ManualCheckins: repoManualCheckinSliceToOutput(manualCheckins),
			})
			if err != nil {
				log.ErrorContext(c.Context(), "cannot marshal checkins", slog.String("error", err.Error()), slog.Any("err", err))
				continue
			}

			if err = webscocketConn.WriteMessage(mt, msg); err != nil {
				log.WarnContext(c.Context(), "cannot write to websocket", slog.String("error", err.Error()))
				webscocketConn.Close()

				delete(controller.wsClients, webscocketConn)

				break
			}
		}
	})(c)
}

func (controller *Controller) PatchCheckedOutConfirmed(c *fiber.Ctx) error {
	planningCenterID := c.Params("planning_center_id")
	if planningCenterID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "planning_center_id is required")
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

	updated, err := controller.checkinRepo.SetCheckedOutConfirmedAt(c.Context(), planningCenterID, confirmed)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "checkin not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(repoCheckinToOutput(updated))
}

func buildFilter(c *fiber.Ctx) (checkin.Filter, error) {
	locationGroupName := c.Query("location_group_name", "")
	var err error

	if locationGroupName != "" {
		locationGroupName, err = url.QueryUnescape(locationGroupName)
		if err != nil {
			return checkin.Filter{}, errors.New("cannot parse location_group_name")
		}
	}

	filter := checkin.Filter{
		LocationGroupName: locationGroupName,
		PlanningCenterID:  c.Query("planning_center_id"),
		FirstName:         c.Query("first_name"),
		LastName:          c.Query("last_name"),
	}

	if idStr := c.Query("id"); idStr != "" {
		filter.ID, err = strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return checkin.Filter{}, errors.New("cannot parse id")
		}
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		limitInt, err := strconv.Atoi(limitStr)
		if err != nil {
			return checkin.Filter{}, errors.New("cannot parse limit")
		}
		if limitInt < 0 {
			return checkin.Filter{}, errors.New("limit must be positive")
		}
		filter.Limit = limitInt
	}

	if inc := c.Query("include_unassigned"); inc == "1" || inc == "true" {
		filter.IncludeUnassigned = true
	}

	// parse repeated/comma location_group_id
	ids, err := parseLocationGroupIDs(c)
	if err != nil {
		return checkin.Filter{}, err
	}
	if len(ids) == 1 {
		filter.LocationGroupID = ids[0]
	}
	filter.LocationGroupIDs = ids

	if cobStr := c.Query("checked_out_before"); cobStr != "" {
		// try time.ParseDuration
		ago, err := time.ParseDuration(cobStr)
		if err == nil {
			filter.CheckedOutAtBefore = time.Now().Add(ago)
		} else {
			filter.CheckedOutAtBefore, err = time.ParseInLocation(time.RFC3339, cobStr, time.UTC)
			if err != nil {
				return checkin.Filter{}, errors.New("cannot parse checked_out_before")
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
				return checkin.Filter{}, errors.New("cannot parse checked_out_after")
			}
		}
	}

	if !filter.CheckedOutAtAfter.IsZero() && !filter.CheckedOutAtBefore.IsZero() && filter.CheckedOutAtAfter.After(filter.CheckedOutAtBefore) {
		return checkin.Filter{}, errors.New("checked_out_after must be before checked_out_before")
	}

	return filter, nil
}

func parseLocationGroupIDs(c *fiber.Ctx) ([]int64, error) {
	var ids []int64
	var parseErr error
	c.Request().URI().QueryArgs().VisitAll(func(key, value []byte) {
		if parseErr != nil || string(key) != "location_group_id" {
			return
		}
		for _, part := range strings.Split(string(value), ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			parsed, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				parseErr = errors.New("cannot parse location_group_id")
				return
			}
			if parsed < 0 {
				parseErr = errors.New("location_group_id must be positive")
				return
			}
			if parsed > 0 {
				ids = append(ids, parsed)
			}
		}
	})
	return ids, parseErr
}

func repoCheckinToOutput(checkin checkin.Checkin) Checkin {
	var coa *time.Time
	if !checkin.CheckedOutAt.IsZero() {
		coa = &checkin.CheckedOutAt
	}
	var coc *time.Time
	if !checkin.CheckedOutConfirmedAt.IsZero() {
		coc = &checkin.CheckedOutConfirmedAt
	}
	return Checkin{
		PlanningCenterID:      checkin.PlanningCenterID,
		LocationID:            checkin.LocationID,
		LocationGroupID:       checkin.LocationGroupID,
		FirstName:             checkin.FirstName,
		LastName:              checkin.LastName,
		SecurityCode:          checkin.SecurityCode,
		CheckedOutAt:          coa,
		CheckedOutConfirmedAt: coc,
		Source:                "planning_center",
	}
}

func repoCheckinSliceToOutput(checkins []checkin.Checkin) []Checkin {
	output := make([]Checkin, len(checkins))
	for i := range checkins {
		output[i] = repoCheckinToOutput(checkins[i])
	}
	return output
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
	return Checkin{
		PublicID:              manualCheckin.PublicID,
		FirstName:             manualCheckin.FirstName,
		LastName:              manualCheckin.LastName,
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

func sortCheckins(checkins []checkin.Checkin) []checkin.Checkin {
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

func manualFilterFromCheckinFilter(filter checkin.Filter) manualcheckin.Filter {
	return manualcheckin.Filter{
		FirstName:          filter.FirstName,
		LastName:           filter.LastName,
		CheckedOutAtBefore: filter.CheckedOutAtBefore,
		CheckedOutAtAfter:  filter.CheckedOutAtAfter,
		Limit:              filter.Limit,
	}
}

type Checkin struct {
	PlanningCenterID      string     `json:"planning_center_id"`
	LocationID            int64      `json:"location_id"`
	LocationGroupID       *int64     `json:"location_group_id"`
	PublicID              string     `json:"public_id"`
	FirstName             string     `json:"first_name"`
	LastName              string     `json:"last_name"`
	SecurityCode          string     `json:"security_code"`
	CheckedOutAt          *time.Time `json:"checked_out_at"`
	CheckedOutConfirmedAt *time.Time `json:"checked_out_confirmed_at"`
	Source                string     `json:"source"`
}

type CheckoutsResponse struct {
	Checkins       []Checkin `json:"checkins"`
	ManualCheckins []Checkin `json:"manual_checkins"`
}

type CheckinFilter struct {
	Location         string    `json:"location"`
	CheckedOutBefore time.Time `json:"checked_out_before"`
	CheckedOutAfter  time.Time `json:"checked_out_after"`
}
