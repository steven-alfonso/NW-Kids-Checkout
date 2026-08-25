package checkoutsfetcher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kids-checkin/internal/client/planningcenter"
	"kids-checkin/internal/db"
	"kids-checkin/internal/logger"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/event"
	"kids-checkin/internal/repo/eventcheckwindow"
	"kids-checkin/internal/repo/location"
	"kids-checkin/internal/static"
	"kids-checkin/internal/telemetry"

	"github.com/google/uuid"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// fetcherTracer resolves against the global tracer provider at span creation
// time, so it picks up the provider installed by telemetry.Setup.
var fetcherTracer = otel.Tracer("kids-checkin/checkout-fetcher")

// eventMutexStripeCount is the number of striped per-event locks. A fixed pool
// bounds memory: a per-event map of mutexes would grow without bound as new
// events are created. The worker semaphore caps concurrency at 5, so 64 stripes
// keep false sharing between unrelated events negligible.
const eventMutexStripeCount = 64

// shutdownGracePeriod is how long the fetcher waits for in-flight work to drain
// after a shutdown signal before force-exiting. It is set slightly above the
// Planning Center client's HTTP timeout so a single in-flight request can
// finish before the force-exit fires.
const shutdownGracePeriod = 11 * time.Second

// retryBackoffBase is the initial delay before an event that failed to fully
// resolve is retried. Each consecutive failure doubles the delay up to
// retryBackoffMax, so a poison event cannot livelock the fetcher by forcing a
// full 12h lookback re-fetch on every 3s cycle.
const retryBackoffBase = 5 * time.Second

// retryBackoffMax caps the per-event retry backoff.
const retryBackoffMax = 5 * time.Minute

// retryEscalationThreshold is the number of consecutive per-event failures
// before the fetcher logs the event at ERROR severity. Before this threshold
// the failures are logged at WARN, so a quietly failing event is not invisible.
const retryEscalationThreshold = 5

// errEventTimeout marks a per-event Planning Center fetch that timed out. It
// is treated as non-fatal for the loop (like success) but is distinguishable
// so the timeout can be observed and reported rather than silently swallowed.
type errEventTimeout struct {
	eventID string
	err     error
}

func (e errEventTimeout) Error() string {
	return fmt.Sprintf("timeout fetching checkouts for event %s: %v", e.eventID, e.err)
}

func (e errEventTimeout) Unwrap() error { return e.err }

// isEventTimeout reports whether err represents a non-fatal per-event timeout.
func isEventTimeout(err error) bool {
	var te errEventTimeout
	return errors.As(err, &te)
}

// errEventCheckinFailure marks an event batch where one or more checkins could
// not be created. It is non-fatal for the loop (like a timeout): the event's
// LastCheckedOutTime is not advanced, so the failed rows are retried on a
// backoff schedule, but the failure is distinguishable so it can be observed
// and counted instead of being treated as a hard loop error. The retry
// guarantee only holds while the failed rows remain inside the trailing
// lookback window (default 12h): if the worker is down for longer than the
// lookback between a failed attempt and its retry, the rows fall out of the
// fetch range and are permanently lost.
type errEventCheckinFailure struct {
	eventID string
	errs    []error
}

func (e errEventCheckinFailure) Error() string {
	return fmt.Sprintf("failed to create checkins for event %s: %v", e.eventID, errors.Join(e.errs...))
}

func (e errEventCheckinFailure) Unwrap() error { return errors.Join(e.errs...) }

// isEventCheckinFailure reports whether err represents a non-fatal per-event
// checkin-creation failure.
func isEventCheckinFailure(err error) bool {
	var cf errEventCheckinFailure
	return errors.As(err, &cf)
}

// errEventFetchFailure marks a per-event Planning Center fetch that was
// truncated by the pagination guard. Like errEventTimeout it is non-fatal for
// the loop: the event's LastCheckedOutTime is not advanced so it is retried
// next cycle, but the failure is distinguishable so it can be observed and
// counted instead of being treated as a hard loop error.
type errEventFetchFailure struct {
	eventID string
	err     error
}

func (e errEventFetchFailure) Error() string {
	return fmt.Sprintf("fetching checkouts for event %s failed: %v", e.eventID, e.err)
}

func (e errEventFetchFailure) Unwrap() error { return e.err }

// isEventFetchFailure reports whether err represents a non-fatal per-event
// fetch truncation.
func isEventFetchFailure(err error) bool {
	var ef errEventFetchFailure
	return errors.As(err, &ef)
}

// errEventFetchError marks a per-event Planning Center fetch that failed with a
// non-timeout, non-truncation error (e.g. a 5xx server error or a malformed
// response). It is non-fatal for the loop (like errEventTimeout): the event's
// LastCheckedOutTime is not advanced so it is retried on a backoff schedule,
// but the failure is distinguishable so it can be observed and counted instead
// of treating a transient per-event failure as a hard loop error that would
// take down the whole worker.
type errEventFetchError struct {
	eventID string
	err     error
}

func (e errEventFetchError) Error() string {
	return fmt.Sprintf("failed to fetch checkouts for event %s: %v", e.eventID, e.err)
}

func (e errEventFetchError) Unwrap() error { return e.err }

