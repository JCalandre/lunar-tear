# Arena & Friends Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Friends and Arena (PvP) features work end-to-end on the lunar-tear server: a shared player-directory foundation (snapshot table + master-data bot synthesis), full Friends (social graph + cheer→stamina loop), and core client-authoritative Arena PvP.

**Architecture:** Both features read from a single `PlayerDirectory` that combines real-player snapshots (a new standalone `player_snapshots` table refreshed on login and PvP-deck edit) with deterministically synthesized bots. New per-user bookkeeping state (friends, requests, pvp points/logs/matching) is threaded through the established UserState pattern. `diffUserData` is produced automatically by the existing gRPC `DiffInterceptor`, so handlers only mutate via `UpdateUser`. Arena combat is client-authoritative: the server hands the client the opponent deck and trusts the reported result.

**Tech Stack:** Go 1.25, gRPC, SQLite (goose migrations), protobuf. Module `lunar-tear/server`, run from `lt-upstream/server`.

**Branch:** `feat/arena-and-friends` (already created off `origin/main`).

**Spec:** `docs/superpowers/specs/2026-06-10-arena-and-friends-design.md`

---

## Key facts established during research (read before starting)

- **`UpdateUser(userId, mutate)`** (`internal/store/sqlite/user.go:243`) works for ANY userId, loads→clones→mutates→saves in its own transaction. There is **no per-user lock**. Cross-user writes are safe as **separate sequential** `UpdateUser` calls — **never nest** an `UpdateUser` inside another's mutator.
- **`diffUserData` is automatic.** `internal/interceptor/diff.go` loads the current user before the handler, runs it, reloads after, and sets the response's `diffUserData` via `userdata.ChangedTables` + `userdata.ComputeDelta`. **Do not** add the new internal friend/pvp state to `FullClientTableMap`/`changed_tables.go` — it is returned directly in dedicated response messages, not mirrored as client tables. Stamina/item grants (which ARE client tables) surface automatically.
- **Per-user-state pattern** (mirror exactly): struct in `internal/store/types.go` → field on `UserState` → init in `UserState.EnsureMaps()` (types.go) AND `initMaps()` (`sqlite/load.go`) → clone in `internal/store/clone.go` → save in `sqlite/save.go` (initial insert block + differential block) → load in `sqlite/load.go` (a `queryRows(...)` call) → goose migration in `migrations/`. Bools persisted via `boolToInt`. Map keys that are structs implement `MarshalText`/`UnmarshalText`.
- **Deck:** `DeckTypePvp = 2` (`internal/model/deck.go`) is the Arena defense deck. Deck slots read via `store.ReadDeckSlots(user, deckType, deckNumber) []DeckCharacterInput`. Per-character source maps on `UserState`: `Costumes`, `Weapons`, `Companions`, `Thoughts`, `Parts`, `DeckSubWeapons`, `DeckParts`, `WeaponAbilities`/`WeaponSkills`, `CharacterBoardAbilities`, `CharacterBoardStatusUps`, `CostumeAwakenStatusUps`, `CostumeLotteryEffects`. **No existing aggregator builds `PvpDeckCharacter` — write it from scratch.**
- **Stamina:** `user.Status.StaminaMilliValue int32` (millis) + `StaminaUpdateDatetime`. Grant via `store.RecoverStamina(user, millis, maxStaminaMillis, nowMillis)` (`internal/store/stamina.go`). See `internal/service/consumableitem.go` `UseEffectItem` for the max-stamina lookup to reuse.
- **Display name:** `user.Profile.Name`.
- **Login hook:** `UserServiceServer.GameStart` (`internal/service/user.go:87`), once per app launch.
- **PvP-deck edit hook:** `DeckServiceServer.ReplaceDeck` (`internal/service/deck.go:149`), when `DeckType == DeckTypePvp`.
- **Master data:** `holder.Get()` returns `*runtime.Catalogs` with `.Costume`, `.Weapon`, `.Companion`, `.GameConfig`, etc. PvP grade entity `EntityMPvpGrade{PvpGradeId, NecessaryPvpPoint, ...}` (`internal/masterdata/entities.go`). The grade catalog access path is verified in Task 12.
- **Service construction:** `cmd/lunar-tear/grpc.go:75 registerServices`. `userStore` implements both `store.UserRepository` and `store.SessionRepository`.
- **gametime:** `gametime.NowMillis()`, `gametime.StartOfDayMillis()` (use for the daily-reset day bucket).
- **Verification:** from `lt-upstream/server`: `go build ./...` (slow on first run — run in background) and `go vet ./internal/...`. Add small `_test.go` for pure logic. Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

---

## File Structure

**New files:**
- `internal/store/social.go` — new value/key structs: `FriendEdge`, `FriendKey` (int64 key, no struct key needed — use `map[int64]`), `FriendRequest`, `PvpState`, `BattleLogEntry`, `MatchingEntry`.
- `internal/store/snapshot.go` — `PlayerSnapshot` struct + `SnapshotRepository` interface.
- `internal/store/sqlite/snapshot.go` — SQLite `SnapshotRepository` impl.
- `internal/service/pvp_projection.go` — `BuildPvpDeckCharacters(user, deckType, deckNumber) []*pb.PvpDeckCharacter`, `PickDefenseDeck(user) (model.DeckType, int32)`.
- `internal/service/botgen.go` — deterministic bot synthesis (`BotIdBase`, `IsBotId`, `synthBot`, `synthBotDeck`).
- `internal/service/playerdirectory.go` — `PlayerCard`, `PlayerDirectory`, its constructor and methods.
- `internal/service/snapshot_refresh.go` — `RefreshSnapshot(snaps, user)` helper.
- `internal/service/pvp.go` — `PvpServiceServer` (all 9 RPCs).
- `internal/service/botgen_test.go`, `pvp_projection_test.go`, `pvp_points_test.go`, `friend_reset_test.go` — unit tests.
- `migrations/<timestamp>_add_social_pvp.sql` — schema.

**Modified files:**
- `internal/store/types.go` — add 5 fields to `UserState`; init in `EnsureMaps`.
- `internal/store/clone.go` — clone the new fields.
- `internal/store/sqlite/load.go` — `initMaps` + load queries.
- `internal/store/sqlite/save.go` — initial insert + differential save.
- `internal/store/sqlite/store.go` (or wherever `SQLiteStore` is) — ensure it satisfies `SnapshotRepository` (methods live in `sqlite/snapshot.go`).
- `internal/service/friend.go` — full rewrite (all 12 RPCs).
- `internal/service/user.go` — call `RefreshSnapshot` in `GameStart`.
- `internal/service/deck.go` — call `RefreshSnapshot` in `ReplaceDeck` when `DeckType==DeckTypePvp`.
- `cmd/lunar-tear/grpc.go` — build `PlayerDirectory`, pass to Friend+Pvp services, register `PvpService`.

---

# Stage 0 — Store foundation

### Task 1: New per-user state structs + UserState fields

**Files:**
- Create: `internal/store/social.go`
- Modify: `internal/store/types.go` (UserState struct + `EnsureMaps`)
- Modify: `internal/store/clone.go`

- [ ] **Step 1: Create the new state structs**

Create `internal/store/social.go`:

```go
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
	PlayerId         int64
	Name             string
	PvpPoint         int32
	Rank             int32
	DeckPower        int32
	IsBot            bool
	MostPowerfulCostumeId int32
}
```

- [ ] **Step 2: Add fields to UserState**

In `internal/store/types.go`, inside the `UserState` struct (near the other map fields, ~line 117), add:

```go
	Friends                 map[int64]FriendEdge
	IncomingFriendRequests  map[int64]FriendRequest
	OutgoingFriendRequests  map[int64]FriendRequest
	Pvp                     PvpState
	PvpAttackLog            []BattleLogEntry
	PvpDefenseLog           []BattleLogEntry
	PvpMatching             []MatchingEntry
```

- [ ] **Step 3: Initialize maps in EnsureMaps**

In `internal/store/types.go`, inside `func (u *UserState) EnsureMaps()` (~line 282, alongside the other `if u.X == nil` blocks), add:

```go
	if u.Friends == nil {
		u.Friends = make(map[int64]FriendEdge)
	}
	if u.IncomingFriendRequests == nil {
		u.IncomingFriendRequests = make(map[int64]FriendRequest)
	}
	if u.OutgoingFriendRequests == nil {
		u.OutgoingFriendRequests = make(map[int64]FriendRequest)
	}
```

(The slices `PvpAttackLog`/`PvpDefenseLog`/`PvpMatching` and the `Pvp` value need no init — nil slices and a zero struct are valid.)

- [ ] **Step 4: Clone the new fields**

In `internal/store/clone.go`, inside `CloneUserState` (alongside the other `maps.Clone` lines, ~line 83), add:

```go
	out.Friends = maps.Clone(u.Friends)
	out.IncomingFriendRequests = maps.Clone(u.IncomingFriendRequests)
	out.OutgoingFriendRequests = maps.Clone(u.OutgoingFriendRequests)
	out.PvpAttackLog = append([]BattleLogEntry(nil), u.PvpAttackLog...)
	out.PvpDefenseLog = append([]BattleLogEntry(nil), u.PvpDefenseLog...)
	out.PvpMatching = append([]MatchingEntry(nil), u.PvpMatching...)
```

(`out.Pvp` is a value type already copied by the struct copy at the top of `CloneUserState`.)

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/store/...`
Expected: builds clean (the new fields are not yet persisted — that is Tasks 2–3).

- [ ] **Step 6: Commit**

```bash
git add internal/store/social.go internal/store/types.go internal/store/clone.go
git commit -m "feat(store): add friends + pvp per-user state structs and fields

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Migration — snapshot table + per-user tables

**Files:**
- Create: `migrations/<timestamp>_add_social_pvp.sql`

- [ ] **Step 1: Create the migration file**

