# Task 7: Fix Non-Idempotent Migration with Dead `ALTER TABLE`

## Context

In `db/migrations/20260825030013_add_guest_family_model.up.sqlite`, line 33 does:
```sql
ALTER TABLE manual_checkins ADD COLUMN child_id INTEGER NULL REFERENCES children(id);
```
Then immediately creates a new `manual_checkins_new` table (lines 35-45) with `child_id` already included, copies data (line 47-48), drops the old table (line 51), and renames (line 52). The `ALTER TABLE` on line 33 is dead work — the old table is immediately replaced. The migration is also non-idempotent because running it twice would fail on `ALTER TABLE ADD COLUMN` (column already exists).

## Step-by-step plan

### Step 1: Write failing test (TDD red phase)
Create `db/migration_test.go` with a test that:
1. Creates an in-memory SQLite DB
2. Applies all `.up.sqlite` migrations in order
3. Inserts data into `manual_checkins` and verifies the CHECK constraint works

The test should pass even with the dead line removed (it tests the *correct* behavior, not the bug).

### Step 2: Fix the migration
Remove line 33 from `db/migrations/20260825030013_add_guest_family_model.up.sqlite`.

### Step 3: Run test (TDD green phase)
Run `go test ./db/...` to confirm the test passes.

### Step 4: Run `go test ./...` to verify no regressions
Ensure all existing tests still pass.

### Step 5: Regenerate `db/structure.sql`
Run `make db-migrate` to regenerate from the fixed migration. (Check if `migrate` tool is available; if not, note it for manual regen.)

## Files to modify
- `db/migrations/20260825030013_add_guest_family_model.up.sqlite` (remove line 33)
- `db/migration_test.go` (new file)
- `db/structure.sql` (regenerated)