// isEventFetchError reports whether err represents a non-fatal per-event fetch
// failure.
func isEventFetchError(err error) bool {
	var ef errEventFetchError
	return errors.As(err, &ef)
}

// errEventDropped marks an event batch where one or more checkouts were
// dropped because their location could not be resolved. It is non-fatal for
// the loop (like errEventTimeout): the event's LastCheckedOutTime is not
// advanced so the dropped rows are retried on a backoff schedule, but the
// failure is distinguishable so it can be observed and counted in the cycle
// summary instead of being invisible to the loop.
type errEventDropped struct {
	eventID      string
	droppedCount int
}

func (e errEventDropped) Error() string {
	return fmt.Sprintf("dropped %d checkouts for event %s", e.droppedCount, e.eventID)
}

// isEventDropped reports whether err represents a non-fatal per-event drop.
func isEventDropped(err error) bool {
	var ed errEventDropped
	return errors.As(err, &ed)
}

// errForceExitNow is returned by runSignalLoop when a second shutdown signal is
// received, so the caller can force-exit the process immediately. A hard exit
// is intentionally terminal and is not part of the normal error plumbing.
var errForceExitNow = errors.New("second shutdown signal received; exiting immediately")

// runSignalLoop relays OS signals to the fetcher's shutdown machinery until
// sigDone is closed. The first signal starts a graceful drain: stopCh is closed
// so no new work is scheduled, and a timer arms forceExitCh after gracePeriod so
// the caller can force-exit if draining takes too long. A second signal returns
// errForceExitNow so the caller can hard-exit immediately.
//
// It runs until sigDone is closed so the goroutine is cleaned up on normal
// shutdown (the first signal alone would otherwise leave it blocked on the
// channel for the rest of the process lifetime). Any pending force-exit timer
// is stopped when it exits.
func runSignalLoop(logCtx context.Context, sigCh <-chan os.Signal, sigDone <-chan struct{}, stopOnce *sync.Once, stopCh, forceExitCh chan struct{}, gracePeriod time.Duration) error {
	var forceExitTimer *time.Timer
	firstSignal := true
	for {
		select {
		case <-sigDone:
			if forceExitTimer != nil {
				forceExitTimer.Stop()
			}
			return nil
		case <-sigCh:
		}

		if firstSignal {
			firstSignal = false
			stopOnce.Do(func() {
				logger.FromContext(logCtx).InfoContext(logCtx, "received shutdown signal, draining; no new work will be scheduled")
				close(stopCh)
				forceExitTimer = time.AfterFunc(gracePeriod, func() {
					close(forceExitCh)
				})
			})
			continue
		}

		// A second signal is the operator's hard-stop request. The pending
		// force-exit timer is no longer needed and must be stopped so its
		// callback cannot fire into forceExitCh after the caller has left the
		// normal shutdown flow.
		if forceExitTimer != nil {
			forceExitTimer.Stop()
		}
		return errForceExitNow
	}
}

// Service coordinates the by-event checkout fetcher. It owns the Planning
// Center client, the repos it reads and writes, and the in-memory location
// map (PlanningCenterID -> local ID) used to resolve checkout locations.
type Service struct {
	pcClient        planningcenter.Client
	eventRepo       event.Repo
	checkinRepo     checkin.Repo
	checkWindowRepo eventcheckwindow.Repo
	locationsRepo   location.Repo

	locationIDMap       *sync.Map
	eventMutexStripes   [eventMutexStripeCount]sync.Mutex
	eventUpdateInterval time.Duration
	useCheckWindows     bool
	// now is the time source used for scheduling decisions (e.g. check-window
	// evaluation and update-interval checks). It is a field rather than a
	// package global so tests can inject a stub clock without mutating shared
	// state; it defaults to time.Now.
	now func() time.Time

	// retryMu guards eventRetries. Events that fail to fully resolve (dropped
	// checkouts, create failures, pagination truncation) are placed in backoff
	// so they are not re-selected every cycle while failing; a success clears
	// the event's state. The map is bounded by the number of auto-fetch events.
	retryMu      sync.Mutex
	eventRetries map[int64]retryState
}

// retryState tracks consecutive failures for a single event and when it may be
// selected again.
type retryState struct {
	consecutiveFailures int
	backoffUntil        time.Time
}