Name it with a timestamp AFTER the latest existing migration (check `ls migrations/` and pick a larger `YYYYMMDDHHMMSS`). Example name: `migrations/20260610120000_add_social_pvp.sql`.

```sql
-- +goose Up
CREATE TABLE user_friends (
    user_id                INTEGER NOT NULL REFERENCES users(user_id),
    friend_player_id       INTEGER NOT NULL,
    became_friends_at      INTEGER NOT NULL DEFAULT 0,
    cheer_sent_today       INTEGER NOT NULL DEFAULT 0,
    cheer_received_pending INTEGER NOT NULL DEFAULT 0,
    stamina_received_today INTEGER NOT NULL DEFAULT 0,
    last_reset_day         INTEGER NOT NULL DEFAULT 0,
    latest_version         INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, friend_player_id)
);

CREATE TABLE user_friend_requests_incoming (
    user_id          INTEGER NOT NULL REFERENCES users(user_id),
    from_player_id   INTEGER NOT NULL,
    requested_at     INTEGER NOT NULL DEFAULT 0,
    latest_version   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, from_player_id)
);

CREATE TABLE user_friend_requests_outgoing (
    user_id          INTEGER NOT NULL REFERENCES users(user_id),
    to_player_id     INTEGER NOT NULL,
    requested_at     INTEGER NOT NULL DEFAULT 0,
    latest_version   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, to_player_id)
);

CREATE TABLE user_pvp_state (
    user_id            INTEGER NOT NULL PRIMARY KEY REFERENCES users(user_id),
    pvp_point          INTEGER NOT NULL DEFAULT 0,
    attack_win_count   INTEGER NOT NULL DEFAULT 0,
    attack_lose_count  INTEGER NOT NULL DEFAULT 0,
    defense_win_count  INTEGER NOT NULL DEFAULT 0,
    defense_lose_count INTEGER NOT NULL DEFAULT 0,
    last_finish_day    INTEGER NOT NULL DEFAULT 0,
    latest_version     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE user_pvp_logs (
    user_id             INTEGER NOT NULL REFERENCES users(user_id),
    is_defense          INTEGER NOT NULL,           -- 0 = attack log, 1 = defense log
    seq                 INTEGER NOT NULL,
    opponent_player_id  INTEGER NOT NULL,
    opponent_name       TEXT    NOT NULL DEFAULT '',
    opponent_pvp_point  INTEGER NOT NULL DEFAULT 0,
    opponent_deck_power INTEGER NOT NULL DEFAULT 0,
    is_victory          INTEGER NOT NULL DEFAULT 0,
    battle_datetime     INTEGER NOT NULL DEFAULT 0,
    fluctuated_point    INTEGER NOT NULL DEFAULT 0,
    rank                INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, is_defense, seq)
);

CREATE TABLE player_snapshots (
    player_id           INTEGER NOT NULL PRIMARY KEY,
    user_name           TEXT    NOT NULL DEFAULT '',
    level               INTEGER NOT NULL DEFAULT 0,
    max_deck_power      INTEGER NOT NULL DEFAULT 0,
    favorite_costume_id INTEGER NOT NULL DEFAULT 0,
    pvp_point           INTEGER NOT NULL DEFAULT 0,
    last_login_datetime INTEGER NOT NULL DEFAULT 0,
    defense_deck_json   TEXT    NOT NULL DEFAULT '[]',
    updated_at          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_player_snapshots_pvp_point ON player_snapshots(pvp_point DESC);

-- +goose Down
DROP TABLE IF EXISTS player_snapshots;
DROP TABLE IF EXISTS user_pvp_logs;
DROP TABLE IF EXISTS user_pvp_state;
DROP TABLE IF EXISTS user_friend_requests_outgoing;
DROP TABLE IF EXISTS user_friend_requests_incoming;
DROP TABLE IF EXISTS user_friends;
```

- [ ] **Step 2: Apply the migration**

Run: `make migrate`
Expected: goose reports the new migration applied with no error.

- [ ] **Step 3: Verify the tables exist**

Run: `go run ./cmd/... ` is not needed; instead inspect: open `db/game.db` and confirm tables, e.g. with the project's preferred method, or simply re-run `make migrate` and confirm it reports "no migrations to run."
Expected: the six tables exist.

- [ ] **Step 4: Commit**

```bash
git add migrations/20260610120000_add_social_pvp.sql
git commit -m "feat(db): migration for friends, pvp state/logs, and player_snapshots

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: SQLite save/load for the new per-user fields

**Files:**
- Modify: `internal/store/sqlite/load.go` (`initMaps` + load queries)
- Modify: `internal/store/sqlite/save.go` (initial insert block + differential block)

- [ ] **Step 1: Initialize the maps in initMaps**

In `internal/store/sqlite/load.go`, inside `func initMaps(u *store.UserState)` (~line 63), add:

```go
	u.Friends = make(map[int64]store.FriendEdge)
	u.IncomingFriendRequests = make(map[int64]store.FriendRequest)
	u.OutgoingFriendRequests = make(map[int64]store.FriendRequest)
```

- [ ] **Step 2: Add load queries**

In `internal/store/sqlite/load.go`, in the map-loading section (near the other `queryRows(...)` calls, ~line 459), add:

```go
	queryRows(db, `SELECT friend_player_id, became_friends_at, cheer_sent_today, cheer_received_pending,
		stamina_received_today, last_reset_day, latest_version FROM user_friends WHERE user_id=?`, uid,
		func(rows *sql.Rows) {
			var v store.FriendEdge
			var cs, cr, sr int
			rows.Scan(&v.PlayerId, &v.BecameFriendsAt, &cs, &cr, &sr, &v.LastResetDay, &v.LatestVersion)
			v.CheerSentToday = cs != 0
			v.CheerReceivedPending = cr != 0
			v.StaminaReceivedToday = sr != 0
			u.Friends[v.PlayerId] = v
		})

	queryRows(db, `SELECT from_player_id, requested_at, latest_version FROM user_friend_requests_incoming WHERE user_id=?`, uid,
		func(rows *sql.Rows) {
			var v store.FriendRequest
			rows.Scan(&v.PlayerId, &v.RequestedAt, &v.LatestVersion)
			u.IncomingFriendRequests[v.PlayerId] = v
		})

	queryRows(db, `SELECT to_player_id, requested_at, latest_version FROM user_friend_requests_outgoing WHERE user_id=?`, uid,
		func(rows *sql.Rows) {
			var v store.FriendRequest
			rows.Scan(&v.PlayerId, &v.RequestedAt, &v.LatestVersion)
			u.OutgoingFriendRequests[v.PlayerId] = v
		})

	queryRows(db, `SELECT pvp_point, attack_win_count, attack_lose_count, defense_win_count,
		defense_lose_count, last_finish_day, latest_version FROM user_pvp_state WHERE user_id=?`, uid,
		func(rows *sql.Rows) {
			rows.Scan(&u.Pvp.PvpPoint, &u.Pvp.AttackWinCount, &u.Pvp.AttackLoseCount,
				&u.Pvp.DefenseWinCount, &u.Pvp.DefenseLoseCount, &u.Pvp.LastFinishDay, &u.Pvp.LatestVersion)
		})

	queryRows(db, `SELECT is_defense, seq, opponent_player_id, opponent_name, opponent_pvp_point,
		opponent_deck_power, is_victory, battle_datetime, fluctuated_point, rank
		FROM user_pvp_logs WHERE user_id=? ORDER BY seq ASC`, uid,
		func(rows *sql.Rows) {
			var e store.BattleLogEntry
			var isDef, win int
			rows.Scan(&isDef, &e.Seq, &e.OpponentPlayerId, &e.OpponentName, &e.OpponentPvpPoint,
				&e.OpponentDeckPower, &win, &e.BattleDatetime, &e.FluctuatedPoint, &e.Rank)
			e.IsVictory = win != 0
			if isDef != 0 {
				u.PvpDefenseLog = append(u.PvpDefenseLog, e)
			} else {
				u.PvpAttackLog = append(u.PvpAttackLog, e)
			}
		})
```

(Note: `PvpMatching` is a transient cache — persisting it is optional. Persist it too for restart-stability by adding a small table, OR keep it in-memory only. **Decision: keep `PvpMatching` in-memory only** — it is rebuilt on the next `GetMatchingList`. Do NOT load/save it. This avoids a seventh table.)

- [ ] **Step 3: Add the initial-insert block in save.go**

In `internal/store/sqlite/save.go`, in the new-user insert section (mirroring the `for k, v := range u.CostumeAwakenStatusUps` block, ~line 292), add a function used by both initial and differential paths is cleaner, but to match the file's existing style add direct blocks. First the initial insert (full write):

```go
	for _, v := range u.Friends {
		if err := exec(`INSERT INTO user_friends (user_id, friend_player_id, became_friends_at, cheer_sent_today,
			cheer_received_pending, stamina_received_today, last_reset_day, latest_version) VALUES (?,?,?,?,?,?,?,?)`,
			uid, v.PlayerId, v.BecameFriendsAt, boolToInt(v.CheerSentToday), boolToInt(v.CheerReceivedPending),
			boolToInt(v.StaminaReceivedToday), v.LastResetDay, v.LatestVersion); err != nil {
			return err
		}
	}
	for _, v := range u.IncomingFriendRequests {
		if err := exec(`INSERT INTO user_friend_requests_incoming (user_id, from_player_id, requested_at, latest_version)
			VALUES (?,?,?,?)`, uid, v.PlayerId, v.RequestedAt, v.LatestVersion); err != nil {
			return err
		}
	}
	for _, v := range u.OutgoingFriendRequests {
		if err := exec(`INSERT INTO user_friend_requests_outgoing (user_id, to_player_id, requested_at, latest_version)
			VALUES (?,?,?,?)`, uid, v.PlayerId, v.RequestedAt, v.LatestVersion); err != nil {
			return err
		}
	}
	if err := exec(`INSERT INTO user_pvp_state (user_id, pvp_point, attack_win_count, attack_lose_count,
		defense_win_count, defense_lose_count, last_finish_day, latest_version) VALUES (?,?,?,?,?,?,?,?)`,
		uid, u.Pvp.PvpPoint, u.Pvp.AttackWinCount, u.Pvp.AttackLoseCount, u.Pvp.DefenseWinCount,
		u.Pvp.DefenseLoseCount, u.Pvp.LastFinishDay, u.Pvp.LatestVersion); err != nil {
		return err
	}
	for _, e := range u.PvpAttackLog {
		if err := exec(`INSERT INTO user_pvp_logs (user_id, is_defense, seq, opponent_player_id, opponent_name,
			opponent_pvp_point, opponent_deck_power, is_victory, battle_datetime, fluctuated_point, rank)
			VALUES (?,0,?,?,?,?,?,?,?,?,?)`, uid, e.Seq, e.OpponentPlayerId, e.OpponentName, e.OpponentPvpPoint,
			e.OpponentDeckPower, boolToInt(e.IsVictory), e.BattleDatetime, e.FluctuatedPoint, e.Rank); err != nil {
			return err
		}
	}
	for _, e := range u.PvpDefenseLog {
		if err := exec(`INSERT INTO user_pvp_logs (user_id, is_defense, seq, opponent_player_id, opponent_name,
			opponent_pvp_point, opponent_deck_power, is_victory, battle_datetime, fluctuated_point, rank)
			VALUES (?,1,?,?,?,?,?,?,?,?,?)`, uid, e.Seq, e.OpponentPlayerId, e.OpponentName, e.OpponentPvpPoint,
			e.OpponentDeckPower, boolToInt(e.IsVictory), e.BattleDatetime, e.FluctuatedPoint, e.Rank); err != nil {
			return err
		}
	}
