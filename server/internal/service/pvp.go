package service

import (
	"context"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
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

const matchingCount = 5

func (s *PvpServiceServer) buildMatching(user *store.UserState) []store.MatchingEntry {
	cards := s.dir.RealPlayersNear(user.PlayerId, user.Pvp.PvpPoint, matchingCount)
	cards = s.dir.FillWithBots(cards, matchingCount, user.PlayerId, gametime.NowMillis(), user.Pvp.PvpPoint)
	out := make([]store.MatchingEntry, 0, len(cards))
	for _, c := range cards {
		rank, _ := s.snaps.RankOfPlayer(c.PlayerId)
		out = append(out, store.MatchingEntry{
			PlayerId: c.PlayerId, Name: c.Name, PvpPoint: c.PvpPoint, Rank: int32(rank),
			DeckPower: c.MaxDeckPower, IsBot: c.IsBot, MostPowerfulCostumeId: c.FavoriteCostumeId,
		})
	}
	return out
}

func matchingToProto(entries []store.MatchingEntry) []*pb.MatchingOpponent {
	var out []*pb.MatchingOpponent
	for _, e := range entries {
		out = append(out, &pb.MatchingOpponent{
			PlayerId: e.PlayerId, Name: e.Name, PvpPoint: e.PvpPoint, Rank: e.Rank,
			DeckPower: e.DeckPower, MostPowerfulCostumeId: e.MostPowerfulCostumeId,
		})
	}
	return out
}

func (s *PvpServiceServer) GetMatchingList(ctx context.Context, _ *emptypb.Empty) (*pb.GetMatchingListResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	after, _ := s.users.UpdateUser(userId, func(u *store.UserState) {
		if len(u.PvpMatching) == 0 {
			u.PvpMatching = s.buildMatching(u)
		}
	})
	return &pb.GetMatchingListResponse{Matching: matchingToProto(after.PvpMatching)}, nil
}

func (s *PvpServiceServer) UpdateMatchingList(ctx context.Context, _ *emptypb.Empty) (*pb.UpdateMatchingListResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	after, _ := s.users.UpdateUser(userId, func(u *store.UserState) {
		u.PvpMatching = s.buildMatching(u)
	})
	return &pb.UpdateMatchingListResponse{Matching: matchingToProto(after.PvpMatching)}, nil
}