// FetchCheckouts runs the by-event checkout fetcher. It periodically polls
// Planning Center for new checkouts per event, using a background goroutine
// to keep an in-memory location map (PlanningCenterID -> local ID) refreshed
// every 5 minutes.
func FetchCheckouts(ctx context.Context, cmd *cli.Command) error {
	tel, err := telemetry.Setup(ctx, "kids-checkin-fetcher")
	if err != nil {
		return fmt.Errorf("setting up telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := tel.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.Warn("telemetry shutdown failed", slog.String("error", shutdownErr.Error()))
		}
	}()

	dbFile := cmd.String("db-file")
	var database *sql.DB
	if tel.Enabled() {
		database, err = db.InitDBInstrumented(dbFile, tel.TracerProvider, tel.MeterProvider)
	} else {
		database, err = db.InitDB(dbFile)
	}
	if err != nil {
		return fmt.Errorf("init db %s: %w", dbFile, err)
	}

	defer database.Close()

	interval := cmd.Duration("interval")
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}

	eventUpdateInterval := cmd.Duration("event-update-interval")
	if eventUpdateInterval <= 0 {
		return fmt.Errorf("event-update-interval must be greater than 0")
	}

	runtime := cmd.Duration("runtime")
	useCheckWindows := cmd.Bool("use-check-windows")

	var cancel context.CancelFunc
	serviceMode := cmd.Bool("service")
	if serviceMode {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		if runtime <= 0 {
			return fmt.Errorf("runtime must be greater than 0")
		}
		ctx, cancel = context.WithTimeout(ctx, runtime)
	}
	defer cancel()

	logCtx := logger.WithLogger(ctx, slog.With(slog.String("worker", "checkout-fetcher-v2")))

	if serviceMode {
		logger.FromContext(logCtx).WarnContext(logCtx, "running as service: --runtime is ignored")
	}

	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	sigDone := make(chan struct{})
	var stopOnce sync.Once

	// The signal handler is unregistered on return so a second call to
	// FetchCheckouts cannot stack handlers, and sigDone is closed so the loop
	// goroutine exits instead of blocking on the channel forever after a normal
	// shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	defer close(sigDone)

	go func() {
		err := runSignalLoop(logCtx, sigCh, sigDone, &stopOnce, stopCh, forceExitCh, shutdownGracePeriod)
		if errors.Is(err, errForceExitNow) {
			// The second signal is the operator's hard-stop request; it must
			// terminate the process even if the main flow is wedged, so it
			// bypasses normal error plumbing.
			logger.FromContext(logCtx).ErrorContext(logCtx, "received second shutdown signal, exiting immediately")
			os.Exit(1)
		}
	}()

	pcClient, checkinRepo, locationsRepo, eventRepo, eventCheckWindowRepo := getClients(database)

	logAttrs := []any{
		slog.Bool("service", serviceMode),
		slog.Duration("interval", interval),
		slog.Duration("event_update_interval", eventUpdateInterval),
		slog.String("db_file", dbFile),
	}
	if !serviceMode {
		logAttrs = append(logAttrs, slog.Duration("runtime", runtime))
	}
	logAttrs = append(logAttrs, slog.Bool("use_check_windows", useCheckWindows))
	logger.FromContext(logCtx).InfoContext(logCtx, "starting checkout fetcher (by event)", logAttrs...)

	locationMap := sync.Map{}
	svc := &Service{
		pcClient:            pcClient,
		eventRepo:           eventRepo,
		checkinRepo:         checkinRepo,
		checkWindowRepo:     eventCheckWindowRepo,
		locationsRepo:       locationsRepo,
		locationIDMap:       &locationMap,
		eventUpdateInterval: eventUpdateInterval,
		useCheckWindows:     useCheckWindows,
		now:                 time.Now,
	}

	if err := svc.loadLocationMap(ctx); err != nil {
		return fmt.Errorf("failed to load location map: %w", err)
	}

	go svc.refreshLocationMapLoop(logCtx, stopCh)

	return svc.run(logCtx, stopCh, forceExitCh, interval)
}

// loadLocationMap populates the service's in-memory location map from the
// locations repo.
func (s *Service) loadLocationMap(ctx context.Context) error {
	locations, err := s.locationsRepo.ListLocations(ctx, location.LocationFilter{})
	if err != nil {
		return err
	}

	for _, loc := range locations {
		s.locationIDMap.Store(loc.PlanningCenterID, loc.ID)
	}
	return nil
}

// refreshLocationMapLoop keeps s.locationIDMap in sync with the locations
// repo. It runs until ctx is cancelled or stopCh is closed.
func (s *Service) refreshLocationMapLoop(ctx context.Context, stopCh <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
		}

		locations, err := s.locationsRepo.ListLocations(ctx, location.LocationFilter{})
		if err != nil {
			logger.FromContext(ctx).ErrorContext(ctx, "could not fetch locations", "error", err)
			continue
		}

		// Mark-and-sweep: add new entries, remove stale ones that no longer
		// appear in the repo.
		seen := make(map[string]bool, len(locations))
		for _, loc := range locations {
			seen[loc.PlanningCenterID] = true
			s.locationIDMap.Store(loc.PlanningCenterID, loc.ID)
		}
		s.locationIDMap.Range(func(key, _ any) bool {
			if !seen[key.(string)] {
				s.locationIDMap.Delete(key)
			}
			return true
		})
	}
}