```

- [ ] **Step 4: Add the differential-save block in save.go**

In the differential section (mirroring the `for k, v := range after.CostumeAwakenStatusUps` + delete loop, ~line 858), add:

```go
	for k, v := range after.Friends {
		if old, ok := before.Friends[k]; !ok || old != v {
			exec(`INSERT OR REPLACE INTO user_friends (user_id, friend_player_id, became_friends_at, cheer_sent_today,
				cheer_received_pending, stamina_received_today, last_reset_day, latest_version) VALUES (?,?,?,?,?,?,?,?)`,
				uid, v.PlayerId, v.BecameFriendsAt, boolToInt(v.CheerSentToday), boolToInt(v.CheerReceivedPending),
				boolToInt(v.StaminaReceivedToday), v.LastResetDay, v.LatestVersion)
		}
	}
	for k := range before.Friends {
		if _, ok := after.Friends[k]; !ok {
			exec(`DELETE FROM user_friends WHERE user_id=? AND friend_player_id=?`, uid, k)
		}
	}
	for k, v := range after.IncomingFriendRequests {
		if old, ok := before.IncomingFriendRequests[k]; !ok || old != v {
			exec(`INSERT OR REPLACE INTO user_friend_requests_incoming (user_id, from_player_id, requested_at, latest_version)
				VALUES (?,?,?,?)`, uid, v.PlayerId, v.RequestedAt, v.LatestVersion)
		}
	}
	for k := range before.IncomingFriendRequests {
		if _, ok := after.IncomingFriendRequests[k]; !ok {
			exec(`DELETE FROM user_friend_requests_incoming WHERE user_id=? AND from_player_id=?`, uid, k)
		}
	}
	for k, v := range after.OutgoingFriendRequests {
		if old, ok := before.OutgoingFriendRequests[k]; !ok || old != v {
			exec(`INSERT OR REPLACE INTO user_friend_requests_outgoing (user_id, to_player_id, requested_at, latest_version)
				VALUES (?,?,?,?)`, uid, v.PlayerId, v.RequestedAt, v.LatestVersion)
		}
	}
	for k := range before.OutgoingFriendRequests {
		if _, ok := after.OutgoingFriendRequests[k]; !ok {
			exec(`DELETE FROM user_friend_requests_outgoing WHERE user_id=? AND to_player_id=?`, uid, k)
		}
	}
	if before.Pvp != after.Pvp {
		exec(`INSERT OR REPLACE INTO user_pvp_state (user_id, pvp_point, attack_win_count, attack_lose_count,
			defense_win_count, defense_lose_count, last_finish_day, latest_version) VALUES (?,?,?,?,?,?,?,?)`,
			uid, after.Pvp.PvpPoint, after.Pvp.AttackWinCount, after.Pvp.AttackLoseCount, after.Pvp.DefenseWinCount,
			after.Pvp.DefenseLoseCount, after.Pvp.LastFinishDay, after.Pvp.LatestVersion)
	}
	// Logs are append-mostly and capped; simplest correct approach: rewrite both logs for this user.
	if !pvpLogsEqual(before.PvpAttackLog, after.PvpAttackLog) || !pvpLogsEqual(before.PvpDefenseLog, after.PvpDefenseLog) {
		exec(`DELETE FROM user_pvp_logs WHERE user_id=?`, uid)
		for _, e := range after.PvpAttackLog {
			exec(`INSERT INTO user_pvp_logs (user_id, is_defense, seq, opponent_player_id, opponent_name,
				opponent_pvp_point, opponent_deck_power, is_victory, battle_datetime, fluctuated_point, rank)
				VALUES (?,0,?,?,?,?,?,?,?,?,?)`, uid, e.Seq, e.OpponentPlayerId, e.OpponentName, e.OpponentPvpPoint,
				e.OpponentDeckPower, boolToInt(e.IsVictory), e.BattleDatetime, e.FluctuatedPoint, e.Rank)
		}
		for _, e := range after.PvpDefenseLog {
			exec(`INSERT INTO user_pvp_logs (user_id, is_defense, seq, opponent_player_id, opponent_name,
				opponent_pvp_point, opponent_deck_power, is_victory, battle_datetime, fluctuated_point, rank)
				VALUES (?,1,?,?,?,?,?,?,?,?,?)`, uid, e.Seq, e.OpponentPlayerId, e.OpponentName, e.OpponentPvpPoint,
				e.OpponentDeckPower, boolToInt(e.IsVictory), e.BattleDatetime, e.FluctuatedPoint, e.Rank)
		}
	}
```

Add this helper at the bottom of `save.go`:

```go
func pvpLogsEqual(a, b []store.BattleLogEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Build and verify**

Run: `go build ./internal/store/...`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add internal/store/sqlite/load.go internal/store/sqlite/save.go
git commit -m "feat(store): persist friends + pvp state/logs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: PlayerSnapshot type + SnapshotRepository

**Files:**
- Create: `internal/store/snapshot.go`
- Create: `internal/store/sqlite/snapshot.go`

- [ ] **Step 1: Define the snapshot type and repository interface**

Create `internal/store/snapshot.go`:

```go
package store

// PlayerSnapshot is a real player's public face, written on login and PvP-deck edit.
type PlayerSnapshot struct {
	PlayerId          int64
	UserName          string
	Level             int32
	MaxDeckPower      int32
	FavoriteCostumeId int32
	PvpPoint          int32
	LastLoginDatetime int64
	DefenseDeckJson   string // serialized []*pb.PvpDeckCharacter
	UpdatedAt         int64
}

// SnapshotRepository stores and queries player snapshots (the cross-user directory source).
type SnapshotRepository interface {
	UpsertSnapshot(s PlayerSnapshot) error
	GetSnapshot(playerId int64) (PlayerSnapshot, error)
	// ListSnapshotsExcluding returns up to limit snapshots other than excludePlayerId,
	// ordered by closeness of pvp_point to nearPoint (closest first).
	ListSnapshotsNear(excludePlayerId int64, nearPoint int32, limit int) ([]PlayerSnapshot, error)
	// ListSnapshotsByPointDesc returns a ranking page ordered by pvp_point desc.
	ListSnapshotsByPointDesc(offset, limit int) ([]PlayerSnapshot, error)
	// CountSnapshots / RankOfPlayer support ranking position math.
	CountSnapshots() (int, error)
	RankOfPlayer(playerId int64) (int, error) // 1-based rank by pvp_point desc; 0 if absent
}
```

- [ ] **Step 2: Implement the repository on SQLiteStore**

Create `internal/store/sqlite/snapshot.go` (the receiver type must match the existing store struct — confirm it is `*SQLiteStore` in `internal/store/sqlite/store.go`):

```go
package sqlite

import (
	"database/sql"

	"lunar-tear/server/internal/store"
)

func (s *SQLiteStore) UpsertSnapshot(snap store.PlayerSnapshot) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO player_snapshots
		(player_id, user_name, level, max_deck_power, favorite_costume_id, pvp_point,
		 last_login_datetime, defense_deck_json, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		snap.PlayerId, snap.UserName, snap.Level, snap.MaxDeckPower, snap.FavoriteCostumeId,
		snap.PvpPoint, snap.LastLoginDatetime, snap.DefenseDeckJson, snap.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetSnapshot(playerId int64) (store.PlayerSnapshot, error) {
	var snap store.PlayerSnapshot
	err := s.db.QueryRow(`SELECT player_id, user_name, level, max_deck_power, favorite_costume_id,
		pvp_point, last_login_datetime, defense_deck_json, updated_at
		FROM player_snapshots WHERE player_id=?`, playerId).Scan(
		&snap.PlayerId, &snap.UserName, &snap.Level, &snap.MaxDeckPower, &snap.FavoriteCostumeId,
		&snap.PvpPoint, &snap.LastLoginDatetime, &snap.DefenseDeckJson, &snap.UpdatedAt)
	if err == sql.ErrNoRows {
		return snap, store.ErrNotFound
	}
	return snap, err
}

func scanSnapshots(rows *sql.Rows) ([]store.PlayerSnapshot, error) {
	defer rows.Close()
	var out []store.PlayerSnapshot
	for rows.Next() {
		var s store.PlayerSnapshot
		if err := rows.Scan(&s.PlayerId, &s.UserName, &s.Level, &s.MaxDeckPower, &s.FavoriteCostumeId,
			&s.PvpPoint, &s.LastLoginDatetime, &s.DefenseDeckJson, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListSnapshotsNear(excludePlayerId int64, nearPoint int32, limit int) ([]store.PlayerSnapshot, error) {
	rows, err := s.db.Query(`SELECT player_id, user_name, level, max_deck_power, favorite_costume_id,
		pvp_point, last_login_datetime, defense_deck_json, updated_at
		FROM player_snapshots WHERE player_id<>?
		ORDER BY ABS(pvp_point - ?) ASC LIMIT ?`, excludePlayerId, nearPoint, limit)
	if err != nil {
		return nil, err
	}
	return scanSnapshots(rows)
}

func (s *SQLiteStore) ListSnapshotsByPointDesc(offset, limit int) ([]store.PlayerSnapshot, error) {
	rows, err := s.db.Query(`SELECT player_id, user_name, level, max_deck_power, favorite_costume_id,
		pvp_point, last_login_datetime, defense_deck_json, updated_at
		FROM player_snapshots ORDER BY pvp_point DESC, player_id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanSnapshots(rows)
}

