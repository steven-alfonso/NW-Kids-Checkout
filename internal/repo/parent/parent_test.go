package parent

import (
	"database/sql"
	"log"
	"os"
	"testing"
	"time"

	"kids-checkin/internal/db"

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

func Test_sqliteRepo_CreateParent(t *testing.T) {
	_, err := squirrel.Delete("parents").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	s := NewRepo(testDB)

	created, err := s.CreateParent(t.Context(), Parent{
		FirstName: "John",
		LastName:  "Smith",
		Phone:     "555-1234",
		Email:     "john@example.com",
	})
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.WithinDuration(t, time.Now().UTC(), created.CreatedAt, 5*time.Second)

	var firstName, lastName, phone, email string
	err = testDB.QueryRowContext(t.Context(),
		"SELECT first_name, last_name, phone, email FROM parents WHERE id = ?", created.ID,
	).Scan(&firstName, &lastName, &phone, &email)
	require.NoError(t, err)
	assert.Equal(t, "John", firstName)
	assert.Equal(t, "Smith", lastName)
	assert.Equal(t, "555-1234", phone)
	assert.Equal(t, "john@example.com", email)
}