// run executes the main fetch loop, polling Planning Center for checkouts on
// the configured interval until it stops.
//
// The polling cadence is measured from the start of each iteration, not its
// completion: the loop start is recorded before the work runs, so the blocking
// select below sleeps only until interval has elapsed since the iteration
// began. If an iteration's work itself takes longer than the interval, the
// pause is clamped to a minimum gap of interval/2 so the loop does not
// busy-loop back-to-back with no pause against the DB and API; the worker
// semaphore and per-event mutexes still bound overlap, so two iterations never
// run concurrently.
//
// Shutdown conditions are checked in two places: once before the loop body (so
// a shutdown that happened while the loop slept or processed is acted on
// immediately instead of scheduling another round of work) and once in the
// blocking select at the end of each iteration. The pre-loop check is a single
// non-blocking probe; it is not duplicate logic, it only avoids starting work
// we know is about to be abandoned.
func (s *Service) run(ctx context.Context, stopCh, forceExitCh <-chan struct{}, interval time.Duration) error {
	for {
		if ctx.Err() != nil || isClosed(stopCh) {
			return nil
		}

		logger.FromContext(ctx).InfoContext(ctx, "checking for events that need updating")

		// Record the loop start so the cadence is measured from the start of the
		// iteration rather than its completion.
		loopStart := s.now()

		err := s.eventCheckoutLoop(ctx, stopCh)
		if err != nil {
			if shouldSwallowLoopError(ctx, stopCh) {
				continue
			}
			return fmt.Errorf("failed to eventCheckoutLoop: %w", err)
		}

		// The cadence is measured from loop start: the pause is the remainder
		// of the interval that had not elapsed when the work finished. If the
		// work itself took longer than the interval, the remainder is zero or
		// negative; clamp a minimum gap in that case so a slow cycle does not
		// busy-loop back-to-back with no pause against the DB and API.
		gap := interval - s.now().Sub(loopStart)
		if gap <= 0 {
			gap = interval / 2
		}
		timer := time.NewTimer(gap)

		select {
		case <-forceExitCh:
			timer.Stop()
			return forceExit(ctx)
		case <-stopCh:
			timer.Stop()
			return nil
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		timer.Stop()
	}
}

// forceExit reports that the graceful drain did not complete within the grace
// period. It is the single place that formats the forced-exit error, so the
// message and log stay consistent across the shutdown paths that use it.
func forceExit(ctx context.Context) error {
	logger.FromContext(ctx).ErrorContext(ctx, "graceful shutdown did not complete within the grace period, forcing exit", slog.Duration("grace_period", shutdownGracePeriod))
	return fmt.Errorf("graceful shutdown did not complete within %s", shutdownGracePeriod)
}

// isClosed reports whether ch has been closed.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// shouldSwallowLoopError reports whether a per-loop error should be treated as
// non-fatal because the fetcher is shutting down or its runtime has expired.
// During a graceful drain the context is intentionally not cancelled so
// in-flight work can complete, so stopCh must be checked in addition to ctx.
func shouldSwallowLoopError(ctx context.Context, stopCh <-chan struct{}) bool {
	select {
	case <-stopCh:
		return true
	default:
	}
	return ctx.Err() != nil
}

func (s *Service) eventCheckoutLoop(ctx context.Context, stopCh <-chan struct{}) error {
	ctx, span := fetcherTracer.Start(ctx, "checkout_fetch_cycle")
	defer span.End()

	select {
	case <-stopCh:
		return nil
	default:
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	autoFetch := true
	events, err := s.eventRepo.ListEvents(ctx, event.EventFilter{
		AutoFetch: &autoFetch,
	})
	if err != nil {
		return fmt.Errorf("failed to list events: %w", err)
	}

	// Bound the retry map to the current auto-fetch event set. Entries are
	// otherwise only cleared by a fully successful cycle, so an event that
	// fails and is then removed or disabled would leave a permanent entry.
	activeIDs := make(map[int64]struct{}, len(events))
	for _, ev := range events {
		activeIDs[ev.ID] = struct{}{}
	}
	s.pruneRetryStates(activeIDs)

	now := s.now()

	windowsByEvent := make(map[int64][]eventcheckwindow.EventCheckWindow)
	var active map[int64]bool
	if s.useCheckWindows {
		windows, err := s.checkWindowRepo.ListCheckWindows(ctx, eventcheckwindow.Filter{})
		if err != nil {
			return fmt.Errorf("failed to list check windows: %w", err)
		}

		for _, w := range windows {
			windowsByEvent[w.EventID] = append(windowsByEvent[w.EventID], w)
		}

		active = activeEventIDs(logger.FromContext(ctx), windowsByEvent, now)
	}

	var eventsToUpdate []event.Event
	skippedOutsideWindow := 0
	for _, ev := range events {
		if s.useCheckWindows {
			if _, hasWindows := windowsByEvent[ev.ID]; hasWindows && !active[ev.ID] {
				skippedOutsideWindow++
				continue
			}
		}
		if ev.LastCheckedOutTime.IsZero() || now.Sub(ev.LastCheckedOutTime) >= s.eventUpdateInterval {
			if s.inRetryBackoff(ev.ID, now) {
				continue
			}
			eventsToUpdate = append(eventsToUpdate, ev)
		}
	}

	if skippedOutsideWindow > 0 {
		logger.FromContext(ctx).InfoContext(ctx, "events skipped: outside configured check window", slog.Int("skipped_count", skippedOutsideWindow))
	}

	if len(eventsToUpdate) == 0 {
		logger.FromContext(ctx).InfoContext(ctx, "no events need updating")
		return nil
	}

	logger.FromContext(ctx).InfoContext(ctx, "processing events that need updating", slog.Int("total_events", len(events)), slog.Int("events_to_update", len(eventsToUpdate)))

	var (
		wg             sync.WaitGroup
		errs           []error
		timeouts       int
		createFailures int
		fetchFailures  int
		fetchErrors    int
		droppedEvents  int
		errMu          sync.Mutex
		sem            = make(chan struct{}, 5)
	)

loop:
	for _, ev := range eventsToUpdate {
		select {
		case <-ctx.Done():
			errMu.Lock()
			errs = append(errs, ctx.Err())
			errMu.Unlock()
			break loop
		case <-stopCh:
			break loop
		case sem <- struct{}{}:
			wg.Add(1)
			go func(ev event.Event) {
				defer wg.Done()
				defer func() { <-sem }()
				if err := s.processEventCheckouts(ctx, ev, now); err != nil {
					errMu.Lock()
					switch {
					case isEventTimeout(err):
						timeouts++
					case isEventCheckinFailure(err):
						createFailures++
					case isEventFetchFailure(err):
						fetchFailures++
					case isEventFetchError(err):
						fetchErrors++
					case isEventDropped(err):
						droppedEvents++
					default:
						errs = append(errs, err)
					}
					errMu.Unlock()
				}
			}(ev)
		}
	}

	wg.Wait()

	if timeouts > 0 {
		logger.FromContext(ctx).WarnContext(ctx, "checkout fetch timed out for some events; will retry next cycle", slog.Int("timeout_count", timeouts))
	}

	if createFailures > 0 {
		logger.FromContext(ctx).WarnContext(ctx, "failed to create checkins for some events; will retry next cycle", slog.Int("failed_event_count", createFailures))
	}

	if fetchFailures > 0 {
		logger.FromContext(ctx).WarnContext(ctx, "pagination limit exceeded for some events; will retry next cycle", slog.Int("failed_event_count", fetchFailures))
	}

	if fetchErrors > 0 {
		logger.FromContext(ctx).WarnContext(ctx, "failed to fetch checkouts for some events; will retry next cycle", slog.Int("failed_event_count", fetchErrors))
	}

	if droppedEvents > 0 {
		logger.FromContext(ctx).WarnContext(ctx, "dropped checkouts for some events; will retry next cycle", slog.Int("dropped_event_count", droppedEvents))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// getEventMutex returns the striped lock guarding the given event. Using
// uint64 for the modulo avoids a negative index if eventID is ever negative.
func (s *Service) getEventMutex(eventID int64) *sync.Mutex {
	return &s.eventMutexStripes[uint64(eventID)%uint64(len(s.eventMutexStripes))]
}

// recordRetryFailure marks ev as having failed to fully resolve this cycle and
// puts it in backoff for an exponentially growing delay, doubling per
// consecutive failure. Once an event has failed retryEscalationThreshold
// consecutive cycles it is logged at ERROR severity, because each further
// failure brings the unresolved checkouts closer to aging out of the lookback
// window (see processEventCheckouts).
func (s *Service) recordRetryFailure(ctx context.Context, eventID int64, now time.Time, failedIDs []string) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if s.eventRetries == nil {
		s.eventRetries = make(map[int64]retryState)
	}

	st := s.eventRetries[eventID]
	st.consecutiveFailures++

	shift := st.consecutiveFailures - 1
	if shift > 8 {
		shift = 8
	}
	delay := retryBackoffBase << shift
	if delay > retryBackoffMax {
		delay = retryBackoffMax
	}
	st.backoffUntil = now.Add(delay)
	s.eventRetries[eventID] = st

	if st.consecutiveFailures >= retryEscalationThreshold {
		// This is the escalation tripwire for log-based monitoring. Alert on
		// alert_name=event-checkouts-escalation: an event has failed
		// retryEscalationThreshold consecutive cycles, so its unresolved
		// checkouts are at risk of aging out of the lookback window (see
		// processEventCheckouts) and being permanently lost.
		logger.FromContext(ctx).ErrorContext(ctx,
			"event failed to resolve for consecutive cycles; checkouts may age out of the lookback window",
			slog.Int64("event_id", eventID),
			slog.Int("consecutive_failures", st.consecutiveFailures),
			slog.Int("failed_count", len(failedIDs)),
			slog.Bool("alert", true),
			slog.String("alert_name", "event-checkouts-escalation"),
		)
	}
}

// resetRetryState clears ev's retry state after a fully successful cycle, so
// an event that resolves does not carry prior failures toward escalation.
func (s *Service) resetRetryState(eventID int64) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	delete(s.eventRetries, eventID)
}

// pruneRetryStates drops retry state for events that are no longer in the
// auto-fetch set. Without this the map would grow without bound as events are
// removed or disabled, since entries are otherwise only cleared on a fully
// successful cycle.
func (s *Service) pruneRetryStates(activeIDs map[int64]struct{}) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	for id := range s.eventRetries {
		if _, ok := activeIDs[id]; !ok {
			delete(s.eventRetries, id)
		}
	}
}

// inRetryBackoff reports whether ev is currently backed off and should not be
// selected for processing.
func (s *Service) inRetryBackoff(eventID int64, now time.Time) bool {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	st, ok := s.eventRetries[eventID]
	return ok && st.backoffUntil.After(now)
}

func (s *Service) processEventCheckouts(ctx context.Context, ev event.Event, now time.Time) (err error) {
	ctx, span := fetcherTracer.Start(
		ctx,
		"process_event_checkouts",
		trace.WithAttributes(attribute.String("pc_event_id", ev.PlanningCenterID)),
	)
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}()

	mu := s.getEventMutex(ev.ID)
	mu.Lock()
	defer mu.Unlock()

	currentEvent, err := s.eventRepo.GetEventByID(ctx, ev.ID)
	if err != nil {
		return fmt.Errorf("failed to get event %d: %w", ev.ID, err)
	}

	timeToUse := currentEvent.LastCheckedOutTime
	lookBack := getLookBackTime(logger.FromContext(ctx))
	if timeToUse.Before(now.Add(lookBack)) {
		timeToUse = now.Add(lookBack)
	}

	checkouts, err := s.pcClient.GetCheckoutsForEvent(ctx, currentEvent.PlanningCenterID, timeToUse, 0)

	if err != nil {
		var timeoutErr *planningcenter.TimeoutError
		if errors.As(err, &timeoutErr) {
			logger.FromContext(ctx).WarnContext(ctx, "timeout fetching checkouts for event", slog.String("event_id", currentEvent.PlanningCenterID), slog.String("error", err.Error()))
			s.recordRetryFailure(ctx, currentEvent.ID, now, nil)
			return errEventTimeout{eventID: currentEvent.PlanningCenterID, err: err}
		}
		if errors.Is(err, planningcenter.ErrPaginationLimitExceeded) {
			logger.FromContext(ctx).WarnContext(ctx, "pagination limit exceeded fetching checkouts for event", slog.String("event_id", currentEvent.PlanningCenterID))
			s.recordRetryFailure(ctx, currentEvent.ID, now, nil)
			return errEventFetchFailure{eventID: currentEvent.PlanningCenterID, err: err}
		}
		logger.FromContext(ctx).WarnContext(ctx, "failed to fetch checkouts for event", slog.String("event_id", currentEvent.PlanningCenterID), slog.String("error", err.Error()))
		s.recordRetryFailure(ctx, currentEvent.ID, now, nil)
		return errEventFetchError{eventID: currentEvent.PlanningCenterID, err: err}
	}
	logger.FromContext(ctx).InfoContext(ctx, "fetched checkouts for event", slog.String("event_id", currentEvent.PlanningCenterID), slog.Int("checkouts_count", len(checkouts)))

	var (
		dropped    int
		droppedIDs []string
		batchErrs  []error
		failedIDs  []string
	)
	for _, checkout := range checkouts {
		locationIDA, found := s.locationIDMap.Load(checkout.PlanningCenterLocationID)
		if !found {
			dropped++
			droppedIDs = append(droppedIDs, checkout.ID)
			logger.FromContext(ctx).ErrorContext(ctx, "could not find location by planning center id in map", "checkout_pc_id", checkout.PlanningCenterLocationID)
			continue
		}

		locID, ok := locationIDA.(int64)
		if !ok {
			dropped++
			droppedIDs = append(droppedIDs, checkout.ID)
			logger.FromContext(ctx).ErrorContext(ctx,
				"unexpected location id type in map",
				slog.String("checkout_pc_id", checkout.PlanningCenterLocationID),
				slog.Any("type", fmt.Sprintf("%T", locationIDA)),
			)
			continue
		}

		co := checkin.Checkin{
			PlanningCenterID: checkout.ID,
			LocationID:       locID,
			EventID:          currentEvent.ID,
			FirstName:        checkout.FirstName,
			LastName:         checkout.LastName,
			SecurityCode:     checkout.SecurityCode,
			CheckedOutAt:     checkout.CheckedOutAt,
			FetchedAt:        checkout.FetchedAt,
		}

		if _, err := s.checkinRepo.CreateCheckin(ctx, co); err != nil {
			batchErrs = append(batchErrs, fmt.Errorf("create checkin %s for event %s: %w", co.PlanningCenterID, currentEvent.PlanningCenterID, err))
			failedIDs = append(failedIDs, co.PlanningCenterID)
			continue
		}
	}

	// If some checkouts could not be created, do not advance LastCheckedOutTime.
	// Advancing the window would permanently lose the failed rows, since the
	// next cycle queries Planning Center for checkouts after the new timestamp
	// and never re-reads them. The non-fatal errEventCheckinFailure keeps the
	// loop running so the event is retried; checkouts already inserted are
	// idempotent upserts. The retry guarantee only holds while the failed rows
	// remain inside the trailing lookback window (default 12h): if the worker
	// is down for longer than the lookback between a failed attempt and its
	// retry, the rows fall out of the fetch range and are permanently lost.
	if len(batchErrs) > 0 {
		logger.FromContext(ctx).WarnContext(ctx,
			"failed to create some checkins; not advancing LastCheckedOutTime so they are retried",
			slog.String("event_id", currentEvent.PlanningCenterID),
			slog.Int("failed_count", len(batchErrs)),
		)
		s.recordRetryFailure(ctx, currentEvent.ID, now, failedIDs)
		return errEventCheckinFailure{eventID: currentEvent.PlanningCenterID, errs: batchErrs}
	}

	// If any checkouts were dropped because their location could not be
	// resolved, do not advance LastCheckedOutTime. Advancing the window would
	// permanently lose those checkouts, since the next cycle queries Planning
	// Center for checkouts after the new timestamp and never re-reads them.
	// Keeping the window in place retries the event; checkouts already inserted
	// are idempotent upserts. The retry guarantee only holds while the dropped
	// rows remain inside the trailing lookback window (default 12h): if the
	// worker is down for longer than the lookback between a failed attempt and
	// its retry, the rows fall out of the fetch range and are permanently lost.
	if dropped > 0 {
		logger.FromContext(ctx).WarnContext(ctx,
			"dropped checkouts for event; not advancing LastCheckedOutTime so they are retried",
			slog.String("event_id", currentEvent.PlanningCenterID),
			slog.Int("dropped_count", dropped),
		)
		s.recordRetryFailure(ctx, currentEvent.ID, now, droppedIDs)
		return errEventDropped{eventID: currentEvent.PlanningCenterID, droppedCount: dropped}
	}

	// LastCheckedOutTime is stamped with the cycle-start time (captured once in
	// eventCheckoutLoop), not the actual processing-completion time. For a
	// long-running cycle this stamps the window slightly in the past, causing a
	// redundant re-fetch of already-created checkouts next cycle. Acceptable:
	// the re-created rows are idempotent upserts, so no data is lost or
	// duplicated.
	newLastCheckedOut := now.UTC()
	updatedEvent := event.Event{
		ID:                 currentEvent.ID,
		Name:               currentEvent.Name,
		PlanningCenterID:   currentEvent.PlanningCenterID,
		AutoFetch:          currentEvent.AutoFetch,
		LastCheckedOutTime: newLastCheckedOut,
		LocationGroupID:    currentEvent.LocationGroupID,
	}

	err = s.eventRepo.UpdateEvent(ctx, updatedEvent)
	if err != nil {
		return fmt.Errorf("failed to update event %d: %w", currentEvent.ID, err)
	}

	s.resetRetryState(currentEvent.ID)

	return nil
}