func (s *SQLiteStore) CountSnapshots() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM player_snapshots`).Scan(&n)
	return n, err
}

func (s *SQLiteStore) RankOfPlayer(playerId int64) (int, error) {
	var pvpPoint int32
	err := s.db.QueryRow(`SELECT pvp_point FROM player_snapshots WHERE player_id=?`, playerId).Scan(&pvpPoint)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var ahead int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM player_snapshots WHERE pvp_point > ?`, pvpPoint).Scan(&ahead)
	if err != nil {
		return 0, err
	}
	return ahead + 1, nil
}
```

- [ ] **Step 3: Build and verify SQLiteStore satisfies the interface**

Add a compile-time assertion at the bottom of `internal/store/sqlite/snapshot.go`:

```go
var _ store.SnapshotRepository = (*SQLiteStore)(nil)
```

Run: `go build ./internal/store/...`
Expected: builds clean. If `*SQLiteStore` is not the receiver type, fix the receiver to match `store.go`.

- [ ] **Step 4: Commit**

```bash
git add internal/store/snapshot.go internal/store/sqlite/snapshot.go
git commit -m "feat(store): player_snapshots repository

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

# Stage 1 — Directory, projection, bots

### Task 5: PvP deck projection

**Files:**
- Create: `internal/service/pvp_projection.go`
- Create: `internal/service/pvp_projection_test.go`

- [ ] **Step 1: Write the projection (read source maps confirmed in research)**

Create `internal/service/pvp_projection.go`. Before writing, open `internal/service/proj_deck.go` and `internal/store/helpers.go` `ReadDeckSlots` to confirm exact field/map names on `UserState` (`Costumes`, `Weapons`, `Companions`, `Thoughts`, `Parts`, `DeckSubWeapons`, `DeckParts`, `WeaponAbilities`, `WeaponSkills`, `CharacterBoardAbilities`, `CharacterBoardStatusUps`, `CostumeAwakenStatusUps`, `CostumeLotteryEffects`). Then:

```go
package service

import (
	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
)

// PickDefenseDeck returns the deck a player exposes to opponents: their PvP deck if set,
// else their quest deck #1 as a fallback so brand-new players still field a valid team.
func PickDefenseDeck(user *store.UserState) (model.DeckType, int32) {
	pvpKey := store.DeckKey{DeckType: model.DeckTypePvp, UserDeckNumber: 1}
	if d, ok := user.Decks[pvpKey]; ok && d.UserDeckCharacterUuid01 != "" {
		return model.DeckTypePvp, 1
	}
	return model.DeckTypeQuest, 1
}

// BuildPvpDeckCharacters projects a stored deck into the rich battle representation the
// client needs for StartBattle. Empty slots are skipped. Optional sub-collections are
// emitted only when present, so a minimal deck is still structurally valid.
func BuildPvpDeckCharacters(user *store.UserState, deckType model.DeckType, deckNumber int32) []*pb.PvpDeckCharacter {
	key := store.DeckKey{DeckType: deckType, UserDeckNumber: deckNumber}
	deck, ok := user.Decks[key]
	if !ok {
		return nil
	}
	uuids := []string{deck.UserDeckCharacterUuid01, deck.UserDeckCharacterUuid02, deck.UserDeckCharacterUuid03}
	var out []*pb.PvpDeckCharacter
	for _, dcUuid := range uuids {
		if dcUuid == "" {
			continue
		}
		dc, ok := user.DeckCharacters[dcUuid]
		if !ok {
			continue
		}
		out = append(out, buildOnePvpCharacter(user, dcUuid, dc))
	}
	return out
}

func buildOnePvpCharacter(user *store.UserState, dcUuid string, dc store.DeckCharacterState) *pb.PvpDeckCharacter {
	ch := &pb.PvpDeckCharacter{}

	if c, ok := user.Costumes[dc.UserCostumeUuid]; ok {
		ch.Costume = &pb.CostumeInfo{
			CostumeId:                             c.CostumeId,
			LimitBreakCount:                       c.LimitBreakCount,
			Level:                                 c.Level,
			CharacterLevel:                        c.Level,
			CostumeLotteryEffectUnlockedSlotCount: c.CostumeLotteryEffectUnlockedSlotCount,
		}
	}
	if comp, ok := user.Companions[dc.UserCompanionUuid]; ok {
		ch.Companion = &pb.CompanionInfo{CompanionId: comp.CompanionId, Level: comp.Level}
	}
	if w, ok := user.Weapons[dc.MainUserWeaponUuid]; ok {
		ch.MainWeapon = buildWeaponInfo(user, w)
	}
	if t, ok := user.Thoughts[dc.UserThoughtUuid]; ok {
		ch.Thought = &pb.ThoughtInfo{ThoughtId: t.ThoughtId}
	}
	// Sub-weapons and parts (read from the deck-scoped maps).
	for _, wu := range user.DeckSubWeapons[dcUuid] {
		if w, ok := user.Weapons[wu]; ok {
			ch.SubWeapon = append(ch.SubWeapon, buildWeaponInfo(user, w))
		}
	}
	for _, pu := range user.DeckParts[dcUuid] {
		if p, ok := user.Parts[pu]; ok {
			ch.Parts = append(ch.Parts, &pb.PartsInfo{
				PartsId:           p.PartsId,
				Level:             p.Level,
				PartsMainStatusId: p.PartsStatusMainId,
			})
		}
	}
	return ch
}

func buildWeaponInfo(user *store.UserState, w store.WeaponState) *pb.WeaponInfo {
	wi := &pb.WeaponInfo{
		WeaponId:        w.WeaponId,
		LimitBreakCount: w.LimitBreakCount,
		Level:           w.Level,
	}
	for _, a := range user.WeaponAbilities[w.UserWeaponUuid] {
		wi.WeaponAbility = append(wi.WeaponAbility, &pb.WeaponAbilityInfo{AbilityId: a.SlotNumber, Level: a.Level})
	}
	for _, sk := range user.WeaponSkills[w.UserWeaponUuid] {
		wi.WeaponSkill = append(wi.WeaponSkill, &pb.WeaponSkillInfo{SkillId: sk.SlotNumber, Level: sk.Level})
	}
	return wi
}
```

> Implementer note: the exact shape of `user.WeaponAbilities` / `user.WeaponSkills` / `user.DeckSubWeapons` / `user.DeckParts` (map key types) must be confirmed against `types.go`; adjust the indexing accordingly. The goal is a structurally valid `PvpDeckCharacter` with costume, main weapon, companion, sub-weapons, and parts populated. Character-board / awaken / lottery sub-collections are OPTIONAL for a working battle and may be added later; leave them empty for the core.

- [ ] **Step 2: Write a structural test**

Create `internal/service/pvp_projection_test.go`:

```go
package service

import (
	"testing"

	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
)

func TestBuildPvpDeckCharacters_minimalDeck(t *testing.T) {
	u := &store.UserState{}
	u.EnsureMaps()
	u.Costumes["c1"] = store.CostumeState{UserCostumeUuid: "c1", CostumeId: 1001, Level: 50}
	u.Weapons["w1"] = store.WeaponState{UserWeaponUuid: "w1", WeaponId: 2001, Level: 40}
	u.DeckCharacters["dc1"] = store.DeckCharacterState{
		UserDeckCharacterUuid: "dc1", UserCostumeUuid: "c1", MainUserWeaponUuid: "w1",
	}
	u.Decks[store.DeckKey{DeckType: model.DeckTypePvp, UserDeckNumber: 1}] = store.DeckState{
		DeckType: model.DeckTypePvp, UserDeckNumber: 1, UserDeckCharacterUuid01: "dc1",
	}

	got := BuildPvpDeckCharacters(u, model.DeckTypePvp, 1)
	if len(got) != 1 {
		t.Fatalf("want 1 character, got %d", len(got))
	}
	if got[0].Costume == nil || got[0].Costume.CostumeId != 1001 {
		t.Fatalf("costume not projected: %+v", got[0].Costume)
	}
	if got[0].MainWeapon == nil || got[0].MainWeapon.WeaponId != 2001 {
		t.Fatalf("main weapon not projected: %+v", got[0].MainWeapon)
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/service/ -run TestBuildPvpDeckCharacters -v`
Expected: PASS. (If `EnsureMaps` does not initialize `Costumes`/`Weapons`/`DeckCharacters`/`Decks`, initialize them inline in the test instead.)

- [ ] **Step 4: Commit**

