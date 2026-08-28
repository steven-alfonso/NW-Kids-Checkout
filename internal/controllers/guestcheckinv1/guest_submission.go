package guestcheckinv1

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"strconv"
	"strings"
	"time"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/repo"
	"kids-checkin/internal/repo/guestsubmission"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
)

type Controller struct {
	submissionRepo guestsubmission.Repo
	sessionStore   session.Storer
}

func NewController(db *sql.DB, sessionStore session.Storer) *Controller {
	return &Controller{
		submissionRepo: guestsubmission.NewRepo(db),
		sessionStore:   sessionStore,
	}
}

func noStoreCache(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	return c.Next()
}

func (controller *Controller) RegisterRoutes(app *fiber.App) {
	group := app.Group("/v1/checkins")
	group.Use(middleware.AuthRequired(controller.sessionStore, ""))
	group.Use(noStoreCache)
	group.Post("/guest-submissions", controller.CreateSubmission)
	group.Get("/guest-submissions", controller.ListSubmissions)
	group.Patch("/guest-submissions/:public_id/status", controller.PatchSubmissionStatus)
	group.Post("/guest-submissions/:public_id/checkins", controller.CreateSubmissionCheckins)

	adminGroup := app.Group("/v1/admin")
	adminGroup.Use(middleware.AuthRequired(controller.sessionStore, "admin"))
	adminGroup.Use(noStoreCache)
	adminGroup.Get("/guest-submissions", controller.AdminListSubmissions)

	app.Get("/checkin", middleware.AuthRequired(controller.sessionStore, ""), controller.KioskPage)
	app.Get("/admin/guest-entries", middleware.AuthRequired(controller.sessionStore, "admin"), controller.AdminPage)
}

func (controller *Controller) KioskPage(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	f, err := static.EmbeddedFS.Open("pages/checkin/index.html")
	if err != nil {
		return fiber.ErrInternalServerError
	}
	defer f.Close()
	c.Type("html")
	return c.SendStream(f)
}

func (controller *Controller) AdminPage(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	f, err := static.EmbeddedFS.Open("pages/admin-guest-entries/index.html")
	if err != nil {
		return fiber.ErrInternalServerError
	}
	defer f.Close()
	c.Type("html")
	return c.SendStream(f)
}

type createSubmissionPayload struct {
	Parent   parentPayload  `json:"parent"`
	Children []childPayload `json:"children"`
}

type parentPayload struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
}

type childPayload struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	DOB       string `json:"dob"`
	Grade     string `json:"grade"`
}