// minutesPerWeek is one week expressed in minutes (Monday 00:00 UTC to the
// following Monday 00:00 UTC).
const minutesPerWeek = 7 * 24 * 60

// Window represents a time range within a week, in minutes elapsed since Monday
// 00:00 UTC. EndMinutes is exclusive.
type Window struct {
	StartMinutes int
	EndMinutes   int
}

// minutesSinceWeekStartUTC returns how many minutes have elapsed since Monday
// 00:00:00 UTC of the week containing now.
func minutesSinceWeekStartUTC(now time.Time) int {
	now = now.UTC()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekday--
	weekStart := now.AddDate(0, 0, -weekday)
	weekStart = weekStart.Truncate(24 * time.Hour)
	return int(now.Sub(weekStart).Minutes())
}

func parseTime(timeStr string) (hour, minute int, err error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format: %s", timeStr)
	}
	parts[0] = strings.TrimLeft(parts[0], "0")
	if parts[0] == "" {
		parts[0] = "0"
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour: %s", timeStr)
	}

	parts[1] = strings.TrimLeft(parts[1], "0")
	if parts[1] == "" {
		parts[1] = "0"
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute: %s", timeStr)
	}
	return hour, minute, nil
}

// localToUTCWeekMinutes converts a local (day of week, hour, minute, timezone)
// to minutes since Monday 00:00 UTC, anchored to the week that now falls in.
func localToUTCWeekMinutes(dayOfWeek, hour, minute int, timezone string, now time.Time) (int, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	now = now.In(loc)

	utcNow := now.UTC()
	utcWeekday := int(utcNow.Weekday())
	if utcWeekday == 0 {
		utcWeekday = 7
	}
	utcWeekday--
	utcWeekStart := utcNow.AddDate(0, 0, -utcWeekday)
	utcWeekStart = time.Date(utcWeekStart.Year(), utcWeekStart.Month(), utcWeekStart.Day(), 0, 0, 0, 0, time.UTC)

	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekday--

	weekStart := time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, loc)
	targetTime := time.Date(
		weekStart.Year(), weekStart.Month(), weekStart.Day()+(dayOfWeek-1),
		hour, minute, 0, 0, loc,
	)

	// time.Date resolves an ambiguous (DST fall-back) wall-clock time to one of
	// the two occurrences; Go does not guarantee which. When it picks the later,
	// post-transition occurrence, the same wall clock read one real hour earlier
	// is identical, and the window boundary would extend an hour too long. Prefer
	// the earlier occurrence so boundaries follow wall-clock semantics.
	if earlier := targetTime.Add(-time.Hour); earlier.In(loc).Hour() == targetTime.In(loc).Hour() && earlier.In(loc).Minute() == targetTime.In(loc).Minute() {
		targetTime = earlier
	}

	return int(targetTime.UTC().Sub(utcWeekStart).Minutes()), nil
}

