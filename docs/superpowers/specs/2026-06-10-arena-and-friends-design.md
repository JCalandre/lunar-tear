# Arena & Friends — Design Spec

**Date:** 2026-06-10
**Project:** lunar-tear (NieR Reincarnation private server — Go 1.25 + gRPC + SQLite)
**Status:** Approved design, pending implementation plan

## Goal

Make the **Friends** and **Arena** (PvP) features work on the server. Today:

- **Friends** — `friend.proto` defines 12 RPCs and the service is registered, but `friend.go`
  implements only 4 read RPCs and they all return empty. The 8 mutating RPCs fall through to
  gRPC `Unimplemented`. There is no friend state in the store.
- **Arena** — in this game the Arena *is* PvP. `pvp.proto` fully defines `PvpService`, but there
  is **no `pvp.go` and the service is never registered**, so the entire Arena returns
  `Unimplemented`.

## Decisions (locked during brainstorming)

1. **Player model:** small multi-account LAN. Friends/PvP work between **real registered
   players**, with **simulated fallback** to fill gaps.
2. **Scope/sequencing:** one spec, sequenced — shared foundation → Friends → **core** Arena.
   Heavy seasonal/reward machinery is **deferred** to a follow-up spec.
3. **Fallback:** bots are **synthesized from master data** (sampling costume/weapon/companion
   tables), not cloned from real players and not hand-authored.
4. **Friends cheer:** the **full cheer → stamina loop**, with **bots included** (bots present as
   having cheered you, so the daily reward loop works even with few real friends).
5. **Architecture:** a **snapshot table + on-the-fly bot synthesis** (Approach 1). Both features
   read from a single `PlayerDirectory`; neither touches another user's live state directly.

### Key protocol fact

Arena combat is **client-authoritative**. `StartBattle` returns the opponent's full deck; the
client runs the auto-battle locally and reports the result via `FinishBattle(isVictory)`. The
server **never simulates combat** — Arena is matchmaking + bookkeeping (points, rank, logs).

---

## Architecture — the shared foundation

### `PlayerDirectory`

The spine. Answers "who else is out there, and what do they look like to others?" Everything
else depends only on this.

**Interface (consumer-facing):**

- `RealPlayersNear(viewerId, pvpPoint, limit) -> []PlayerCard` — real snapshots, excluding viewer,
  preferring entries near `pvpPoint`.
- `FillWithBots(existing, targetCount, seed) -> []PlayerCard` — tops the list up with synthesized
  bots to reach `targetCount`.
- `DefenseDeckOf(card) -> []PvpDeckCharacter` — real → projected from the stored snapshot deck;
  bot → synthesized directly.
- `IsBot(playerId) -> bool` — bot ids live in a reserved high range (e.g. `>= 1<<40`) so they
  never collide with real `playerId`s.

`PlayerCard` = `{playerId, name, level, maxDeckPower, favoriteCostumeId, pvpPoint, isBot}` — the
common currency for both features' list responses.

### `player_snapshots` table

One row per real player capturing their **public face**:

| column            | notes                                  |
|-------------------|----------------------------------------|
| `playerId`        | PK                                     |
| `userName`        |                                        |
| `level`           |                                        |
| `maxDeckPower`    |                                        |
| `favoriteCostumeId` |                                      |
| `pvpPoint`        | for matchmaking targeting + ranking    |
| `lastLoginDatetime` |                                      |
| `defenseDeckJson` | serialized defense deck (read whole)   |
| `updatedAt`       |                                        |

Standalone table (not nested in `UserState`). The defense deck is stored as a JSON blob — it is
read-mostly and only ever consumed whole by `DefenseDeckOf`.

### Snapshot refresh hook

A small helper updates a player's snapshot at two moments, no background job:

- **On login** — profile + `pvpPoint`.
- **After the player edits the defense deck** — the serialized defense deck.

> **To pin down in the plan:** which `DeckType` is the PvP/defense deck (confirm the enum value).

### Bot synthesis (`botGen`)

