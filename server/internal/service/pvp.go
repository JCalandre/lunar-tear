package service

import (
	"context"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/runtime"
	"lunar-tear/server/internal/store"

	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

type PvpServiceServer struct {
	pb.UnimplementedPvpServiceServer
	users    store.UserRepository
	sessions store.SessionRepository
	snaps    store.SnapshotRepository
	dir      *PlayerDirectory
	holder   *runtime.Holder
}

func NewPvpServiceServer(users store.UserRepository, sessions store.SessionRepository, snaps store.SnapshotRepository, dir *PlayerDirectory, holder *runtime.Holder) *PvpServiceServer {
	return &PvpServiceServer{users: users, sessions: sessions, snaps: snaps, dir: dir, holder: holder}
}

const currentSeasonId int32 = 1 // single fixed season for the core; rollover deferred

func (s *PvpServiceServer) GetTopData(ctx context.Context, _ *emptypb.Empty) (*pb.GetTopDataResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.LoadUser(userId)
	rank, _ := s.snaps.RankOfPlayer(user.PlayerId)
	if rank == 0 {
		rank = 1
	}
	return &pb.GetTopDataResponse{
		CurrentSeasonId: currentSeasonId,
		PvpPoint:        user.Pvp.PvpPoint,
		Rank:            int32(rank),
	}, nil
}

// pointDelta is the rank-aware change for one match. Win: base + bonus for beating a stronger
// opponent (capped). Loss: a small fixed penalty (caller floors total at 0).
func pointDelta(myPoint, oppPoint int32, victory bool) int32 {
	if victory {
		d := int32(20)
		if oppPoint > myPoint {
			d += (oppPoint - myPoint) / 50
			if d > 50 {
				d = 50
			}
		}
		return d
	}
	return -10
}

func applyPointDelta(cur, delta int32) int32 {
	v := cur + delta
	if v < 0 {
		return 0
	}
	return v
}