// mergeWindows converts check windows into merged UTC week-minute ranges,
// splitting windows that cross the Sunday/Saturday boundary into two ranges
// and coalescing overlaps. now anchors which week the windows map to.
func mergeWindows(checkWindows []eventcheckwindow.EventCheckWindow, now time.Time) ([]Window, error) {
	var ranges []Window

	for _, ct := range checkWindows {
		startH, startM, err := parseTime(ct.StartTime)
		if err != nil {
			return nil, fmt.Errorf("parse start time: %w", err)
		}
		endH, endM, err := parseTime(ct.EndTime)
		if err != nil {
			return nil, fmt.Errorf("parse end time: %w", err)
		}

		startMinutes, err := localToUTCWeekMinutes(ct.StartDayOfWeek, startH, startM, ct.Timezone, now)
		if err != nil {
			return nil, fmt.Errorf("start time timezone: %w", err)
		}
		endMinutes, err := localToUTCWeekMinutes(ct.EndDayOfWeek, endH, endM, ct.Timezone, now)
		if err != nil {
			return nil, fmt.Errorf("end time timezone: %w", err)
		}

		if startMinutes <= endMinutes {
			ranges = append(ranges, Window{startMinutes, endMinutes})
		} else {
			ranges = append(ranges,
				Window{startMinutes, minutesPerWeek},
				Window{0, endMinutes},
			)
		}
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].StartMinutes < ranges[j].StartMinutes
	})

	merged := make([]Window, 0)
	for _, r := range ranges {
		if len(merged) == 0 || merged[len(merged)-1].EndMinutes < r.StartMinutes {
			merged = append(merged, r)
		} else {
			if r.EndMinutes > merged[len(merged)-1].EndMinutes {
				merged[len(merged)-1].EndMinutes = r.EndMinutes
			}
		}
	}

	return merged, nil
}

