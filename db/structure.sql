CREATE TABLE location_groups
(
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);
CREATE TABLE locations
(
    id                        INTEGER PRIMARY KEY,
    planning_center_id        TEXT    NOT NULL UNIQUE,
    planning_center_parent_id TEXT     DEFAULT NULL,
    location_group_id         INTEGER  DEFAULT NULL,
    event_id                  INTEGER NOT NULL,
    name                      TEXT    NOT NULL,
    auto_fetch                INTEGER  DEFAULT 0,
    last_checked_out_time     DATETIME DEFAULT NULL
);
CREATE INDEX idx_name ON locations (name);
CREATE UNIQUE INDEX idx_planning_center_id ON locations (planning_center_id);
CREATE TABLE checkins
(
    id                 INTEGER PRIMARY KEY,
    planning_center_id TEXT    NOT NULL UNIQUE,
    location_id        INTEGER NOT NULL,
    first_name         TEXT    NOT NULL,
    last_name          TEXT    NOT NULL,
    security_code      TEXT    NOT NULL,
    checked_out_at     DATETIME DEFAULT NULL
, checked_out_confirmed_at DATETIME DEFAULT NULL, event_id DEFAULT NULL, fetched_at DATETIME DEFAULT NULL);
CREATE INDEX idx_checked_out_at ON checkins (checked_out_at);
CREATE TABLE events
(
    id                 INTEGER PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    planning_center_id TEXT NOT NULL
, auto_fetch INTEGER DEFAULT 0, last_checked_out_time DATETIME DEFAULT NULL, location_group_id INTEGER REFERENCES location_groups (id));
CREATE UNIQUE INDEX idx_location_groups_name ON location_groups (name);
CREATE INDEX idx_unique_pcid ON events(planning_center_id);
CREATE TABLE event_check_windows
(
    id                INTEGER PRIMARY KEY,
    event_id          INTEGER NOT NULL REFERENCES events (id) ON DELETE CASCADE ON UPDATE CASCADE,
    start_day_of_week INTEGER NOT NULL CHECK (start_day_of_week BETWEEN 1 AND 7),
    start_time        TEXT    NOT NULL,
    end_day_of_week   INTEGER NOT NULL CHECK (end_day_of_week BETWEEN 1 AND 7),
    end_time          TEXT    NOT NULL,
    timezone          TEXT    NOT NULL
);
CREATE INDEX idx_event_check_windows_event_id ON event_check_windows (event_id);
CREATE TABLE parents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    email TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    CHECK (phone <> '' OR email <> '')
);
CREATE TABLE children (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL REFERENCES parents(id),
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    dob TEXT NOT NULL,
    grade TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE TABLE guest_submissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    public_id TEXT NOT NULL UNIQUE,
    parent_id INTEGER NOT NULL REFERENCES parents(id),
    approved_at DATETIME NULL,
    rejected_at DATETIME NULL,
    entered_at DATETIME NULL,
    checkins_backfilled_at DATETIME NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX idx_children_parent_id ON children(parent_id);
CREATE TABLE "manual_checkins" (
    id INTEGER PRIMARY KEY,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    public_id TEXT,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    checked_out_at DATETIME DEFAULT NULL,
    checked_out_confirmed_at DATETIME DEFAULT NULL,
    child_id INTEGER NULL REFERENCES children(id),
    -- Either child_id is set (linked to a child record) OR first_name and
    -- last_name must be non-empty (standalone guest checkin without a child record).
    CHECK (child_id IS NOT NULL OR (LENGTH(TRIM(first_name)) > 0 AND LENGTH(TRIM(last_name)) > 0))
);
CREATE INDEX idx_manual_checked_out_at ON manual_checkins (checked_out_at);
CREATE INDEX idx_manual_checkins_child_id ON manual_checkins(child_id);
CREATE INDEX idx_guest_submissions_created_at ON guest_submissions(created_at);
CREATE INDEX idx_guest_submissions_parent_id ON guest_submissions(parent_id);
