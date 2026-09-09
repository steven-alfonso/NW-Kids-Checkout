package manualcheckin

import (
	"context"
	"time"
)

type MockRepo struct {
	ListManualCheckinsFunc          func(ctx context.Context, filter Filter) ([]ManualCheckin, error)
	ListManualCheckinsFuncCallCount int

	CreateManualCheckinFunc          func(ctx context.Context, manualCheckin ManualCheckin) (ManualCheckin, error)
	CreateManualCheckinFuncCallCount int

	SetManualCheckedOutAtFunc          func(ctx context.Context, id int64, checkedOut bool) (ManualCheckin, error)
	SetManualCheckedOutAtFuncCallCount int

	SetManualCheckedOutConfirmedAtFunc          func(ctx context.Context, id int64, confirmed bool) (ManualCheckin, error)
	SetManualCheckedOutConfirmedAtFuncCallCount int

	RemoveOldManualCheckinsFunc          func(ctx context.Context, olderThan time.Time) (deletedCount int64, err error)
	RemoveOldManualCheckinsFuncCallCount int

	DeleteAllManualCheckinsFunc          func(ctx context.Context) (int64, error)
	DeleteAllManualCheckinsFuncCallCount int
}

func (repo *MockRepo) ListManualCheckins(ctx context.Context, filter Filter) ([]ManualCheckin, error) {
	repo.ListManualCheckinsFuncCallCount++
	if repo.ListManualCheckinsFunc != nil {
		return repo.ListManualCheckinsFunc(ctx, filter)
	}

	panic("MockRepo.ListManualCheckins not implemented")
}

func (repo *MockRepo) CreateManualCheckin(ctx context.Context, manualCheckin ManualCheckin) (ManualCheckin, error) {
	repo.CreateManualCheckinFuncCallCount++
	if repo.CreateManualCheckinFunc != nil {
		return repo.CreateManualCheckinFunc(ctx, manualCheckin)
	}

	panic("MockRepo.CreateManualCheckin not implemented")
}

func (repo *MockRepo) SetManualCheckedOutConfirmedAt(ctx context.Context, id int64, confirmed bool) (ManualCheckin, error) {
	repo.SetManualCheckedOutConfirmedAtFuncCallCount++
	if repo.SetManualCheckedOutConfirmedAtFunc != nil {
		return repo.SetManualCheckedOutConfirmedAtFunc(ctx, id, confirmed)
	}

	panic("MockRepo.SetManualCheckedOutConfirmedAt not implemented")
}

func (repo *MockRepo) SetManualCheckedOutAt(ctx context.Context, id int64, checkedOut bool) (ManualCheckin, error) {
	repo.SetManualCheckedOutAtFuncCallCount++
	if repo.SetManualCheckedOutAtFunc != nil {
		return repo.SetManualCheckedOutAtFunc(ctx, id, checkedOut)
	}

	panic("MockRepo.SetManualCheckedOutAt not implemented")
}

func (repo *MockRepo) RemoveOldManualCheckins(ctx context.Context, olderThan time.Time) (deletedCount int64, err error) {
	repo.RemoveOldManualCheckinsFuncCallCount++
	if repo.RemoveOldManualCheckinsFunc != nil {
		return repo.RemoveOldManualCheckinsFunc(ctx, olderThan)
	}

	panic("MockRepo.RemoveOldManualCheckins not implemented")
}

func (repo *MockRepo) DeleteAllManualCheckins(ctx context.Context) (int64, error) {
	repo.DeleteAllManualCheckinsFuncCallCount++
	if repo.DeleteAllManualCheckinsFunc != nil {
		return repo.DeleteAllManualCheckinsFunc(ctx)
	}

	panic("MockRepo.DeleteAllManualCheckins not implemented")
}