func validateCreateSubmissionPayload(p createSubmissionPayload) error {
	if strings.TrimSpace(p.Parent.FirstName) == "" || strings.TrimSpace(p.Parent.LastName) == "" {
		return errors.New("parent first_name and last_name are required")
	}
	if p.Parent.Phone == "" && p.Parent.Email == "" {
		return errors.New("either parent phone or email is required")
	}
	if len(p.Children) == 0 {
		return errors.New("at least one child is required")
	}
	if len(p.Children) > 10 {
		return errors.New("at most 10 children are allowed per submission")
	}

	if p.Parent.Phone != "" {
		digits := 0
		for _, r := range p.Parent.Phone {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits < 7 {
			return errors.New("phone must contain at least 7 digits")
		}
	}
	if p.Parent.Email != "" && !strings.Contains(p.Parent.Email, "@") {
		return errors.New("invalid email")
	}

	for i, child := range p.Children {
		if strings.TrimSpace(child.FirstName) == "" || strings.TrimSpace(child.LastName) == "" || strings.TrimSpace(child.DOB) == "" || strings.TrimSpace(child.Grade) == "" {
			return fmt.Errorf("child %d: first_name, last_name, dob, and grade are required", i+1)
		}
		dob, err := time.Parse("2006-01-02", child.DOB)
		if err != nil {
			return fmt.Errorf("child %d: dob must be YYYY-MM-DD", i+1)
		}
		if dob.After(time.Now()) {
			return fmt.Errorf("child %d: dob cannot be in the future", i+1)
		}
	}
	return nil
}

func (controller *Controller) CreateSubmission(c *fiber.Ctx) error {
	var payload createSubmissionPayload
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if err := validateCreateSubmissionPayload(payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	parent := guestsubmission.Parent{
		FirstName: payload.Parent.FirstName,
		LastName:  payload.Parent.LastName,
		Phone:     payload.Parent.Phone,
		Email:     payload.Parent.Email,
	}
	children := make([]guestsubmission.Child, 0, len(payload.Children))
	for _, child := range payload.Children {
		children = append(children, guestsubmission.Child{
			FirstName: child.FirstName,
			LastName:  child.LastName,
			DOB:       child.DOB,
			Grade:     child.Grade,
		})
	}

	submission, err := controller.submissionRepo.CreateSubmission(c.Context(), parent, children)
	if err != nil {
		slog.Error("failed to create submission", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}

	return c.JSON(submissionToResponse(submission))
}

func (controller *Controller) ListSubmissions(c *fiber.Ctx) error {
	filter, err := buildFilter(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if filter.Status == "" {
		filter.Status = guestsubmission.StatusPending
	}

	submissions, err := controller.submissionRepo.ListSubmissions(c.Context(), filter)
	if err != nil {
		slog.Error("failed to list submissions", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}

	return c.JSON(submissionsToSummary(submissions))
}

func (controller *Controller) AdminListSubmissions(c *fiber.Ctx) error {
	filter, err := buildFilter(c)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}

	submissions, err := controller.submissionRepo.ListSubmissions(c.Context(), filter)
	if err != nil {
		slog.Error("failed to list admin submissions", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}

	return c.JSON(submissionsToResponse(submissions))
}

func (controller *Controller) PatchSubmissionStatus(c *fiber.Ctx) error {
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

	type statusPayload struct {
		Status string `json:"status"`
	}
	var payload statusPayload
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&payload); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if payload.Status == "" {
		return fiber.NewError(fiber.StatusBadRequest, "status is required")
	}

	submissions, err := controller.submissionRepo.ListSubmissions(c.Context(), guestsubmission.Filter{PublicID: publicID})
	if err != nil {
		slog.Error("failed to list submissions for patch", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}
	if len(submissions) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "submission not found")
	}
	submission := submissions[0]

	sess, _ := controller.sessionStore.Get(c)
	role, _ := sess.Get("role").(string)
	isAdmin := role == "admin"

	if !isValidTransition(isAdmin, submission.Status, payload.Status) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid status transition")
	}

	if payload.Status == guestsubmission.StatusApproved {
		err = controller.submissionRepo.ApproveSubmission(c.Context(), publicID, time.Now().UTC())
	} else {
		err = controller.submissionRepo.UpdateSubmissionStatus(c.Context(), publicID, payload.Status, time.Now().UTC())
	}
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "submission not found")
		}
		if errors.Is(err, guestsubmission.ErrConflict) {
			return fiber.NewError(fiber.StatusBadRequest, "submission status changed, please retry")
		}
		slog.Error("failed to update submission status", "public_id", publicID, "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}

	updated, err := controller.submissionRepo.ListSubmissions(c.Context(), guestsubmission.Filter{PublicID: publicID})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}
	if len(updated) == 0 {
		return fiber.NewError(fiber.StatusInternalServerError, "submission not found after update")
	}
	return c.JSON(submissionToSummary(updated[0]))
}

func (controller *Controller) CreateSubmissionCheckins(c *fiber.Ctx) error {
	publicID := c.Params("public_id")
	if publicID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "public_id is required")
	}

	err := controller.submissionRepo.CreateManualCheckins(c.Context(), publicID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "submission not found")
		}
		if errors.Is(err, guestsubmission.ErrInvalidStatus) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		slog.Error("failed to create manual checkins", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}

	updated, err := controller.submissionRepo.ListSubmissions(c.Context(), guestsubmission.Filter{PublicID: publicID})
	if err != nil {
		slog.Error("failed to list submissions after checkin creation", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "internal error")
	}
	if len(updated) == 0 {
		return fiber.NewError(fiber.StatusInternalServerError, "submission not found after update")
	}
	return c.JSON(submissionToSummary(updated[0]))
}

func buildFilter(c *fiber.Ctx) (guestsubmission.Filter, error) {
	filter := guestsubmission.Filter{}
	if status := c.Query("status"); status != "" {
		switch status {
		case guestsubmission.StatusPending, guestsubmission.StatusApproved, guestsubmission.StatusRejected, guestsubmission.StatusEntered:
		default:
			return guestsubmission.Filter{}, fiber.NewError(fiber.StatusBadRequest, "invalid status")
		}
		filter.Status = status
	}
	if id := c.Query("public_id"); id != "" {
		filter.PublicID = id
	}
	if q := c.Query("without_manual_checkins"); q == "true" || q == "1" {
		filter.WithoutManualCheckins = true
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return guestsubmission.Filter{}, fiber.NewError(fiber.StatusBadRequest, "invalid limit")
		}
		if n > 200 {
			n = 200
		}
		filter.Limit = n
	}
	return filter, nil
}

func isValidTransition(isAdmin bool, from, to string) bool {
	switch {
	case from == guestsubmission.StatusPending && to == guestsubmission.StatusApproved:
		return true
	case from == guestsubmission.StatusPending && to == guestsubmission.StatusRejected:
		return true
	case isAdmin && from == guestsubmission.StatusPending && to == guestsubmission.StatusEntered:
		return true
	case isAdmin && from == guestsubmission.StatusApproved && to == guestsubmission.StatusEntered:
		return true
	default:
		return false
	}
}
