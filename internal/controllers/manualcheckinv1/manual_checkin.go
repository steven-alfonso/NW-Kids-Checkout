package manualcheckinv1

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"sort"
	"strconv"
	"strings"
	"time"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/repo"
	"kids-checkin/internal/repo/manualcheckin"
	"kids-checkin/internal/web/menu"
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
	// Use per-route auth rather than group middleware, so no prefix catches an
	// unlisted route.
	authRequired := middleware.AuthRequired(controller.sessionStore, "")
	app.Get("/v1/checkins/manual-checkins", authRequired, controller.GetManualCheckins)
	app.Post("/v1/checkins/manual-checkins", authRequired, controller.PostManualCheckin)
	app.Patch("/v1/checkins/manual-checkins/:public_id/checked_out", authRequired, controller.PatchManualCheckedOut)
	app.Patch("/v1/checkins/manual-checkins/:public_id/checked_out_confirmed", authRequired, controller.PatchManualCheckedOutConfirmed)

	app.Get("/manual-checkins", authRequired, controller.ManualCheckinsPage)
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
	return sendManualCheckinsJSON(c, repoManualCheckinSliceToOutput(manualCheckins))
}

// sendManualCheckinsJSON writes the list with a validator, so a client that
// sends If-None-Match can revalidate instead of re-downloading an unchanged
// list. It mirrors the checkouts poll.
func sendManualCheckinsJSON(c *fiber.Ctx, payload []Checkin) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not encode manual check-ins")
	}

	// Weak validator: the identity representation is hashed here, but gzip may
	// content-code it on the way out, and a weak tag is the correct validator
	// when a weak transformation like compression is applied (RFC 9110 8.8.1).
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:16]) + `"`

	// private, no-cache: the body carries children's names and check-in and
	// check-out times, so only the user's browser may store it, and it must
	// revalidate on every request. Logout sends Clear-Site-Data: "cache" to
	// evict the stored body. If these devices ever become shared, switch to
	// no-store and accept a full payload on every request.
	c.Set(fiber.HeaderCacheControl, "private, no-cache")
	// The body varies once compression is applied.
	c.Vary(fiber.HeaderAcceptEncoding)
	c.Set(fiber.HeaderETag, etag)

	if etagMatches(c.Get(fiber.HeaderIfNoneMatch), etag) {
		return c.SendStatus(fiber.StatusNotModified)
	}

	// Set the content type directly rather than via c.Type, which treats its
	// argument as a file extension and would fall back to octet-stream.
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Send(body)
}

// etagMatches reports whether an If-None-Match header covers the current etag.
// The header may carry a list and may use the weak W/ prefix on either side.
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	candidate := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(ifNoneMatch, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.TrimPrefix(part, "W/") == candidate {
			return true
		}
	}
	return false
}

func (controller *Controller) PostManualCheckin(c *fiber.Ctx) error {
	type manualCheckinPayload struct {
		PublicID          string `json:"public_id"`
		FirstName         string `json:"first_name"`
		LastName          string `json:"last_name"`
		ImmediateCheckout bool   `json:"immediate_checkout"`
	}

	var payload manualCheckinPayload
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	if payload.FirstName == "" {
		return fiber.NewError(fiber.StatusBadRequest, "first_name is required")
	}
	if payload.LastName == "" {
		return fiber.NewError(fiber.StatusBadRequest, "last_name is required")
	}

	manualCheckin := manualcheckin.ManualCheckin{
		PublicID:  payload.PublicID,
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
	}
	if payload.ImmediateCheckout {
		manualCheckin.CheckedOutAt = time.Now().UTC()
	}

	created, err := controller.manualRepo.CreateManualCheckin(c.Context(), manualCheckin)
	if err != nil {
		if errors.Is(err, manualcheckin.ErrInvalidManualCheckin) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(repoManualCheckinToOutput(created))
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

	content, err := io.ReadAll(f)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	sess, err := controller.sessionStore.Get(c)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not fetch session")
	}
	authenticated, _ := sess.Get("authenticated").(bool)
	role, _ := sess.Get("role").(string)

	menuHTML, err := menu.RenderHTML(authenticated, role)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	html := strings.Replace(string(content), menu.Placeholder, menuHTML, 1)

	// The page embeds no child data, but it is the authenticated route that loads
	// the PII poll, so a shared tablet must not retain it after logout. This is
	// defense in depth and keeps it consistent with the checkouts page.
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	c.Type("html")
	return c.Send([]byte(html))
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
