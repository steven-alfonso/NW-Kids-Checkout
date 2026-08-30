package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/event"
	"kids-checkin/internal/repo/guestsubmission"
	"kids-checkin/internal/repo/location"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/google/uuid"
)

func main() {
	var dbFile string
	var count int
	flag.StringVar(&dbFile, "db-file", "", "path to sqlite db file (defaults to DB_FILE env or database/kids-checkin.db)")
	flag.IntVar(&count, "count", 20, "number of records to create per type")
	flag.Parse()

	if dbFile == "" {
		dbFile = os.Getenv("DB_FILE")
		if dbFile == "" {
			dbFile = "database/kids-checkin.db"
		}
	}

	slog.Info("random-data: starting", slog.String("db_file", dbFile), slog.Int("count", count))

	database, err := db.InitDB(dbFile)
	if err != nil {
		slog.Error("failed to init DB", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()

	ctx := context.Background()

	if err := ensurePrerequisites(ctx, database); err != nil {
		slog.Error("failed to ensure prerequisites", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := seedCheckins(ctx, database, count); err != nil {
		slog.Error("failed to seed checkins", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := seedManualCheckins(ctx, database, count); err != nil {
		slog.Error("failed to seed manual checkins", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := seedGuestSubmissions(ctx, database, count); err != nil {
		slog.Error("failed to seed guest submissions", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.Info("random-data: complete", slog.Int("checkins", count), slog.Int("manual_checkins", count), slog.Int("guest_submissions", count))
}

func ensurePrerequisites(ctx context.Context, database *sql.DB) error {
	eventRepo := event.NewRepo(database)
	events, err := eventRepo.ListEvents(ctx, event.EventFilter{})
	if err != nil {
		return fmt.Errorf("listing events: %w", err)
	}
	var eventID int64
	if len(events) == 0 {
		created, err := eventRepo.CreateEvent(ctx, event.Event{
			Name:             fmt.Sprintf("Random Event %d", rand.Intn(100000)),
			PlanningCenterID: uuid.NewString(),
		})
		if err != nil {
			return fmt.Errorf("creating event: %w", err)
		}
		eventID = created.ID
		slog.Info("created prerequisite event", slog.Int64("event_id", eventID))
	} else {
		eventID = events[rand.Intn(len(events))].ID
	}

	locationRepo := location.NewRepo(database)
	locations, err := locationRepo.ListLocations(ctx, location.LocationFilter{})
	if err != nil {
		return fmt.Errorf("listing locations: %w", err)
	}
	if len(locations) == 0 {
		created, err := locationRepo.CreateLocation(ctx, location.Location{
			PlanningCenterID: uuid.NewString(),
			EventID:          eventID,
			Name:             fmt.Sprintf("Random Location %d", rand.Intn(100000)),
		})
		if err != nil {
			return fmt.Errorf("creating location: %w", err)
		}
		slog.Info("created prerequisite location", slog.Int64("location_id", created.ID))
	}
	return nil
}

var firstNames = []string{
	"Emma", "Liam", "Olivia", "Noah", "Ava", "Ethan", "Sophia", "Mason",
	"Isabella", "Logan", "Mia", "James", "Charlotte", "Lucas", "Amelia",
	"Oliver", "Harper", "Elijah", "Aria", "Henry", "Evelyn", "Alexander",
	"Grace", "Michael", "Chloe", "Daniel", "Lily", "Matthew", "Avery",
	"Samuel", "Sofia", "David", "Jackson", "Scarlett", "Wyatt", "Zoe",
	"John", "Jane", "Alex", "Sam", "Taylor", "Jordan", "Casey", "Riley",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller",
	"Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez",
	"Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin",
	"Lee", "Perez", "Thompson", "White", "Harris", "Sanchez", "Clark",
	"Ramirez", "Lewis", "Robinson", "Walker", "Young", "Allen", "King",
	"Wright", "Scott", "Torres", "Nguyen", "Hill", "Flores", "Green",
}

var grades = []string{
	"None", "Pre-K", "Kindergarten", "1st", "2nd", "3rd", "4th", "5th", "6th", "7th", "8th", "9th", "10th", "11th", "12th",
}

func randomFirstName() string { return firstNames[rand.Intn(len(firstNames))] }
func randomLastName() string  { return lastNames[rand.Intn(len(lastNames))] }
func randomGrade() string     { return grades[rand.Intn(len(grades))] }

func randomPhone() string {
	// 10-digit US-style number
	return fmt.Sprintf("555-%03d-%04d", rand.Intn(900)+100, rand.Intn(9000)+1000)
}

func randomEmail(first, last string) string {
	domains := []string{"example.com", "test.com", "kids.local", "mail.com"}
	return fmt.Sprintf("%s.%s%d@%s", first, last, rand.Intn(1000), domains[rand.Intn(len(domains))])
}

func randomDOB() string {
	year := rand.Intn(11) + 2014 // 2014-2024
	month := time.Month(rand.Intn(12) + 1)
	day := rand.Intn(28) + 1
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func randomSecurityCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func seedCheckins(ctx context.Context, database *sql.DB, count int) error {
	locationRepo := location.NewRepo(database)
	locations, err := locationRepo.ListLocations(ctx, location.LocationFilter{})
	if err != nil {
		return err
	}
	if len(locations) == 0 {
		return fmt.Errorf("no locations available for checkins")
	}

	eventRepo := event.NewRepo(database)
	events, _ := eventRepo.ListEvents(ctx, event.EventFilter{})
	var eventIDs []int64
	for _, e := range events {
		eventIDs = append(eventIDs, e.ID)
	}

	repo := checkin.NewRepo(database)
	for i := 0; i < count; i++ {
		loc := locations[rand.Intn(len(locations))]
		var eventID int64
		if len(eventIDs) > 0 && rand.Float32() < 0.8 {
			eventID = eventIDs[rand.Intn(len(eventIDs))]
		}

		// Random checked-out time within last 30 days, or zero for unchecked
		var checkedOutAt time.Time
		var fetchedAt time.Time
		var confirmedAt time.Time
		if rand.Float32() < 0.85 {
			checkedOutAt = time.Now().Add(-time.Duration(rand.Intn(30*24)) * time.Hour).Add(-time.Duration(rand.Intn(60)) * time.Minute).UTC()
			fetchedAt = checkedOutAt.Add(-time.Duration(rand.Intn(60)) * time.Minute)
			if rand.Float32() < 0.5 {
				confirmedAt = checkedOutAt.Add(time.Duration(rand.Intn(30)) * time.Minute)
			}
		} else {
			// still set fetched_at for variety
			fetchedAt = time.Now().Add(-time.Duration(rand.Intn(24)) * time.Hour).UTC()
		}

		_, err := repo.CreateCheckin(ctx, checkin.Checkin{
			PlanningCenterID:      uuid.NewString(),
			LocationID:            loc.ID,
			EventID:               eventID,
			FirstName:             randomFirstName(),
			LastName:              randomLastName(),
			SecurityCode:          randomSecurityCode(),
			CheckedOutAt:          checkedOutAt,
			FetchedAt:             fetchedAt,
			CheckedOutConfirmedAt: confirmedAt,
		})
		if err != nil {
			return fmt.Errorf("creating checkin %d: %w", i, err)
		}
	}
	slog.Info("seeded checkins", slog.Int("count", count))
	return nil
}

func seedManualCheckins(ctx context.Context, database *sql.DB, count int) error {
	repo := manualcheckin.NewRepo(database)
	for i := 0; i < count; i++ {
		first := randomFirstName()
		last := randomLastName()

		var checkedOutAt time.Time
		var confirmedAt time.Time
		roll := rand.Float32()
		if roll < 0.4 {
			// checked out
			checkedOutAt = time.Now().Add(-time.Duration(rand.Intn(14*24)) * time.Hour).UTC()
			if rand.Float32() < 0.5 {
				confirmedAt = checkedOutAt.Add(time.Duration(rand.Intn(60)) * time.Minute)
			}
		} else if roll < 0.7 {
			// not checked out (zero time)
		} else {
			// checked out recently
			checkedOutAt = time.Now().Add(-time.Duration(rand.Intn(24)) * time.Hour).UTC()
		}

		_, err := repo.CreateManualCheckin(ctx, manualcheckin.ManualCheckin{
			FirstName:             first,
			LastName:              last,
			CheckedOutAt:          checkedOutAt,
			CheckedOutConfirmedAt: confirmedAt,
		})
		if err != nil {
			return fmt.Errorf("creating manual checkin %d: %w", i, err)
		}
	}
	slog.Info("seeded manual checkins", slog.Int("count", count))
	return nil
}

func seedGuestSubmissions(ctx context.Context, database *sql.DB, count int) error {
	repo := guestsubmission.NewRepo(database)
	for i := 0; i < count; i++ {
		parentFirst := randomFirstName()
		parentLast := randomLastName()
		phone := ""
		email := ""
		// ensure at least one of phone/email per parents CHECK
		switch rand.Intn(3) {
		case 0:
			phone = randomPhone()
		case 1:
			email = randomEmail(parentFirst, parentLast)
		default:
			phone = randomPhone()
			email = randomEmail(parentFirst, parentLast)
		}

		numChildren := rand.Intn(3) + 1 // 1-3
		children := make([]guestsubmission.Child, 0, numChildren)
		for c := 0; c < numChildren; c++ {
			children = append(children, guestsubmission.Child{
				FirstName: randomFirstName(),
				LastName:  randomLastName(),
				DOB:       randomDOB(),
				Grade:     randomGrade(),
			})
		}

		sub, err := repo.CreateSubmission(ctx, guestsubmission.Parent{
			FirstName: parentFirst,
			LastName:  parentLast,
			Phone:     phone,
			Email:     email,
		}, children)
		if err != nil {
			return fmt.Errorf("creating guest submission %d: %w", i, err)
		}

		// Distribute statuses: keep variety. Use UpdateSubmissionStatus for
		// approved/entered so standalone manual_checkins count stays exactly 20
		// (ApproveSubmission auto-creates linked manual_checkins which would
		// push the total above 20). UpdateSubmissionStatus keeps counts independent.
		r := rand.Float32()
		switch {
		case r < 0.4:
			// leave pending
		case r < 0.6:
			if err := repo.UpdateSubmissionStatus(ctx, sub.PublicID, guestsubmission.StatusApproved, time.Now().UTC()); err != nil {
				slog.Warn("failed to approve guest submission", slog.String("public_id", sub.PublicID), slog.String("error", err.Error()))
			}
		case r < 0.8:
			if err := repo.UpdateSubmissionStatus(ctx, sub.PublicID, guestsubmission.StatusEntered, time.Now().UTC()); err != nil {
				slog.Warn("failed to mark entered", slog.String("public_id", sub.PublicID), slog.String("error", err.Error()))
			}
		default:
			if err := repo.UpdateSubmissionStatus(ctx, sub.PublicID, guestsubmission.StatusRejected, time.Now().UTC()); err != nil {
				slog.Warn("failed to reject guest submission", slog.String("public_id", sub.PublicID), slog.String("error", err.Error()))
			}
		}

		// Randomize created_at to spread over last 30 days for more realistic metrics
		createdAt := time.Now().Add(-time.Duration(rand.Intn(30*24)) * time.Hour).UTC()
		_, err = database.ExecContext(ctx, `UPDATE guest_submissions SET created_at = ? WHERE public_id = ?`, createdAt, sub.PublicID)
		if err != nil {
			slog.Warn("failed to randomize guest submission created_at", slog.String("public_id", sub.PublicID), slog.String("error", err.Error()))
		}
		_, err = database.ExecContext(ctx, `UPDATE parents SET created_at = ? WHERE id = ?`, createdAt, sub.ParentID)
		if err != nil {
			slog.Warn("failed to randomize parent created_at", slog.String("error", err.Error()))
		}
	}
	slog.Info("seeded guest submissions", slog.Int("count", count))
	return nil
}
