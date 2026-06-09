package service

import (
	"context"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
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
	if len(filtered) > 10 {
		filtered = filtered[:10]
	}
	var out []*pb.User
	for _, c := range filtered {
		out = append(out, userProto(c))
	}
	return &pb.SearchRecommendedUsersResponse{Users: out}, nil
}

func (s *FriendServiceServer) SendFriendRequest(ctx context.Context, req *pb.SendFriendRequestRequest) (*pb.SendFriendRequestResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	self, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.SendFriendRequestResponse{}, nil
	}
	target := req.PlayerId
	if target == self.PlayerId {
		return &pb.SendFriendRequestResponse{}, nil
	}
	now := gametime.NowMillis()

	if s.dir.IsBot(target) {
		s.users.UpdateUser(userId, func(u *store.UserState) {
			u.Friends[target] = store.FriendEdge{PlayerId: target, BecameFriendsAt: now,
				CheerReceivedPending: true, LastResetDay: dayBucket()}
			delete(u.OutgoingFriendRequests, target)
		})
		return &pb.SendFriendRequestResponse{}, nil
	}

	if _, already := self.Friends[target]; already {
		return &pb.SendFriendRequestResponse{}, nil
	}
	s.users.UpdateUser(userId, func(u *store.UserState) {
		u.OutgoingFriendRequests[target] = store.FriendRequest{PlayerId: target, RequestedAt: now}
	})
	targetUserId, err := s.userIdForPlayer(target)
	if err == nil {
		s.users.UpdateUser(targetUserId, func(u *store.UserState) {
			u.IncomingFriendRequests[self.PlayerId] = store.FriendRequest{PlayerId: self.PlayerId, RequestedAt: now}
			u.Notifications.FriendRequestReceiveCount = int32(len(u.IncomingFriendRequests))
		})
	}
	return &pb.SendFriendRequestResponse{}, nil
}

func (s *FriendServiceServer) AcceptFriendRequest(ctx context.Context, req *pb.AcceptFriendRequestRequest) (*pb.AcceptFriendRequestResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	self, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.AcceptFriendRequestResponse{}, nil
	}
	other := req.PlayerId
	if _, pending := self.IncomingFriendRequests[other]; !pending {
		return &pb.AcceptFriendRequestResponse{}, nil
	}
	now := gametime.NowMillis()
	s.users.UpdateUser(userId, func(u *store.UserState) {
		delete(u.IncomingFriendRequests, other)
		u.Friends[other] = store.FriendEdge{PlayerId: other, BecameFriendsAt: now, LastResetDay: dayBucket()}
		u.Notifications.FriendRequestReceiveCount = int32(len(u.IncomingFriendRequests))
	})
	if otherUserId, err := s.userIdForPlayer(other); err == nil {
		s.users.UpdateUser(otherUserId, func(u *store.UserState) {
			delete(u.OutgoingFriendRequests, self.PlayerId)
			u.Friends[self.PlayerId] = store.FriendEdge{PlayerId: self.PlayerId, BecameFriendsAt: now, LastResetDay: dayBucket()}
		})
	}
	return &pb.AcceptFriendRequestResponse{}, nil
}

func (s *FriendServiceServer) DeclineFriendRequest(ctx context.Context, req *pb.DeclineFriendRequestRequest) (*pb.DeclineFriendRequestResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	s.users.UpdateUser(userId, func(u *store.UserState) {
		for _, pid := range req.PlayerId {
			delete(u.IncomingFriendRequests, pid)
		}
		u.Notifications.FriendRequestReceiveCount = int32(len(u.IncomingFriendRequests))
	})
	return &pb.DeclineFriendRequestResponse{}, nil
}

func (s *FriendServiceServer) DeleteFriend(ctx context.Context, req *pb.DeleteFriendRequest) (*pb.DeleteFriendResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	self, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.DeleteFriendResponse{}, nil
	}
	other := req.PlayerId
	s.users.UpdateUser(userId, func(u *store.UserState) { delete(u.Friends, other) })
	if !s.dir.IsBot(other) {
		if otherUserId, err := s.userIdForPlayer(other); err == nil {
			s.users.UpdateUser(otherUserId, func(u *store.UserState) { delete(u.Friends, self.PlayerId) })
		}
	}
	return &pb.DeleteFriendResponse{}, nil
}

// userIdForPlayer maps a real playerId to its userId. The server assigns player_id = user_id
// at creation, so this is identity for real players (and validates the user exists).
func (s *FriendServiceServer) userIdForPlayer(playerId int64) (int64, error) {
	if _, err := s.users.LoadUser(playerId); err != nil {
		return 0, err
	}
	return playerId, nil
}