When a list needs more entries than real snapshots provide, generate bots **deterministically
from a seed** (`hash(viewerId, slot, dayBucket)`) so a list is stable until explicitly refreshed.
A bot = synthesized profile + a deck assembled by sampling costume/weapon/companion master-data
tables at a **target power band** near the viewer. Bots are never persisted — pure functions of
their seed. Bot ids occupy the reserved high range.

---

## Feature — Friends

### New per-user state (follows the established per-user-state pattern)

- `user.Friends map[int64]FriendEdge` — `{playerId, cheerSentToday, cheerReceivedPending,
  staminaReceivedToday, lastResetDay}`
- `user.IncomingFriendRequests map[int64]RequestMeta`
- `user.OutgoingFriendRequests map[int64]RequestMeta`

### RPCs

**Reads (replace the empty stubs):**

- `GetFriendList` → `Friends` hydrated via the directory into `FriendUser` rows, with per-friend
  `cheerReceived/cheerSent/staminaReceived` flags + aggregate `sendCheerCount`/`receivedCheerCount`.
- `GetFriendRequestList` → `IncomingFriendRequests` hydrated to `User` rows.
- `SearchRecommendedUsers` → directory: real non-friends + bot fill, as `User` rows.
- `GetUser` → single hydrated profile by `playerId`.

**Mutations (currently `Unimplemented`):**

- `SendFriendRequest(playerId)` — **real target:** add to *their* `IncomingFriendRequests` and
  *my* `OutgoingFriendRequests` (cross-user write); bump their
  `NotificationState.FriendRequestReceiveCount`. **Bot target:** auto-accepts immediately →
  straight to friends.
- `AcceptFriendRequest(playerId)` — add the edge on **both** sides, clear matching requests.
- `DeclineFriendRequest(playerIdOld, playerId[])` — drop incoming request(s).
- `DeleteFriend(playerId)` — remove the edge both sides (real) / just mine (bot).

**Cheer loop:**

- `CheerFriend(playerId)` / `BulkCheerFriend` — mark `cheerSentToday`; for a **real** friend, set
  *their* edge's `cheerReceivedPending = true`.
- `ReceiveCheer(playerId)` / `BulkReceiveCheer` — grant the **stamina reward**, mark
  `staminaReceivedToday`, clear pending. **Bots** always present as having cheered you.
- **Daily reset:** lazily on read/cheer — compare `lastResetDay` against the `gametime` day bucket
  and clear flags. No scheduler.

**Stamina reward:** a fixed config-driven grant (item id + amount) applied through the existing
inventory/consumable grant path, so it surfaces in `diffUserData` like every other reward.

### Edge cases

Duplicate request, request to an existing friend, accept of a no-longer-pending request → no-ops.
Self-request → rejected. All return success-shaped empty responses with `diffUserData`, matching
sibling services.

---

## Feature — Arena (core PvP)

### Wiring

Add `pvp.go` implementing `PvpService`; add `RegisterPvpServiceServer(...)` to `grpc.go`.

### New per-user state

- `user.PvpState` → `{pvpPoint, attackWinCount, attackLoseCount, defenseWinCount,
  defenseLoseCount, lastFinishDay}`
- `user.PvpAttackLog []BattleLog`, `user.PvpDefenseLog []BattleLog` (capped, ~30)
- `user.PvpMatching []MatchingEntry` — cached current opponent list, so `StartBattle` can recall a
  chosen opponent's deck.

### Point → rank/grade

Read `EntityMPvpGradeTable` to map `pvpPoint -> grade`. `GetTopData.rank` and leaderboard position
come from ranking real players + self by `pvpPoint`. **Bots are excluded from the global ranking**
(kept stable); they only populate matchmaking.

### RPCs

- `GetTopData` → `currentSeasonId` (single fixed/derived season), `pvpPoint`, `rank`. The
  `weeklyGradeResult / seasonRankResult / weeklyRankResult` sub-messages are **deferred** →
  zero/empty.
- `GetMatchingList` → return fresh cached list, else build one. `UpdateMatchingList` → always
  rebuild (reroll). Both: `RealPlayersNear(pvpPoint)` + `FillWithBots` → ~5 `MatchingOpponent`s,
  cached to `PvpMatching`.