// activeEventIDs returns the set of event IDs whose merged check windows cover
// the time now. Events with no windows are not included.
func activeEventIDs(log *slog.Logger, windowsByEvent map[int64][]eventcheckwindow.EventCheckWindow, now time.Time) map[int64]bool {
	active := make(map[int64]bool, len(windowsByEvent))
	nowMinutes := minutesSinceWeekStartUTC(now)

	for eventID, eventWindows := range windowsByEvent {
		merged, err := mergeWindows(eventWindows, now)
		if err != nil {
			log.Warn("could not merge check windows for event", slog.Int64("event_id", eventID), slog.String("error", err.Error()))
			continue
		}

		for _, w := range merged {
			if nowMinutes >= w.StartMinutes && nowMinutes < w.EndMinutes {
				active[eventID] = true
				break
			}
		}
	}

	return active
}

var (
	lookBackOnce   sync.Once
	lookBackResult time.Duration
)

// getLookBackTime returns the time to look back for checkouts based on the
// CHECKOUT_FETCHER_LOOKBACK_TIME env var. The env var is parsed once and
// cached for the process lifetime; warnings about invalid values are logged
// through the provided structured logger.
func getLookBackTime(log *slog.Logger) time.Duration {
	lookBackOnce.Do(func() {
		lookBackResult = resolveLookBackTime(log, os.Getenv("CHECKOUT_FETCHER_LOOKBACK_TIME"))
	})
	return lookBackResult
}

