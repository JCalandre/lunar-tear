package service

import (
	"hash/fnv"
	"math/rand"
	"sort"

	pb "lunar-tear/server/gen/proto"
)

// BotIdBase is the reserved high range for bot player ids; real ids never reach it.
const BotIdBase int64 = 1 << 40

func IsBotId(playerId int64) bool { return playerId >= BotIdBase }

// botPools holds sorted, stable id slices sampled to build bots.
type botPools struct {
	costumeIds   []int32
	weaponIds    []int32
	companionIds []int32
}

func seedFrom(viewerId int64, slot int, dayBucket int64) int64 {
	h := fnv.New64a()
	var buf [8]byte
	for _, v := range []int64{viewerId, int64(slot), dayBucket} {
		for i := 0; i < 8; i++ {
			buf[i] = byte(v >> (8 * i))
		}
		h.Write(buf[:])
	}
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

func pick(r *rand.Rand, ids []int32, fallback int32) int32 {
	if len(ids) == 0 {
		return fallback
	}
	return ids[r.Intn(len(ids))]
}

// synthBot builds a deterministic bot PlayerCard near targetPoint.
func synthBot(pools botPools, viewerId int64, slot int, dayBucket int64, targetPoint int32) PlayerCard {
	seed := seedFrom(viewerId, slot, dayBucket)
	r := rand.New(rand.NewSource(seed))
	costumeId := pick(r, pools.costumeIds, 1)
	base := targetPoint
	if base < 1000 {
		base = 1000
	}
	delta := int32(r.Intn(int(base/6+1))) - base/12
	power := base + delta
	if power < 100 {
		power = 100
	}
	return PlayerCard{
		PlayerId:          BotIdBase + seed%1_000_000_000,
		Name:              botName(seed),
		Level:             int32(40 + r.Intn(40)),
		MaxDeckPower:      power,
		FavoriteCostumeId: costumeId,
		PvpPoint:          clampNonNeg(targetPoint + (delta / 4)),
		IsBot:             true,
	}
}

func clampNonNeg(v int32) int32 {
	if v < 0 {
		return 0
	}
	return v
}

var botFirst = []string{"Aoi", "Levin", "Argo", "Fio", "Noelle", "Dimos", "Lars", "Yuzu", "Renah", "Gayle"}
var botLast = []string{"v", "x", "z", "q", "", "II", "EX", "+", "α", "Ω"}

func botName(seed int64) string {
	f := botFirst[seed%int64(len(botFirst))]
	l := botLast[(seed/7)%int64(len(botLast))]
	return f + l
}

// synthBotDeck builds a minimal valid PvpDeckCharacter list for a bot.
func synthBotDeck(pools botPools, playerId int64) []*pb.PvpDeckCharacter {
	r := rand.New(rand.NewSource(playerId))
	var out []*pb.PvpDeckCharacter
	for i := 0; i < 3; i++ {
		ch := &pb.PvpDeckCharacter{
			Costume:    &pb.CostumeInfo{CostumeId: pick(r, pools.costumeIds, 1), Level: int32(40 + r.Intn(40)), LimitBreakCount: int32(r.Intn(5))},
			MainWeapon: &pb.WeaponInfo{WeaponId: pick(r, pools.weaponIds, 1), Level: int32(40 + r.Intn(40)), LimitBreakCount: int32(r.Intn(5))},
		}
		if len(pools.companionIds) > 0 {
			ch.Companion = &pb.CompanionInfo{CompanionId: pick(r, pools.companionIds, 1), Level: int32(20 + r.Intn(20))}
		}
		out = append(out, ch)
	}
	return out
}

func sortedInt32Keys[V any](m map[int32]V) []int32 {
	out := make([]int32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
