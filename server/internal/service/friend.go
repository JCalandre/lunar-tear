package service

import (
	"context"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/store"

	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

type FriendServiceServer struct {
	pb.UnimplementedFriendServiceServer
	users    store.UserRepository
	sessions store.SessionRepository
	dir      *PlayerDirectory
}

func NewFriendServiceServer(users store.UserRepository, sessions store.SessionRepository, dir *PlayerDirectory) *FriendServiceServer {
	return &FriendServiceServer{users: users, sessions: sessions, dir: dir}
}

func (s *FriendServiceServer) cardFor(playerId int64) (PlayerCard, bool) {
	if s.dir.IsBot(playerId) {
		return botCardFromId(s.dir.pools(), playerId), true
	}
	snap, err := s.dir.snaps.GetSnapshot(playerId)
	if err != nil {
		return PlayerCard{}, false
	}
	return cardFromSnapshot(snap), true
}

func (s *FriendServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	card, ok := s.cardFor(req.PlayerId)
	if !ok {
		return &pb.GetUserResponse{}, nil
	}
	return &pb.GetUserResponse{User: userProto(card)}, nil
}

func (s *FriendServiceServer) GetFriendList(ctx context.Context, req *pb.GetFriendListRequest) (*pb.GetFriendListResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.GetFriendListResponse{}, nil
	}
	maybeResetCheerDay(&user)
	var friends []*pb.FriendUser
	var sent, received int32
	for pid, edge := range user.Friends {
		card, ok := s.cardFor(pid)
		if !ok {
			continue
		}
		friends = append(friends, friendUserProto(card, edge))
		if edge.CheerSentToday {
			sent++
		}
		if edge.CheerReceivedPending {
			received++
		}
	}
	return &pb.GetFriendListResponse{
		FriendUser:         friends,
		SendCheerCount:     sent,
		ReceivedCheerCount: received,
	}, nil
}

func (s *FriendServiceServer) GetFriendRequestList(ctx context.Context, req *emptypb.Empty) (*pb.GetFriendRequestListResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.GetFriendRequestListResponse{}, nil
	}
	var users []*pb.User
	for pid := range user.IncomingFriendRequests {
		if card, ok := s.cardFor(pid); ok {
			users = append(users, userProto(card))
		}
	}
	return &pb.GetFriendRequestListResponse{User: users}, nil
}

func (s *FriendServiceServer) SearchRecommendedUsers(ctx context.Context, req *emptypb.Empty) (*pb.SearchRecommendedUsersResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.SearchRecommendedUsersResponse{}, nil
	}
	cards := s.dir.RealPlayersNear(user.PlayerId, user.Pvp.PvpPoint, 20)
	filtered := make([]PlayerCard, 0, len(cards))
	for _, c := range cards {
		if _, f := user.Friends[c.PlayerId]; f {
			continue
		}
		if _, q := user.OutgoingFriendRequests[c.PlayerId]; q {
			continue
		}
		filtered = append(filtered, c)
	}
	filtered = s.dir.FillWithBots(filtered, 10, user.PlayerId, dayBucket(), user.Pvp.PvpPoint)
	var out []*pb.User
	for _, c := range filtered {
		out = append(out, userProto(c))
	}
	return &pb.SearchRecommendedUsersResponse{Users: out}, nil
}