```bash
git add internal/service/pvp_projection.go internal/service/pvp_projection_test.go
git commit -m "feat(pvp): project stored deck into PvpDeckCharacter

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Deterministic bot synthesis

**Files:**
- Create: `internal/service/botgen.go`
- Create: `internal/service/botgen_test.go`

- [ ] **Step 1: Write botgen**

Create `internal/service/botgen.go`. It needs read access to the catalogs for costume/weapon/companion ids; pass them in as sorted id slices so generation is deterministic.

```go
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
	// Power band: within +/-15% of targetPoint, floored sensibly.
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
```

> Implementer note: `synthBot` references `PlayerCard`, defined in Task 7. Implement Task 7's `PlayerCard` struct first if the compiler complains, or define both files in the same commit. The `pools` are built from `holder.Get().Costume/.Weapon/.Companion` — confirm those catalog structs expose an id-keyed map (e.g. `cat.Costume.Costumes map[int32]EntityMCostume`) in Task 7 and use `sortedInt32Keys` on it.

- [ ] **Step 2: Write determinism tests**

Create `internal/service/botgen_test.go`:

```go
package service

import "testing"

func TestSynthBot_deterministic(t *testing.T) {
	pools := botPools{costumeIds: []int32{10, 20, 30}, weaponIds: []int32{1, 2}, companionIds: []int32{5}}
	a := synthBot(pools, 42, 0, 1000, 5000)
	b := synthBot(pools, 42, 0, 1000, 5000)
	if a != b {
		t.Fatalf("same seed must yield identical bot:\n%+v\n%+v", a, b)
	}
	c := synthBot(pools, 42, 1, 1000, 5000)
	if a.PlayerId == c.PlayerId {
		t.Fatalf("different slot must yield a different bot id")
	}
}

func TestSynthBot_idRange(t *testing.T) {
	pools := botPools{costumeIds: []int32{10}, weaponIds: []int32{1}}
	for slot := 0; slot < 50; slot++ {
		bot := synthBot(pools, 7, slot, 99, 3000)
		if !IsBotId(bot.PlayerId) {
			t.Fatalf("bot id %d not in reserved range", bot.PlayerId)
		}
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/service/ -run TestSynthBot -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/botgen.go internal/service/botgen_test.go
git commit -m "feat(pvp): deterministic master-data bot synthesis

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: PlayerDirectory

**Files:**
- Create: `internal/service/playerdirectory.go`

- [ ] **Step 1: Write the directory**

Create `internal/service/playerdirectory.go`. Confirm the catalog field names against `internal/runtime/holder.go` (`Catalogs.Costume`, `.Weapon`, `.Companion`) and the id-map field inside each catalog (e.g. `CostumeCatalog.Costumes`); adjust below to match.

```go
package service

import (
	"encoding/json"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/runtime"
	"lunar-tear/server/internal/store"
)

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

// PlayerDirectory answers "who else is out there?" from real snapshots + synthesized bots.
type PlayerDirectory struct {
	snaps  store.SnapshotRepository
	holder *runtime.Holder
}

func NewPlayerDirectory(snaps store.SnapshotRepository, holder *runtime.Holder) *PlayerDirectory {
	return &PlayerDirectory{snaps: snaps, holder: holder}
}

func (d *PlayerDirectory) pools() botPools {
	cat := d.holder.Get()
	p := botPools{}
	if cat != nil {
		if cat.Costume != nil {
			p.costumeIds = sortedInt32Keys(cat.Costume.Costumes) // confirm field name
		}
		if cat.Weapon != nil {
			p.weaponIds = sortedInt32Keys(cat.Weapon.Weapons)
		}
		if cat.Companion != nil {
			p.companionIds = sortedInt32Keys(cat.Companion.Companions)
		}
	}
	return p
}

func cardFromSnapshot(s store.PlayerSnapshot) PlayerCard {
	return PlayerCard{
		PlayerId:          s.PlayerId,
		Name:              s.UserName,
		Level:             s.Level,
		MaxDeckPower:      s.MaxDeckPower,
		FavoriteCostumeId: s.FavoriteCostumeId,
		PvpPoint:          s.PvpPoint,
		IsBot:             false,
	}
}

// RealPlayersNear returns real snapshots (excluding the viewer) closest to nearPoint.
func (d *PlayerDirectory) RealPlayersNear(viewerId int64, nearPoint int32, limit int) []PlayerCard {
	snaps, err := d.snaps.ListSnapshotsNear(viewerId, nearPoint, limit)
	if err != nil {
		return nil
	}
	out := make([]PlayerCard, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, cardFromSnapshot(s))
	}
	return out
}

// FillWithBots tops the list up to targetCount with deterministic bots seeded by viewer+day.
func (d *PlayerDirectory) FillWithBots(existing []PlayerCard, targetCount int, viewerId int64, dayBucket int64, nearPoint int32) []PlayerCard {
	if len(existing) >= targetCount {
		return existing
	}
	pools := d.pools()
	out := existing
	for slot := 0; len(out) < targetCount; slot++ {
		out = append(out, synthBot(pools, viewerId, slot, dayBucket, nearPoint))
	}
	return out
}

// DefenseDeckOf returns the opponent deck for a card: deserialized snapshot deck for a real
// player, or a synthesized deck for a bot.
func (d *PlayerDirectory) DefenseDeckOf(card PlayerCard) []*pb.PvpDeckCharacter {
	if card.IsBot {
		return synthBotDeck(d.pools(), card.PlayerId)
	}
	snap, err := d.snaps.GetSnapshot(card.PlayerId)
	if err != nil {
		return nil
	}
	var deck []*pb.PvpDeckCharacter
	if snap.DefenseDeckJson != "" {
		_ = json.Unmarshal([]byte(snap.DefenseDeckJson), &deck)
	}
	return deck
}

func (d *PlayerDirectory) IsBot(playerId int64) bool { return IsBotId(playerId) }
```

> Implementer note: `json.Unmarshal` into `[]*pb.PvpDeckCharacter` works because proto messages are plain structs with json tags via protobuf-go; if round-tripping proves lossy, switch the snapshot serialization to `protojson` over a wrapper `pb.GetMatchingListResponse`-style message. For the core, encoding via `encoding/json` of the slice (as written by `RefreshSnapshot` in Task 8) is sufficient and symmetric.

- [ ] **Step 2: Build**

Run: `go build ./internal/service/...`
Expected: builds clean once catalog field names are corrected. Fix any `cat.Costume.Costumes` field-name mismatch by reading `internal/masterdata/costume.go` etc.

- [ ] **Step 3: Commit**

```bash
git add internal/service/playerdirectory.go
git commit -m "feat(pvp): PlayerDirectory over snapshots + bots

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Snapshot refresh + hooks

**Files:**
- Create: `internal/service/snapshot_refresh.go`
- Modify: `internal/service/user.go` (`GameStart`)
- Modify: `internal/service/deck.go` (`ReplaceDeck`)

- [ ] **Step 1: Write the refresh helper**

Create `internal/service/snapshot_refresh.go`. Confirm against `types.go` how level / max deck power / favorite costume are stored (`user.Status` level? `user.DeckTypeNotes` for max power? a profile favorite-costume field). Adjust the getters below to the real fields.

```go
package service

import (
	"encoding/json"

	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/store"
)

// RefreshSnapshot writes the user's public face to the snapshot table.
// Best-effort: errors are returned for logging but must not fail the caller's RPC.
func RefreshSnapshot(snaps store.SnapshotRepository, user *store.UserState) error {
	dt, dn := PickDefenseDeck(user)
	deck := BuildPvpDeckCharacters(user, dt, dn)
	deckJSON, err := json.Marshal(deck)
	if err != nil {
		deckJSON = []byte("[]")
	}
	snap := store.PlayerSnapshot{
		PlayerId:          user.PlayerId,
		UserName:          user.Profile.Name,
		Level:             playerLevel(user),
		MaxDeckPower:      maxDeckPower(user),
		FavoriteCostumeId: favoriteCostumeId(user),
		PvpPoint:          user.Pvp.PvpPoint,
		LastLoginDatetime: gametime.NowMillis(),
		DefenseDeckJson:   string(deckJSON),
		UpdatedAt:         gametime.NowMillis(),
	}
	return snaps.UpsertSnapshot(snap)
}

// playerLevel / maxDeckPower / favoriteCostumeId read from the real UserState fields.
// Confirm exact field paths in types.go and correct these three helpers.
func playerLevel(user *store.UserState) int32 {
	return user.Status.Level // confirm: UserStatusState.Level (or wherever player level lives)
}

func maxDeckPower(user *store.UserState) int32 {
	var max int32
	for _, note := range user.DeckTypeNotes {
		if note.MaxDeckPower > max {
			max = note.MaxDeckPower
		}
	}
	return max
}

func favoriteCostumeId(user *store.UserState) int32 {
	return user.Profile.FavoriteCostumeId // confirm field exists; else return 0
}
```

> Implementer note: if `UserStatusState` has no `Level`, find where account level is stored (search `types.go` for `Level`); if there is no profile favorite-costume field, return 0 — the field is cosmetic. These three getters are the only uncertain bindings; resolve them by reading `types.go`, do not guess at runtime.

- [ ] **Step 2: Hook GameStart (login)**

In `internal/service/user.go`, the `GameStart` handler needs access to a `SnapshotRepository`. Add a `snaps store.SnapshotRepository` field to `UserServiceServer` and its constructor (update the constructor call in `grpc.go` in Task 12's wiring — but do it now for user.go to compile; see Step 4). Inside `GameStart`, after the existing `UpdateUser`, add:

```go
	after, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		// ... existing GameStartDatetime logic ...
	})
	if s.snaps != nil {
		if err := RefreshSnapshot(s.snaps, &after); err != nil {
			log.Printf("[UserService] GameStart snapshot refresh failed: %v", err)
		}
	}
