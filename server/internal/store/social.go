package store

// FriendEdge is one entry in a user's friend list. Keyed by the friend's playerId
// in UserState.Friends. Cheer flags are reset daily (see service layer).
type FriendEdge struct {
	PlayerId             int64
	BecameFriendsAt      int64 // millis
	CheerSentToday       bool  // I have cheered them today
	CheerReceivedPending bool  // they cheered me; I can still collect the reward
	StaminaReceivedToday bool  // I have collected the cheer reward today
	LastResetDay         int64 // gametime.StartOfDayMillis() bucket of last reset
	LatestVersion        int64
}

// FriendRequest is a pending request, keyed by the other player's id in
// UserState.IncomingFriendRequests / OutgoingFriendRequests.
type FriendRequest struct {
	PlayerId      int64
	RequestedAt   int64 // millis
	LatestVersion int64
}

// PvpState holds a user's Arena standing and counters.
type PvpState struct {
	PvpPoint         int32
	AttackWinCount   int32
	AttackLoseCount  int32
	DefenseWinCount  int32
	DefenseLoseCount int32
	LastFinishDay    int64
	LatestVersion    int64
}

// BattleLogEntry is one row of attack or defense history (capped to the most recent N).
type BattleLogEntry struct {
	Seq               int64 // monotonic per-user ordering key (use nowMillis at insert)
	OpponentPlayerId  int64
	OpponentName      string
	OpponentPvpPoint  int32
	OpponentDeckPower int32
	IsVictory         bool
	BattleDatetime    int64 // millis
	FluctuatedPoint   int32
	Rank              int32
}

// MatchingEntry is a cached opponent shown in the current matching list, so
// StartBattle can recall the chosen opponent's snapshot/bot identity.
type MatchingEntry struct {
	PlayerId              int64
	Name                  string
	PvpPoint              int32
	Rank                  int32
	DeckPower             int32
	IsBot                 bool
	MostPowerfulCostumeId int32
}