- `StartBattle(opponentPlayerId, deckNumber)` → look opponent up in `PvpMatching`, return their
  defense deck via `DefenseDeckOf`. No combat server-side.
- `FinishBattle(opponentPlayerId, isVictory)` →
  - Point delta — rank-aware: win **+points** (more for beating a higher opponent), loss small
    **−points**, floored at 0.
  - Update `pvpPoint`, recompute rank, bump attack win/lose, prepend an **attack log** entry.
  - If opponent is a **real player**, append a **defense log** entry to *their* state
    (cross-user write). Bots → skipped.
  - Return `before/afterPvpPoint`, `before/afterRank`; `pvpGradeOneMatchRewardId /
    pvpGradeGroupId` reward fields **deferred** → 0.
- `GetRanking(rankFrom)` → leaderboard page from snapshot-backed real-player ranking + self, with
  `userCount` and `rankingPosition`.
- `GetSeasonResult` → derived from stored attack/defense counts.
- `GetAttackLogList` / `GetDefenseLogList` → the stored logs.

### Deferred to a follow-up spec

Seasons rolling over, weekly grade rewards, season-rank/weekly-rank reward tiers, per-match reward
grants. Core Arena is fully playable without them.

### Heaviest piece

Projecting a stored defense deck into the full `PvpDeckCharacter` (costume/weapon/parts/companion/
abilities/status-ups). The plan will check whether existing projection code
(`proj_inventory.go` / `state_projection.go`) can be reused; if not, this is the largest single
task. Bot decks are synthesized directly as `PvpDeckCharacter`, sidestepping projection for fill.

---

## Persistence, error handling & testing

### Persistence (one migration)

- **`player_snapshots`** table (above) — standalone.
- **New `UserState` fields** — `Friends`, `IncomingFriendRequests`, `OutgoingFriendRequests`,
  `PvpState`, `PvpAttackLog`, `PvpDefenseLog`, `PvpMatching` — each threaded through the full
  pattern: struct → `UserState` → `EnsureMaps`/init → `clone` → seed → sqlite `save.go`/`load.go`
  → migration. Bools stored as INTEGER via `boolToInt`.
- One goose migration, timestamp-prefixed, in `server/migrations/`.

### Cross-user writes

The one new access pattern. Go through the existing `UpdateUser(targetId, fn)` primitive on a
different id. Safeguards: (1) **no deadlock** — touch self and target sequentially in a fixed
order, never nested; (2) **existence/bot guard** — `IsBot` + existence check before any cross-user
write.

### Error handling

Match sibling services — degrade quietly, never crash a screen. Unknown/stale `playerId`, decline
of an already-gone request, cheer of a non-friend, `StartBattle` against an opponent no longer
cached → success-shaped empty/zero response with `diffUserData`, logged not errored. Bot-target
branches are explicit. Snapshot refresh failures are non-fatal.

### Testing

Small self-contained `_test.go` for pure logic (repo had zero tests):

- `botGen` determinism + id-range non-collision.
- Point/rank math: deltas, floor-at-zero, grade lookup against a fixed table.
- Cheer daily-reset bucketing across a day boundary (inject `gametime`).
- Friend state machine: request→accept adds both edges; decline/delete clean; duplicate/self
  no-ops.
- `DefenseDeckOf` for a synthesized bot → structurally valid `PvpDeckCharacter`.

Verification per project convention: `go build ./...` + `go vet ./internal/...` (build in
background — slow).

---

## Build sequencing

1. **Foundation** — `PlayerDirectory` + `player_snapshots` table + `botGen` + snapshot refresh hook.
2. **Friends** — end-to-end (state, all 12 RPCs, cheer loop).
3. **Arena core** — end-to-end (`pvp.go`, registration, state, all 9 RPCs).

Each stage is independently testable and leaves the server runnable.

## Out of scope

- Seasonal rollover, weekly/season rank reward tiers, per-match Arena rewards (follow-up spec).
- Server-side combat simulation (combat is client-authoritative by protocol design).
- Any client/APK changes — server-side only.