```

(Use the `UserState` returned by `UpdateUser` so the snapshot reflects the just-updated state.)

- [ ] **Step 3: Hook ReplaceDeck (PvP deck edit)**

In `internal/service/deck.go`, add a `snaps store.SnapshotRepository` field to `DeckServiceServer` + constructor. At the end of `ReplaceDeck`, after the `UpdateUser` call, capture the returned state and refresh when the PvP deck changed:

```go
	after, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		if req.Deck == nil {
			return
		}
		store.ApplyDeckReplacement(user, model.DeckType(req.DeckType), req.UserDeckNumber, deckSlotsFromProto(req.Deck), gametime.NowMillis())
	})
	if s.snaps != nil && model.DeckType(req.DeckType) == model.DeckTypePvp {
		if err := RefreshSnapshot(s.snaps, &after); err != nil {
			log.Printf("[DeckService] ReplaceDeck snapshot refresh failed: %v", err)
		}
	}
```

- [ ] **Step 4: Update constructors so it compiles**

Update `NewUserServiceServer` and `NewDeckServiceServer` signatures to accept `snaps store.SnapshotRepository` and store it. Update their call sites in `cmd/lunar-tear/grpc.go`:
- `service.NewUserServiceServer(userStore, userStore, holder, authURL, noRegister, userStore)` — pass `userStore` as the snapshot repo (SQLiteStore implements `SnapshotRepository`).
- `service.NewDeckServiceServer(userStore, userStore, userStore)`.

Ensure `registerServices`'s `userStore` parameter type includes `store.SnapshotRepository`:

```go
	userStore interface {
		store.UserRepository
		store.SessionRepository
		store.SnapshotRepository
	},
```

- [ ] **Step 5: Build**

Run: `go build ./...` (background — slow)
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add internal/service/snapshot_refresh.go internal/service/user.go internal/service/deck.go cmd/lunar-tear/grpc.go
git commit -m "feat(pvp): refresh player snapshot on login and pvp-deck edit

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

# Stage 2 — Friends

### Task 9: Friend reads

**Files:**
- Modify: `internal/service/friend.go` (rewrite)

- [ ] **Step 1: Rewrite FriendServiceServer with the directory + read RPCs**

Replace `internal/service/friend.go`. Add `dir *PlayerDirectory` to the struct + constructor (wired in Task 12). Implement the four reads:

```go
package service

