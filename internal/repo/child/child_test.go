package child

import (
	"context"
	"database/sql"
	"log"
	"os"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/parent"

	"github.com/Masterminds/squirrel"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	tDB, cleanup, err := db.PrepareTestDB()
	if err != nil {
		log.Fatalf("Failed to prepare test DB: %v", err)
	}
	testDB = tDB

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func Test_sqliteRepo_CreateChild(t *testing.T) {
	_, err := squirrel.Delete("children").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	p, err := parent.NewRepo(testDB).CreateParent(t.Context(), parent.Parent{
		FirstName: "John",
		LastName:  "Smith",
		Phone:     "555-1234",
		Email:     "john@example.com",
	})
	require.NoError(t, err)

	s := NewRepo(testDB)
	created, err := s.CreateChild(context.Background(), Child{
		ParentID:  p.ID,
		FirstName: "Timmy",
		LastName:  "Smith",
		DOB:       "2020-01-01",
		Grade:     "1st Grade",
	})
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.WithinDuration(t, time.Now().UTC(), created.CreatedAt, 5*time.Second)

	var parentID int64
	var firstName, lastName, dob, grade string
	err = testDB.QueryRowContext(t.Context(),
		"SELECT parent_id, first_name, last_name, dob, grade FROM children WHERE id = ?", created.ID,
	).Scan(&parentID, &firstName, &lastName, &dob, &grade)
	require.NoError(t, err)
	assert.Equal(t, p.ID, parentID)
	assert.Equal(t, "Timmy", firstName)
	assert.Equal(t, "Smith", lastName)
	assert.Equal(t, "2020-01-01", dob)
	assert.Equal(t, "1st Grade", grade)
}
