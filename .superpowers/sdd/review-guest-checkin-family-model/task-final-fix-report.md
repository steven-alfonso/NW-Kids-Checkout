# Final Fix Report: Review findings 1-4 (With fixes)

## Summary
Addressed 4 Important findings from final whole-branch review (HEAD 6eb3824 vs base 3cea385) with minimal changes: wrapped zero-child error to 400, documented limit cap, documented manual-checkin name strictness, and extracted shared squirrel Expr helpers.

## Changes
- `internal/repo/guestsubmission/guestsubmission.go:77-81` - Added `ErrInvalidSubmission = errors.New("invalid submission")` sentinel alongside `ErrConflict`/`ErrInvalidStatus`.
- `internal/repo/guestsubmission/guestsubmission.go:503-504` - Changed `errors.New("submission has no children")` to `fmt.Errorf("%w: submission has no children", ErrInvalidSubmission)` so callers can distinguish data error via `errors.Is`.
- `internal/repo/guestsubmission/guestsubmission.go:218-219` - Replaced inline `squirrel.Expr("EXISTS ... NOT EXISTS ...")` in `ListSubmissions` with `withoutManualCheckinsExpr()` helper.
- `internal/repo/guestsubmission/guestsubmission.go:474` - Replaced inline `squirrel.Expr("NOT EXISTS ...")` in `insertManualCheckins` with `childWithoutManualCheckinExpr()` helper.
- `internal/repo/guestsubmission/guestsubmission.go:518-529` - Added helpers `withoutManualCheckinsExpr()` and `childWithoutManualCheckinExpr()` with comment linking shared semantics (both test for absence of manual_checkins row via NOT EXISTS; see also sibling helper).
- `internal/controllers/guestcheckinv1/guest_submission.go:280-282` - `PatchSubmissionStatus` now maps `ErrInvalidStatus` and `ErrInvalidSubmission` to 400 alongside existing `ErrConflict`/`ErrNotFound` handling; covers `ApproveSubmission:insertManualCheckins` zero-child path.
- `internal/controllers/guestcheckinv1/guest_submission.go:308` - `CreateSubmissionCheckins` now maps both `ErrInvalidStatus` and `ErrInvalidSubmission` to 400 (previously only ErrInvalidStatus), so zero-child returns 400 not 500.
- `internal/controllers/guestcheckinv1/guest_submission.go:347` - Added `// limit capped at 200, default 100` comment above cap in `buildFilter`; retains cap behavior per Task 9 report (minimal documented-cap choice).
- `internal/repo/manualcheckin/manualcheckin.go:157` - Added `// Names required even with child_id to ensure standalone guest checkin data completeness; stricter than DB CHECK which permits blank names with child_id` comment above TrimSpace validation.

## Verification
- `gofmt -l .` => empty
- `godotenv go test ./...` => all packages PASS (guestsubmission 0.48s, guestcheckinv1 0.326s, manualcheckin 0.820s, etc.)
- `npx vitest run` => 13 test files, 124 tests PASS
- Manual check: `errors.Is(fmt.Errorf("%w: submission has no children", ErrInvalidSubmission), ErrInvalidSubmission)` true; controller returns 400 for both sentinels.

## Commits
- `fix(review): wrap zero-child error, document limit cap and name strictness, extract expr helpers`

## Concerns
- None. Behavior change is intentionally 500->400 for zero-child invalid data case. Helpers are unexported and SQL-identical to prior inline Expr.
