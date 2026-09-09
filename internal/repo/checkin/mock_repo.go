package checkin

import (
	"context"
	"sync/atomic"
	"time"
)

type MockRepo struct {
	ListCheckinsFunc          func(ctx context.Context, filter Filter) ([]Checkin, error)
	ListCheckinsFuncCallCount atomic.Int64

	CreateCheckinFunc          func(ctx context.Context, checkin Checkin) (Checkin, error)
	CreateCheckinFuncCallCount atomic.Int64

	SetCheckedOutConfirmedAtFunc          func(ctx context.Context, planningCenterID string, confirmed bool) (Checkin, error)
	SetCheckedOutConfirmedAtFuncCallCount atomic.Int64

	RemoveOldCheckinsFunc          func(ctx context.Context, olderThan time.Time) (deletedCount int64, err error)
	RemoveOldCheckinsFuncCallCount atomic.Int64

	DeleteCheckinFunc          func(ctx context.Context, id int64) error
	DeleteCheckinFuncCallCount atomic.Int64

	DeleteAllCheckinsFunc          func(ctx context.Context) (int64, error)
	DeleteAllCheckinsFuncCallCount atomic.Int64
}

func (repo *MockRepo) ListCheckins(ctx context.Context, filter Filter) ([]Checkin, error) {
	repo.ListCheckinsFuncCallCount.Add(1)
	if repo.ListCheckinsFunc != nil {
		return repo.ListCheckinsFunc(ctx, filter)
	}

	panic("MockRepo.ListCheckins not implemented")
}

func (repo *MockRepo) CreateCheckin(ctx context.Context, checkin Checkin) (Checkin, error) {
	repo.CreateCheckinFuncCallCount.Add(1)
	if repo.CreateCheckinFunc != nil {
		return repo.CreateCheckinFunc(ctx, checkin)
	}

	panic("MockRepo.CreateCheckin not implemented")
}

func (repo *MockRepo) SetCheckedOutConfirmedAt(ctx context.Context, planningCenterID string, confirmed bool) (Checkin, error) {
	repo.SetCheckedOutConfirmedAtFuncCallCount.Add(1)
	if repo.SetCheckedOutConfirmedAtFunc != nil {
		return repo.SetCheckedOutConfirmedAtFunc(ctx, planningCenterID, confirmed)
	}

	panic("MockRepo.SetCheckedOutConfirmedAt not implemented")
}

func (repo *MockRepo) RemoveOldCheckins(ctx context.Context, olderThan time.Time) (deletedCount int64, err error) {
	repo.RemoveOldCheckinsFuncCallCount.Add(1)
	if repo.RemoveOldCheckinsFunc != nil {
		return repo.RemoveOldCheckinsFunc(ctx, olderThan)
	}

	panic("MockRepo.RemoveOldCheckins not implemented")
}

func (repo *MockRepo) DeleteCheckin(ctx context.Context, id int64) error {
	repo.DeleteCheckinFuncCallCount.Add(1)
	if repo.DeleteCheckinFunc != nil {
		return repo.DeleteCheckinFunc(ctx, id)
	}

	panic("MockRepo.DeleteCheckin not implemented")
}

func (repo *MockRepo) DeleteAllCheckins(ctx context.Context) (int64, error) {
	repo.DeleteAllCheckinsFuncCallCount.Add(1)
	if repo.DeleteAllCheckinsFunc != nil {
		return repo.DeleteAllCheckinsFunc(ctx)
	}

	panic("MockRepo.DeleteAllCheckins not implemented")
}
