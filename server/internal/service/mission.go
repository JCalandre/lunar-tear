package service

import (
	"context"
	"log"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/runtime"
	"lunar-tear/server/internal/store"
)

type MissionServiceServer struct {
	pb.UnimplementedMissionServiceServer
	users    store.UserRepository
	sessions store.SessionRepository
	holder   *runtime.Holder
}

func NewMissionServiceServer(users store.UserRepository, sessions store.SessionRepository, holder *runtime.Holder) *MissionServiceServer {
	return &MissionServiceServer{users: users, sessions: sessions, holder: holder}
}

func (s *MissionServiceServer) UpdateMissionProgress(ctx context.Context, req *pb.UpdateMissionProgressRequest) (*pb.UpdateMissionProgressResponse, error) {
	log.Printf("[MissionService] UpdateMissionProgress: cage=%v pictureBook=%v", req.CageMeasurableValues, req.PictureBookMeasurableValues)
	// Missions are populated/cleared by the complete-missions tool and claimed
	// via ReceiveMissionRewardsById; the measurable values delivered here are
	// acknowledged so the client proceeds.
	return &pb.UpdateMissionProgressResponse{}, nil
}

// ReceiveMissionRewardsById grants the reward items for each cleared mission the
// client asks to claim, flips the mission to RewardReceived, and lets the diff
// interceptor publish the resulting inventory + mission changes.
func (s *MissionServiceServer) ReceiveMissionRewardsById(ctx context.Context, req *pb.ReceiveMissionRewardsByIdRequest) (*pb.ReceiveMissionRewardsResponse, error) {
	log.Printf("[MissionService] ReceiveMissionRewardsById: missionIds=%v", req.GetMissionId())

	userId := CurrentUserId(ctx, s.users, s.sessions)
	cat := s.holder.Get()
	granter := cat.QuestHandler.Granter
	missions := cat.Mission

	nowMillis := gametime.NowMillis()
	received := make([]*pb.MissionReward, 0)

	_, err := s.users.UpdateUser(userId, func(user *store.UserState) {
		for _, missionId := range req.GetMissionId() {
			m, ok := missions.Missions[missionId]
			if !ok {
				log.Printf("[MissionService] unknown missionId=%d", missionId)
				continue
			}
			state, exists := user.Missions[missionId]
			if !exists {
				log.Printf("[MissionService] user=%d has no row for mission=%d", userId, missionId)
				continue
			}
			if state.MissionProgressStatusType == int32(model.MissionProgressStatusTypeRewardReceived) {
				log.Printf("[MissionService] mission=%d already claimed by user=%d", missionId, userId)
				continue
			}
			if state.MissionProgressStatusType < int32(model.MissionProgressStatusTypeClear) {
				log.Printf("[MissionService] mission=%d not cleared (status=%d) for user=%d; skipping claim", missionId, state.MissionProgressStatusType, userId)
				continue
			}

			for _, r := range missions.RewardsByGroupId[m.MissionRewardId] {
				granter.GrantFull(user, model.PossessionType(r.PossessionType), r.PossessionId, r.Count, nowMillis)
				received = append(received, &pb.MissionReward{
					PossessionType: r.PossessionType,
					PossessionId:   r.PossessionId,
					Count:          r.Count,
				})
			}

			state.MissionProgressStatusType = int32(model.MissionProgressStatusTypeRewardReceived)
			state.LatestVersion = nowMillis
			user.Missions[missionId] = state
		}
	})
	if err != nil {
		log.Printf("[MissionService] ReceiveMissionRewardsById: update user=%d failed: %v", userId, err)
		return &pb.ReceiveMissionRewardsResponse{}, nil
	}

	return &pb.ReceiveMissionRewardsResponse{
		ReceivedPossession: received,
		ExpiredPossession:  []*pb.MissionReward{},
		OverflowPossession: []*pb.MissionReward{},
	}, nil
}

// ReceiveMissionPassRewards grants the reward tiers configured for a mission
// pass. NOTE: mission-pass point/level tracking is not yet modelled, so this
// grants the pass's configured reward tiers when called. The diff interceptor
// publishes the inventory changes.
func (s *MissionServiceServer) ReceiveMissionPassRewards(ctx context.Context, req *pb.ReceiveMissionPassRewardsRequest) (*pb.ReceiveMissionPassRewardsResponse, error) {
	log.Printf("[MissionService] ReceiveMissionPassRewards: missionPassId=%d", req.GetMissionPassId())

	userId := CurrentUserId(ctx, s.users, s.sessions)
	cat := s.holder.Get()
	granter := cat.QuestHandler.Granter
	missions := cat.Mission

	pass, ok := missions.PassById[req.GetMissionPassId()]
	if !ok {
		log.Printf("[MissionService] unknown missionPassId=%d", req.GetMissionPassId())
		return &pb.ReceiveMissionPassRewardsResponse{}, nil
	}

	nowMillis := gametime.NowMillis()
	received := make([]*pb.MissionPassReward, 0)

	_, err := s.users.UpdateUser(userId, func(user *store.UserState) {
		for _, tier := range missions.PassRewardGroup[pass.MissionPassRewardGroupId] {
			granter.GrantFull(user, model.PossessionType(tier.PossessionType), tier.PossessionId, tier.Count, nowMillis)
			received = append(received, &pb.MissionPassReward{
				PossessionType: tier.PossessionType,
				PossessionId:   tier.PossessionId,
				Count:          tier.Count,
			})
		}
	})
	if err != nil {
		log.Printf("[MissionService] ReceiveMissionPassRewards: update user=%d failed: %v", userId, err)
		return &pb.ReceiveMissionPassRewardsResponse{}, nil
	}

	return &pb.ReceiveMissionPassRewardsResponse{
		ReceivedPossession: received,
		OverflowPossession: []*pb.MissionPassReward{},
	}, nil
}
