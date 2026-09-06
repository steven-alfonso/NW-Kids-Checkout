package metricsv1

import (
	"fmt"
	"math"
	"strconv"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/repo/metrics"

	"github.com/gofiber/fiber/v2"
)

type Controller struct {
	repo         metrics.Repo
	sessionStore session.Storer
}

func NewController(repo metrics.Repo, sessionStore session.Storer) *Controller {
	return &Controller{repo: repo, sessionStore: sessionStore}
}

func (controller *Controller) RegisterRoutes(app fiber.Router) {
	group := app.Group("/v1/admin/metrics")
	group.Use(middleware.AuthRequired(controller.sessionStore, "admin"))
	group.Get("", controller.GetMetrics)
	group.Get("/fetch-latency", controller.GetFetchLatency)
	group.Get("/guest", controller.GetGuestMetrics)
}

type DailyMetricResponse struct {
	Date              string  `json:"date"`
	EventName         string  `json:"event_name"`
	Called            int     `json:"called"`
	Confirmed         int     `json:"confirmed"`
	Unconfirmed       int     `json:"unconfirmed"`
	AvgConfirmMinutes float64 `json:"avg_confirm_minutes"`
}

type MetricsResponse struct {
	Days  int                   `json:"days"`
	Daily []DailyMetricResponse `json:"daily"`
}

// parseDays reads and validates the days query param. It returns -1 on invalid
// input and the default of 14 when the param is absent.
func parseDays(c *fiber.Ctx) int {
	raw := c.Query("days")
	if raw == "" {
		return 14
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > 90 {
		return -1
	}
	return parsed
}

func (controller *Controller) GetMetrics(c *fiber.Ctx) error {
	days := parseDays(c)
	if days < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "days must be an integer between 1 and 90")
	}

	daily, err := controller.repo.ListDailyMetrics(c.Context(), metrics.Filter{Days: days})
	if err != nil {
		return fmt.Errorf("listing daily metrics: %w", err)
	}

	response := MetricsResponse{Days: days, Daily: make([]DailyMetricResponse, 0, len(daily))}
	for _, dm := range daily {
		response.Daily = append(response.Daily, DailyMetricResponse{
			Date:              dm.Date,
			EventName:         dm.EventName,
			Called:            dm.Called,
			Confirmed:         dm.Confirmed,
			Unconfirmed:       dm.Unconfirmed,
			AvgConfirmMinutes: math.Round(dm.AvgConfirmMinutes*100) / 100,
		})
	}
	return c.JSON(response)
}

type FetchLatencyMetricResponse struct {
	Date  string  `json:"date"`
	Count int     `json:"count"`
	AvgMs float64 `json:"avg_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
}

type FetchLatencyResponse struct {
	Days int                          `json:"days"`
	Rows []FetchLatencyMetricResponse `json:"rows"`
}

type GuestMetricResponse struct {
	Date        string `json:"date"`
	Submissions int    `json:"submissions"`
	Children    int    `json:"children"`
	Entered     int    `json:"entered"`
	Approved    int    `json:"approved"`
	Rejected    int    `json:"rejected"`
	Pending     int    `json:"pending"`
}

type GuestMetricsResponse struct {
	Days int                   `json:"days"`
	Rows []GuestMetricResponse `json:"rows"`
}

func (controller *Controller) GetFetchLatency(c *fiber.Ctx) error {
	days := parseDays(c)
	if days < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "days must be an integer between 1 and 90")
	}

	latency, err := controller.repo.ListFetchLatency(c.Context(), metrics.Filter{Days: days})
	if err != nil {
		return fmt.Errorf("listing fetch latency: %w", err)
	}

	response := FetchLatencyResponse{Days: days, Rows: make([]FetchLatencyMetricResponse, 0, len(latency))}
	for _, fm := range latency {
		response.Rows = append(response.Rows, FetchLatencyMetricResponse{
			Date:  fm.Date,
			Count: fm.Count,
			AvgMs: math.Round(fm.AvgMs*100) / 100,
			P95Ms: math.Round(fm.P95Ms*100) / 100,
			P99Ms: math.Round(fm.P99Ms*100) / 100,
		})
	}
	return c.JSON(response)
}

func (controller *Controller) GetGuestMetrics(c *fiber.Ctx) error {
	days := parseDays(c)
	if days < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "days must be an integer between 1 and 90")
	}

	guestMetrics, err := controller.repo.ListGuestMetrics(c.Context(), metrics.Filter{Days: days})
	if err != nil {
		return fmt.Errorf("listing guest metrics: %w", err)
	}

	response := GuestMetricsResponse{Days: days, Rows: make([]GuestMetricResponse, 0, len(guestMetrics))}
	for _, gm := range guestMetrics {
		response.Rows = append(response.Rows, GuestMetricResponse{
			Date:        gm.Date,
			Submissions: gm.Submissions,
			Children:    gm.Children,
			Entered:     gm.Entered,
			Approved:    gm.Approved,
			Rejected:    gm.Rejected,
			Pending:     gm.Pending,
		})
	}
	return c.JSON(response)
}