import (
	"context"
	"log"

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
		// Bots are stateless; re-derive a display card from the id deterministically.
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
	maybeResetCheerDay(&user) // pure in-memory reset for display; persisted on next mutation
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
	// drop anyone already a friend or with a pending request
	filtered := cards[:0]
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
```

- [ ] **Step 2: Add the proto-mapping + bot-card helpers**

Create a SEPARATE file `internal/service/friend_helpers.go` (do not append an import block into the middle of `friend.go` — that will not compile):

```go
package service

import (
	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/store"
)

func dayBucket() int64 { return gametime.StartOfDayMillis() }

func userProto(c PlayerCard) *pb.User {
	return &pb.User{
		PlayerId:          c.PlayerId,
		UserName:          c.Name,
		MaxDeckPower:      c.MaxDeckPower,
		FavoriteCostumeId: c.FavoriteCostumeId,
		Level:             c.Level,
	}
}

func friendUserProto(c PlayerCard, e store.FriendEdge) *pb.FriendUser {
	return &pb.FriendUser{
		PlayerId:          c.PlayerId,
		UserName:          c.Name,
		MaxDeckPower:      c.MaxDeckPower,
		FavoriteCostumeId: c.FavoriteCostumeId,
		Level:             c.Level,
		CheerReceived:     e.CheerReceivedPending,
		CheerSent:         e.CheerSentToday,
		StaminaReceived:   e.StaminaReceivedToday,
	}
}

func botCardFromId(pools botPools, playerId int64) PlayerCard {
	// Re-derive a stable display card from a bot id. Name/costume come from the id seed.
	return PlayerCard{
		PlayerId:          playerId,
		Name:              botName(playerId),
		Level:             50,
		MaxDeckPower:      30000,
		FavoriteCostumeId: pick(newRand(playerId), pools.costumeIds, 1),
		IsBot:             true,
	}
}
```

Add a tiny `newRand` helper to `botgen.go`:

```go
func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
```

> Note: `lastLoginDatetime` (a `google.protobuf.Timestamp`) is left nil for the core — the client just shows no "last online". If you later populate it, import `timestamppb "google.golang.org/protobuf/types/known/timestamppb"` and use `timestamppb.New(time.UnixMilli(...))`.

- [ ] **Step 3: Add the cheer-reset stub used above**

`maybeResetCheerDay` is fully implemented in Task 11; for now add a minimal version so this compiles, then expand it in Task 11:

```go
func maybeResetCheerDay(user *store.UserState) {
	today := dayBucket()
	for pid, e := range user.Friends {
		if e.LastResetDay != today {
			e.CheerSentToday = false
			e.StaminaReceivedToday = false
			e.LastResetDay = today
			user.Friends[pid] = e
		}
	}
}
```

- [ ] **Step 4: Build**

Run: `go build ./internal/service/...`
Expected: builds clean (after wiring `dir` into the constructor in Task 12, full `go build ./...` passes; for now this package may complain about the constructor call site — that is resolved in Task 12. If so, temporarily update `grpc.go` line 104 to `service.NewFriendServiceServer(userStore, userStore, nil)` to keep it compiling, and finalize in Task 12.)

- [ ] **Step 5: Commit**

```bash
git add internal/service/friend.go internal/service/botgen.go
git commit -m "feat(friend): real read RPCs over PlayerDirectory

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Friend requests + bot auto-accept

**Files:**
- Modify: `internal/service/friend.go`
- Create: `internal/service/friend_reset_test.go` (state-machine test)

- [ ] **Step 1: Implement the mutating request RPCs**

Add to `friend.go`:

```go
func (s *FriendServiceServer) SendFriendRequest(ctx context.Context, req *pb.SendFriendRequestRequest) (*pb.SendFriendRequestResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	self, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.SendFriendRequestResponse{}, nil
	}
	target := req.PlayerId
	if target == self.PlayerId {
		return &pb.SendFriendRequestResponse{}, nil // no self-friend
	}
	now := gametime.NowMillis()

	if s.dir.IsBot(target) {
		// Bots can't accept; auto-befriend immediately on my side only.
		s.users.UpdateUser(userId, func(u *store.UserState) {
			u.Friends[target] = store.FriendEdge{PlayerId: target, BecameFriendsAt: now,
				CheerReceivedPending: true, LastResetDay: dayBucket()}
			delete(u.OutgoingFriendRequests, target)
		})
		return &pb.SendFriendRequestResponse{}, nil
	}

	// Real target: record my outgoing + their incoming (two separate UpdateUser calls).
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
		return &pb.AcceptFriendRequestResponse{}, nil // no-op if not pending
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
```

- [ ] **Step 2: Add the player-id → user-id resolver**

Real `playerId == userId` (confirmed: `CreateUser` sets `player_id = user_id`). Add to `friend.go`:

```go
// userIdForPlayer maps a real playerId to its userId. Since the server assigns
// player_id = user_id at creation, this is identity for real players.
func (s *FriendServiceServer) userIdForPlayer(playerId int64) (int64, error) {
	if _, err := s.users.LoadUser(playerId); err != nil {
		return 0, err
	}
	return playerId, nil
}
```

- [ ] **Step 3: Write a state-machine test**

The request/accept logic across two users is best tested against a real SQLite store. Create `internal/service/friend_reset_test.go` with an in-memory store if the project has a test helper for it; otherwise test the pure cheer-reset logic here and defer cross-user flow to the manual smoke test in Task 16. Minimum test:

```go
package service

import (
	"testing"

	"lunar-tear/server/internal/store"
)

func TestMaybeResetCheerDay_clearsStaleFlags(t *testing.T) {
	u := &store.UserState{Friends: map[int64]store.FriendEdge{
		1: {PlayerId: 1, CheerSentToday: true, StaminaReceivedToday: true, LastResetDay: 0},
	}}
	maybeResetCheerDay(u)
	e := u.Friends[1]
	if e.CheerSentToday || e.StaminaReceivedToday {
		t.Fatalf("stale daily flags not cleared: %+v", e)
	}
	if e.LastResetDay != dayBucket() {
		t.Fatalf("reset day not stamped")
	}
}
```

- [ ] **Step 4: Run + build**

Run: `go test ./internal/service/ -run TestMaybeResetCheerDay -v` then `go build ./internal/service/...`
Expected: PASS, builds clean.

- [ ] **Step 5: Commit**

```bash
git add internal/service/friend.go internal/service/friend_reset_test.go
git commit -m "feat(friend): requests, accept/decline/delete, bot auto-accept

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Cheer → stamina loop

**Files:**
- Modify: `internal/service/friend.go`

- [ ] **Step 1: Finalize the daily reset (include pending-cheer regeneration for bots)**

Replace `maybeResetCheerDay` with the full version: on a new day, clear my sent/collected flags; for bot friends, regenerate `CheerReceivedPending=true` so there is always something to collect.

```go
func maybeResetCheerDay(user *store.UserState) {
	today := dayBucket()
	for pid, e := range user.Friends {
		if e.LastResetDay == today {
			continue
		}
		e.CheerSentToday = false
		e.StaminaReceivedToday = false
		if IsBotId(pid) {
			e.CheerReceivedPending = true // bots always cheer you
		}
		e.LastResetDay = today
		user.Friends[pid] = e
	}
}
```

- [ ] **Step 2: Implement the cheer RPCs**

Add to `friend.go`. The stamina reward uses `store.RecoverStamina`; reuse the max-stamina lookup from `consumableitem.go` (read `UseEffectItem`) — extract it into `maxStaminaMillisFor(user, cat)` and call it here. The per-cheer reward is a constant.

```go
const cheerStaminaMillis int32 = 30 * 60 * 1000 // 30 minutes of stamina per collected cheer

func (s *FriendServiceServer) CheerFriend(ctx context.Context, req *pb.CheerFriendRequest) (*pb.CheerFriendResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	self, _ := s.users.LoadUser(userId)
	target := req.PlayerId
	s.users.UpdateUser(userId, func(u *store.UserState) {
		maybeResetCheerDay(u)
		if e, ok := u.Friends[target]; ok {
			e.CheerSentToday = true
			u.Friends[target] = e
		}
	})
	if !s.dir.IsBot(target) {
		if tid, err := s.userIdForPlayer(target); err == nil {
			s.users.UpdateUser(tid, func(u *store.UserState) {
				maybeResetCheerDay(u)
				if e, ok := u.Friends[self.PlayerId]; ok {
					e.CheerReceivedPending = true
					u.Friends[self.PlayerId] = e
				}
			})
		}
	}
	return &pb.CheerFriendResponse{}, nil
}

func (s *FriendServiceServer) BulkCheerFriend(ctx context.Context, _ *emptypb.Empty) (*pb.BulkCheerFriendResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	self, _ := s.users.LoadUser(userId)
	var cheered []int64
	s.users.UpdateUser(userId, func(u *store.UserState) {
		maybeResetCheerDay(u)
		for pid, e := range u.Friends {
			if !e.CheerSentToday {
				e.CheerSentToday = true
				u.Friends[pid] = e
				cheered = append(cheered, pid)
			}
		}
	})
	for _, pid := range cheered {
		if s.dir.IsBot(pid) {
			continue
		}
		if tid, err := s.userIdForPlayer(pid); err == nil {
			s.users.UpdateUser(tid, func(u *store.UserState) {
				maybeResetCheerDay(u)
				if e, ok := u.Friends[self.PlayerId]; ok {
					e.CheerReceivedPending = true
					u.Friends[self.PlayerId] = e
				}
			})
		}
	}
	return &pb.BulkCheerFriendResponse{PlayerId: cheered}, nil
}

func (s *FriendServiceServer) ReceiveCheer(ctx context.Context, req *pb.ReceiveCheerRequest) (*pb.ReceiveCheerResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	s.users.UpdateUser(userId, func(u *store.UserState) {
		maybeResetCheerDay(u)
		grantCheerReward(u, req.PlayerId)
	})
	return &pb.ReceiveCheerResponse{}, nil
}

func (s *FriendServiceServer) BulkReceiveCheer(ctx context.Context, _ *emptypb.Empty) (*pb.BulkReceiveCheerResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	var got []int64
	s.users.UpdateUser(userId, func(u *store.UserState) {
		maybeResetCheerDay(u)
		for pid, e := range u.Friends {
			if e.CheerReceivedPending && !e.StaminaReceivedToday {
				grantCheerReward(u, pid)
				got = append(got, pid)
			}
		}
	})
	return &pb.BulkReceiveCheerResponse{PlayerId: got}, nil
}

// grantCheerReward collects one friend's pending cheer: grants stamina and marks it collected.
func grantCheerReward(u *store.UserState, friendPlayerId int64) {
	e, ok := u.Friends[friendPlayerId]
	if !ok || !e.CheerReceivedPending || e.StaminaReceivedToday {
		return
	}
	store.RecoverStamina(u, cheerStaminaMillis, maxStaminaMillisFor(u), gametime.NowMillis())
	e.CheerReceivedPending = false
	e.StaminaReceivedToday = true
	u.Friends[friendPlayerId] = e
}
```

- [ ] **Step 3: Implement maxStaminaMillisFor**

Read `internal/service/consumableitem.go` `UseEffectItem` to see how it derives the user's max stamina (catalog lookup by the user's stamina level/account level). Mirror it. If the FriendService has no `holder`, add a `holder *runtime.Holder` field to `FriendServiceServer` + constructor (wired in Task 12) and:

First read `internal/service/consumableitem.go` `UseEffectItem` and find the expression it passes as `maxStaminaMillis` to `store.RecoverStamina` (the catalog-derived cap for the current user). If that expression is a small reusable lookup, factor it into a shared helper and call it. Concrete, self-contained implementation that compiles and works regardless:

```go
// maxStaminaMillisFor returns the stamina cap to pass to store.RecoverStamina.
// PREFERRED: replace the body with the exact cap expression used in
// consumableitem.go's UseEffectItem (catalog max for the user's level).
// FALLBACK (used if that expression is not easily reused): a generous constant
// cap so the cheer reward always lands. Stamina recovers 1 unit per the
// StaminaRecoveryDivisor (see internal/store/stamina.go); 999 units is a safe ceiling.
func maxStaminaMillisFor(u *store.UserState) int32 {
	const staminaUnitMillis int32 = 180 // confirm against store/stamina.go StaminaRecoveryDivisor
	const maxUnits int32 = 999
	return maxUnits * staminaUnitMillis
}
```

> Implementer note: prefer the real catalog cap from `consumableitem.go` — it is the accurate value. The constant fallback above is guaranteed to compile and to let the reward apply; it only risks letting stamina sit slightly above the intended max, which is cosmetic and acceptable on a private server. Confirm `staminaUnitMillis` against `store/stamina.go` before relying on the fallback.

- [ ] **Step 4: Build**

Run: `go build ./internal/service/...`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add internal/service/friend.go
git commit -m "feat(friend): cheer->stamina loop with daily reset and bot cheers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

# Stage 3 — Arena (core)

### Task 12: Register PvpService + GetTopData + point→grade + wiring

**Files:**
- Create: `internal/service/pvp.go`
- Create: `internal/service/pvp_points_test.go`
- Modify: `cmd/lunar-tear/grpc.go`

- [ ] **Step 1: Confirm the PvP grade catalog access**

Read `internal/masterdata/entities.go` `EntityMPvpGrade` and find where the grade rows are loaded into a catalog on `runtime.Catalogs` (grep `PvpGrade` under `internal/masterdata` and `internal/runtime`). If no catalog field exists yet, the grade rows may be reachable via a generic table accessor; identify the concrete access path and use it in `gradeForPoint` below. If grades are genuinely unavailable, fall back to a simple linear rank (documented in the function).

- [ ] **Step 2: Write the PvP service skeleton + GetTopData + point math**

Create `internal/service/pvp.go`:

```go
package service

import (
	"context"
	"log"
	"sort"

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
// opponent. Loss: a small fixed penalty, floored so total never goes below 0 (applied by caller).
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
```

- [ ] **Step 3: Write the point-math test**

Create `internal/service/pvp_points_test.go`:

```go
package service

import "testing"

func TestPointDelta_winLossFloor(t *testing.T) {
	if d := pointDelta(1000, 1000, true); d != 20 {
		t.Fatalf("even win want 20 got %d", d)
	}
	if d := pointDelta(1000, 2000, true); d <= 20 {
		t.Fatalf("beating stronger opp should exceed base, got %d", d)
	}
	if d := pointDelta(1000, 5000, true); d > 50 {
		t.Fatalf("win bonus must cap at 50, got %d", d)
	}
	if d := pointDelta(1000, 1000, false); d != -10 {
		t.Fatalf("loss want -10 got %d", d)
	}
	if v := applyPointDelta(5, -10); v != 0 {
		t.Fatalf("points must floor at 0, got %d", v)
	}
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/service/ -run TestPointDelta -v`
Expected: PASS.

- [ ] **Step 5: Wire everything in grpc.go**

In `cmd/lunar-tear/grpc.go` `registerServices`, after `holder` is available, build the directory and pass it where needed. Replace the Friend registration line and add Pvp:

```go
	directory := service.NewPlayerDirectory(userStore, holder)
	pb.RegisterFriendServiceServer(srv, service.NewFriendServiceServer(userStore, userStore, directory))
	pb.RegisterPvpServiceServer(srv, service.NewPvpServiceServer(userStore, userStore, userStore, directory, holder))
```

Also finalize the `NewUserServiceServer`/`NewDeckServiceServer` calls to pass `userStore` as the snapshot repo (Task 8 Step 4), and add `holder` to `NewFriendServiceServer` if Task 11 required it (update both the constructor and this call accordingly).

- [ ] **Step 6: Build**

Run: `go build ./...` (background)
Expected: builds clean.

- [ ] **Step 7: Commit**

```bash
git add internal/service/pvp.go internal/service/pvp_points_test.go cmd/lunar-tear/grpc.go
git commit -m "feat(pvp): register PvpService, GetTopData, point math, directory wiring

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Matching list

**Files:**
- Modify: `internal/service/pvp.go`

- [ ] **Step 1: Implement matching build + GetMatchingList + UpdateMatchingList**

```go
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
```

> Note: `PvpMatching` is in-memory only (not persisted, per Task 3). Since `UpdateUser` clones/saves the rest of the state, the matching slice lives on the loaded `UserState` for the duration but is rebuilt after any process restart — acceptable for a transient reroll cache.

- [ ] **Step 2: Build**

Run: `go build ./internal/service/...`
Expected: builds clean.

- [ ] **Step 3: Commit**

```bash
git add internal/service/pvp.go
git commit -m "feat(pvp): matching list build + reroll

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: StartBattle + FinishBattle

**Files:**
- Modify: `internal/service/pvp.go`

- [ ] **Step 1: Implement StartBattle**

```go
func (s *PvpServiceServer) StartBattle(ctx context.Context, req *pb.StartBattleRequest) (*pb.StartBattleResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.LoadUser(userId)
	// Find the chosen opponent in the cached matching list.
	var card PlayerCard
	found := false
	for _, e := range user.PvpMatching {
		if e.PlayerId == req.OpponentPlayerId {
			card = PlayerCard{PlayerId: e.PlayerId, Name: e.Name, PvpPoint: e.PvpPoint,
				MaxDeckPower: e.DeckPower, FavoriteCostumeId: e.MostPowerfulCostumeId, IsBot: e.IsBot}
			found = true
			break
		}
	}
	if !found {
		// Opponent no longer cached: degrade to a fresh bot rather than erroring the screen.
		card = synthBot(s.dir.pools(), user.PlayerId, 0, gametime.NowMillis(), user.Pvp.PvpPoint)
	}
	return &pb.StartBattleResponse{OpponentDeckCharacter: s.dir.DefenseDeckOf(card)}, nil
}
```

- [ ] **Step 2: Implement FinishBattle (bookkeeping + cross-user defense log)**

```go
const maxLogEntries = 30

func (s *PvpServiceServer) FinishBattle(ctx context.Context, req *pb.FinishBattleRequest) (*pb.FinishBattleResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	now := gametime.NowMillis()

	self, _ := s.users.LoadUser(userId)
	beforePoint := self.Pvp.PvpPoint
	beforeRank, _ := s.snaps.RankOfPlayer(self.PlayerId)

	// Opponent identity from the matching cache (for the log + cross-user defense write).
	var opp store.MatchingEntry
	for _, e := range self.PvpMatching {
		if e.PlayerId == req.OpponentPlayerId {
			opp = e
			break
		}
	}
	delta := pointDelta(beforePoint, opp.PvpPoint, req.IsVictory)

	after, _ := s.users.UpdateUser(userId, func(u *store.UserState) {
		u.Pvp.PvpPoint = applyPointDelta(u.Pvp.PvpPoint, delta)
		if req.IsVictory {
			u.Pvp.AttackWinCount++
		} else {
			u.Pvp.AttackLoseCount++
		}
		u.Pvp.LastFinishDay = gametime.StartOfDayMillis()
		entry := store.BattleLogEntry{
			Seq: now, OpponentPlayerId: opp.PlayerId, OpponentName: opp.Name,
			OpponentPvpPoint: opp.PvpPoint, OpponentDeckPower: opp.DeckPower,
			IsVictory: req.IsVictory, BattleDatetime: now, FluctuatedPoint: delta, Rank: int32(beforeRank),
		}
		u.PvpAttackLog = append(u.PvpAttackLog, entry)
		u.PvpAttackLog = capLog(u.PvpAttackLog)
	})

	// Refresh my snapshot so my new pvp_point is reflected in ranking/matching immediately.
	if err := RefreshSnapshot(s.snaps, &after); err != nil {
		log.Printf("[PvpService] FinishBattle snapshot refresh failed: %v", err)
	}

	// Real opponent gets a defense-log entry (their perspective: they "defended", win/loss inverted).
	if !s.dir.IsBot(opp.PlayerId) && opp.PlayerId != 0 {
		if oid, err := s.userIdForPlayer(opp.PlayerId); err == nil {
			s.users.UpdateUser(oid, func(u *store.UserState) {
				defWin := !req.IsVictory
				if defWin {
					u.Pvp.DefenseWinCount++
				} else {
					u.Pvp.DefenseLoseCount++
				}
				entry := store.BattleLogEntry{
					Seq: now, OpponentPlayerId: self.PlayerId, OpponentName: self.Profile.Name,
					OpponentPvpPoint: beforePoint, IsVictory: defWin, BattleDatetime: now,
				}
				u.PvpDefenseLog = append(u.PvpDefenseLog, entry)
				u.PvpDefenseLog = capLog(u.PvpDefenseLog)
			})
		}
	}

	afterRank, _ := s.snaps.RankOfPlayer(self.PlayerId)
	if afterRank == 0 {
		afterRank = 1
	}
	return &pb.FinishBattleResponse{
		BeforePvpPoint: beforePoint, BeforeRank: int32(beforeRank),
		AfterPvpPoint: after.Pvp.PvpPoint, AfterRank: int32(afterRank),
	}, nil
}

func capLog(log []store.BattleLogEntry) []store.BattleLogEntry {
	if len(log) <= maxLogEntries {
		return log
	}
	return log[len(log)-maxLogEntries:]
}
```

- [ ] **Step 3: Add the player-id resolver to PvpService**

Reuse the same identity mapping as Friends (player_id == user_id). Add:

```go
func (s *PvpServiceServer) userIdForPlayer(playerId int64) (int64, error) {
	if _, err := s.users.LoadUser(playerId); err != nil {
		return 0, err
	}
	return playerId, nil
}
```

- [ ] **Step 4: Build**

Run: `go build ./internal/service/...`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add internal/service/pvp.go
git commit -m "feat(pvp): StartBattle deck handoff + FinishBattle bookkeeping

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 15: Ranking, season result, log lists

**Files:**
- Modify: `internal/service/pvp.go`

- [ ] **Step 1: Implement the remaining reads**

```go
func (s *PvpServiceServer) GetRanking(ctx context.Context, req *pb.GetRankingRequest) (*pb.GetRankingResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.LoadUser(userId)
	const pageSize = 50
	offset := int(req.RankFrom)
	if offset < 0 {
		offset = 0
	}
	snaps, _ := s.snaps.ListSnapshotsByPointDesc(offset, pageSize)
	var rows []*pb.RankingUser
	for i, sn := range snaps {
		rows = append(rows, &pb.RankingUser{
			Rank: int32(offset + i + 1), PlayerId: sn.PlayerId, Name: sn.UserName,
			PvpPoint: sn.PvpPoint, DeckPower: sn.MaxDeckPower, FavoriteCostumeId: sn.FavoriteCostumeId,
		})
	}
	count, _ := s.snaps.CountSnapshots()
	myRank, _ := s.snaps.RankOfPlayer(user.PlayerId)
	return &pb.GetRankingResponse{
		RankingUser: rows, UserCount: int32(count), RankingPosition: int32(myRank),
	}, nil
}

func (s *PvpServiceServer) GetSeasonResult(ctx context.Context, _ *emptypb.Empty) (*pb.GetSeasonResultResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.LoadUser(userId)
	p := user.Pvp
	var defRate int32
	if total := p.DefenseWinCount + p.DefenseLoseCount; total > 0 {
		defRate = (p.DefenseWinCount * 1000) / total
	}
	return &pb.GetSeasonResultResponse{
		AttackWinCount: p.AttackWinCount, AttackLoseCount: p.AttackLoseCount,
		AttackPvpPoint: p.PvpPoint, DefenseWinRatePermil: defRate, DefensePvpPoint: p.PvpPoint,
	}, nil
}

func logToProto(entries []store.BattleLogEntry) []*pb.BattleLog {
	// Most-recent first.
	sorted := append([]store.BattleLogEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq > sorted[j].Seq })
	var out []*pb.BattleLog
	for _, e := range sorted {
		out = append(out, &pb.BattleLog{
			PlayerId: e.OpponentPlayerId, Name: e.OpponentName, PvpPoint: e.OpponentPvpPoint,
			DeckPower: e.OpponentDeckPower, IsVictory: e.IsVictory, FluctuatedPvpPoint: e.FluctuatedPoint,
			Rank: e.Rank,
		})
	}
	return out
}

func (s *PvpServiceServer) GetAttackLogList(ctx context.Context, _ *emptypb.Empty) (*pb.GetAttackLogListResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.LoadUser(userId)
	return &pb.GetAttackLogListResponse{AttackLog: logToProto(user.PvpAttackLog)}, nil
}

func (s *PvpServiceServer) GetDefenseLogList(ctx context.Context, _ *emptypb.Empty) (*pb.GetDefenseLogListResponse, error) {
	userId := CurrentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.LoadUser(userId)
	return &pb.GetDefenseLogListResponse{DefenseLog: logToProto(user.PvpDefenseLog)}, nil
}
```

> Note: `BattleLog.battleDatetime` is a `google.protobuf.Timestamp`; leaving it nil is acceptable for the core. Populate it later if the client needs the timestamp column.

- [ ] **Step 2: Build + vet**

Run: `go build ./...` (background) then `go vet ./internal/...`
Expected: both clean.

- [ ] **Step 3: Commit**

```bash
git add internal/service/pvp.go
git commit -m "feat(pvp): ranking, season result, attack/defense log lists

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 16: Full build, run, smoke test

**Files:** none (verification)

- [ ] **Step 1: Full build + vet + unit tests**

Run (from `lt-upstream/server`):
```bash
go build ./...
go vet ./internal/...
go test ./internal/service/ ./internal/store/...
```
Expected: all clean/PASS.

- [ ] **Step 2: Run the server and exercise the features**

Run: `make dev` (or `go run ./cmd/wizard`). With a patched client (or two accounts), verify:
- Friends: open Friends screen (no crash), Recommended shows real players + bots, send a request to a real second account → it appears in their request list → accept → both see each other. Send a request to a bot → instantly a friend. Cheer all → Receive all → stamina increases (visible in the client header).
- Arena: open Arena (no `Unimplemented` crash). GetTopData shows your points/rank. Matching shows ~5 opponents. Start a battle, finish it (win) → points increase; lose → points drop but not below 0. Ranking lists players. Attack log shows your last match; if you attacked a real account, that account's Defense log shows the entry.

- [ ] **Step 3: Capture verification output**

Note in the PR/commit what was observed (per `superpowers:verification-before-completion`): paste the `go test` output and a short note of the manual checks that passed. If the `PvpDeckCharacter` projection causes a client-side battle issue, that is the most likely failure point — revisit Task 5 to populate the missing sub-collections (character board / awaken) the client requires.

- [ ] **Step 4: Final commit (if any fixups)**

```bash
git add -A
git commit -m "test(arena-friends): verification fixups

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Deferred to a follow-up spec (do not implement here)

- PvP seasons rolling over; `weeklyGradeResult` / `seasonRankResult` / `weeklyRankResult` population.
- Weekly/season rank reward tiers and per-match reward grants (`pvpGradeOneMatchRewardId`, `pvpGradeGroupId`).
- Richer `PvpDeckCharacter` sub-collections (character-board abilities/status-ups, awaken, stained glass, lottery effects) if the core minimal projection proves sufficient for the client.
- Friend `lastLoginDatetime` / battle-log timestamps (cosmetic `Timestamp` fields left nil in the core).
