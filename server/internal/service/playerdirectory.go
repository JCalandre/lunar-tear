package service

// PlayerCard is the common currency for friend/arena list rows.
type PlayerCard struct {
	PlayerId          int64
	Name              string
	Level             int32
	MaxDeckPower      int32
	FavoriteCostumeId int32
	PvpPoint          int32
	IsBot             bool
}