// resolveLookBackTime parses a CHECKOUT_FETCHER_LOOKBACK_TIME value, falling
// back to the default when empty or invalid.
func resolveLookBackTime(log *slog.Logger, lbStr string) time.Duration {
	const defaultLookBackTime = -12 * time.Hour

	if lbStr == "" {
		return defaultLookBackTime
	}

	lb, err := time.ParseDuration(lbStr)
	if err != nil {
		log.Warn("could not parse CHECKOUT_FETCHER_LOOKBACK_TIME, using default", slog.String("env_var", lbStr), slog.Duration("default", defaultLookBackTime))
		return defaultLookBackTime
	}

	if lb > 0 {
		log.Warn("CHECKOUT_FETCHER_LOOKBACK_TIME must not be greater than 0, using default", slog.String("env_var", lbStr), slog.Duration("default", defaultLookBackTime))
		return defaultLookBackTime
	}

	return lb
}

func getClients(db *sql.DB) (planningcenter.Client, checkin.Repo, location.Repo, event.Repo, eventcheckwindow.Repo) {
	if strings.ToLower(os.Getenv("CHECKOUT_FETCHER_USE_MOCK")) != "true" {
		return planningcenter.NewClient(), checkin.NewRepo(db), location.NewRepo(db), event.NewRepo(db), eventcheckwindow.NewRepo(db)
	}

	locationsRepo := location.NewRepo(db)
	checkinRepo := checkin.NewRepo(db)
	eventRepo := event.NewRepo(db)
	eventCheckWindowRepo := eventcheckwindow.NewRepo(db)

	var pcClient planningcenter.Client = &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			locations, err := locationsRepo.ListLocations(ctx, location.LocationFilter{})
			if err != nil {
				return nil, err
			}

			var locID string
			if len(locations) > 0 {
				loc := locations[rand.Intn(len(locations))]
				locID = loc.PlanningCenterID
			}

			checkedOutAt := time.Now().UTC()
			// The mock stamps FetchedAt a little after CheckedOutAt so the
			// fetch-latency metric has meaningful data when run against the mock.
			fetchedAt := checkedOutAt.Add(time.Duration(rand.Intn(15)) * time.Second)

			return []planningcenter.Checkout{
				{
					ID:                       "pccheckoutevent_" + uuid.New().String(),
					FirstName:                static.RandomFirstName(),
					LastName:                 static.RandomLastName(),
					SecurityCode:             strings.ToUpper(uuid.New().String()[:4]),
					CheckedOutAt:             checkedOutAt,
					FetchedAt:                fetchedAt,
					PlanningCenterLocationID: locID,
				},
			}, nil
		},
	}

	return pcClient, checkinRepo, locationsRepo, eventRepo, eventCheckWindowRepo
}
