package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type track struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Artist string  `json:"artist"`
	Year   int     `json:"year"`
	File   string  `json:"file,omitempty"`
	Start  float64 `json:"start"`
}

type card struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Year   int    `json:"year"`
}

func (t track) card() card { return card{t.Title, t.Artist, t.Year} }

type player struct {
	ID              string
	Name            string
	DiscordID       string
	Team            string
	Client          *client
	ReadyGeneration int
	DraftAck        string
}

// Each color shares one timeline; a player without a color has their own.
type timeline struct {
	ID      string
	Team    string
	Members []*player
	Cards   []card
	Tokens  int
}

func (t *timeline) includes(p *player) bool {
	for _, member := range t.Members {
		if member.ID == p.ID {
			return true
		}
	}
	return false
}

func (t *timeline) online() bool {
	for _, p := range t.Members {
		if p.Client != nil {
			return true
		}
	}
	return false
}

func teamName(color string) string {
	switch color {
	case "blue":
		return "Blue team"
	case "red":
		return "Red team"
	case "green":
		return "Green team"
	case "yellow":
		return "Yellow team"
	}
	return ""
}

type round struct {
	Draft         guessDraft
	ID            string
	Track         track
	Number        int
	Generation    int
	StartAt       int64
	PrepareUntil  time.Time
	Result        *resultView
	Locked        *lockedGuess
	StealUntil    time.Time
	StealEligible []string
	Steals        []stealView
	Passed        []string
}

// Pointer fields let teammates edit one part without overwriting the others.
type guessDraft struct {
	Position *int    `json:"position,omitempty"`
	Artist   *string `json:"artist,omitempty"`
	Title    *string `json:"title,omitempty"`
}

func (r *room) editDraft(p *player, patch *guessDraft, id string) error {
	if r.Phase != "playing" || !r.isTurn(p) {
		return errors.New("Only the playing team can edit this guess.")
	}
	if patch == nil || id == "" || len(id) > 64 {
		return errors.New("Invalid draft update.")
	}
	if patch.Position != nil && (*patch.Position < 0 || *patch.Position > len(r.currentTimeline().Cards)) {
		return errors.New("Choose a gap in the timeline.")
	}
	for _, value := range []*string{patch.Artist, patch.Title} {
		if value != nil && len([]rune(*value)) > 100 {
			return errors.New("Keep the artist and title to 100 characters each.")
		}
	}
	if patch.Position != nil {
		r.Round.Draft.Position = patch.Position
	}
	if patch.Artist != nil {
		r.Round.Draft.Artist = patch.Artist
	}
	if patch.Title != nil {
		r.Round.Draft.Title = patch.Title
	}
	p.DraftAck = id
	return nil
}

type lockedGuess struct {
	PlayerID string
	Position int
	Artist   string
	Title    string
}

type stealView struct {
	TimelineID string `json:"timelineId"`
	Position   int    `json:"position"`
}

type resultView struct {
	Card        card   `json:"card"`
	Correct     bool   `json:"correct"`
	Skipped     bool   `json:"skipped"`
	PlayerID    string `json:"playerId"`
	AwardedTo   string `json:"awardedTo,omitempty"`
	TokenEarned bool   `json:"tokenEarned"`
}

type room struct {
	Code         string
	HostID       string
	Players      []*player
	Timelines    []*timeline
	Phase        string
	Target       int
	Turn         int
	Deck         []track
	Round        *round
	Played       int
	Winners      []string
	FinishReason string
	Updated      time.Time
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func roomCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

func shuffle(deck []track) {
	for i := len(deck) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			panic(err)
		}
		j := int(n.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}
}

func (r *room) findPlayer(id string) *player {
	for _, p := range r.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (r *room) lobbyTimelines() []*timeline {
	timelines := []*timeline{}
	teams := map[string]*timeline{}
	for _, p := range r.Players {
		if p.Team == "" {
			timelines = append(timelines, &timeline{ID: p.ID, Members: []*player{p}})
			continue
		}
		t := teams[p.Team]
		if t == nil {
			t = &timeline{ID: "team-" + p.Team, Team: p.Team}
			teams[p.Team] = t
			timelines = append(timelines, t)
		}
		t.Members = append(t.Members, p)
	}
	return timelines
}

func (r *room) currentTimeline() *timeline {
	if r.Turn < 0 || r.Turn >= len(r.Timelines) {
		return nil
	}
	return r.Timelines[r.Turn]
}

func (r *room) isTurn(p *player) bool {
	t := r.currentTimeline()
	return t != nil && t.includes(p)
}

func (r *room) playerTimeline(p *player) *timeline {
	for _, t := range r.Timelines {
		if t.includes(p) {
			return t
		}
	}
	return nil
}

func (r *room) start(tracks []track, now time.Time) error {
	if r.Phase != "lobby" {
		return errors.New("This game has already started.")
	}
	timelines := r.lobbyTimelines()
	if len(timelines) == 0 {
		return errors.New("At least one player must join before starting.")
	}
	if len(tracks) < len(timelines)+1 {
		return fmt.Errorf("This game needs at least %d songs: one starting card per team or solo player, plus a song to guess.", len(timelines)+1)
	}
	r.Timelines = timelines
	r.Deck = append([]track(nil), tracks...)
	shuffle(r.Deck)
	for _, t := range r.Timelines {
		t.Cards = []card{r.draw().card()}
	}
	r.Turn = 0
	for i, t := range r.Timelines {
		if t.online() {
			r.Turn = i
			break
		}
	}
	r.Played = 0
	r.Winners = nil
	r.FinishReason = ""
	r.nextRound(now)
	return nil
}

func (r *room) draw() track {
	t := r.Deck[0]
	r.Deck = r.Deck[1:]
	return t
}

func (r *room) nextRound(now time.Time) {
	if len(r.Deck) == 0 {
		r.finish("Every song has had its moment.")
		return
	}
	r.Phase = "playing"
	r.Played++
	r.Round = &round{ID: newID(), Track: r.draw(), Number: r.Played}
	r.prepare(now)
}

func (r *room) prepare(now time.Time) {
	r.Round.Generation++
	r.Round.StartAt = 0
	r.Round.PrepareUntil = now.Add(8 * time.Second)
	for _, p := range r.Players {
		p.ReadyGeneration = 0
	}
}

func (r *room) schedule(now time.Time) bool {
	if r.Phase != "playing" || r.Round == nil || r.Round.StartAt != 0 {
		return false
	}
	connected, ready := 0, 0
	for _, p := range r.Players {
		if p.Client != nil {
			connected++
			if p.ReadyGeneration == r.Round.Generation {
				ready++
			}
		}
	}
	if connected > 0 && (connected == ready || !now.Before(r.Round.PrepareUntil)) {
		r.Round.StartAt = now.Add(1200 * time.Millisecond).UnixMilli()
		return true
	}
	return false
}

func validPlacement(cards []card, year, position int) bool {
	return position >= 0 && position <= len(cards) && (position == 0 || cards[position-1].Year <= year) && (position == len(cards) || year <= cards[position].Year)
}

func (r *room) place(p *player, position int, now time.Time) error {
	return r.lockGuess(p, position, "", "", now)
}

func (r *room) lockGuess(p *player, position int, artist, title string, now time.Time) error {
	if r.Phase != "playing" || !r.isTurn(p) {
		return errors.New("It is not your turn to place a song.")
	}
	if r.Round.StartAt == 0 || now.UnixMilli() < r.Round.StartAt {
		return errors.New("Wait for the song to start.")
	}
	t := r.currentTimeline()
	if position < 0 || position > len(t.Cards) {
		return errors.New("Choose a gap in the timeline.")
	}
	if len([]rune(artist)) > 100 || len([]rune(title)) > 100 {
		return errors.New("Keep the artist and title to 100 characters each.")
	}
	r.Round.Locked = &lockedGuess{p.ID, position, artist, title}
	for _, opponent := range r.Timelines {
		if opponent != t && opponent.Tokens > 0 && opponent.online() {
			r.Round.StealEligible = append(r.Round.StealEligible, opponent.ID)
		}
	}
	if len(r.Round.StealEligible) == 0 {
		r.resolveGuess()
	} else {
		r.Phase = "stealing"
		r.Round.StealUntil = now.Add(20 * time.Second)
	}
	return nil
}

func (r *room) nextTurn() int {
	turn := r.Turn
	for range r.Timelines {
		turn = (turn + 1) % len(r.Timelines)
		if r.Timelines[turn].online() {
			break
		}
	}
	return turn
}

func (r *room) canContinue(p *player) bool {
	if r.Phase != "reveal" {
		return false
	}
	if len(r.Winners) > 0 || len(r.Deck) == 0 {
		return p.ID == r.HostID || r.isTurn(p)
	}
	return r.Timelines[r.nextTurn()].includes(p)
}

func (r *room) advance(now time.Time) {
	if len(r.Winners) > 0 {
		r.Phase = "finished"
		return
	}
	r.Turn = r.nextTurn()
	r.nextRound(now)
}

func (r *room) finish(reason string) {
	r.Phase = "finished"
	r.FinishReason = reason
	best := 0
	r.Winners = nil
	for _, t := range r.Timelines {
		if len(t.Cards) > best {
			best = len(t.Cards)
			r.Winners = []string{t.ID}
		} else if len(t.Cards) == best {
			r.Winners = append(r.Winners, t.ID)
		}
	}
}

type playerView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Team   string `json:"team"`
	Online bool   `json:"online"`
	Ready  bool   `json:"ready"`
}

type timelineView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Team      string   `json:"team"`
	PlayerIDs []string `json:"playerIds"`
	Cards     []card   `json:"cards"`
	Online    bool     `json:"online"`
	Tokens    int      `json:"tokens"`
}

type roundView struct {
	ID             string      `json:"id"`
	Number         int         `json:"number"`
	Generation     int         `json:"generation"`
	StartAt        int64       `json:"startAt"`
	Offset         float64     `json:"offset"`
	Result         *resultView `json:"result,omitempty"`
	LockedPosition *int        `json:"lockedPosition,omitempty"`
	StealUntil     int64       `json:"stealUntil,omitempty"`
	StealEligible  []string    `json:"stealEligible"`
	Steals         []stealView `json:"steals"`
	Passed         []string    `json:"passed"`
}

type roomView struct {
	Draft        *guessDraft    `json:"draft,omitempty"`
	DraftAck     string         `json:"draftAck,omitempty"`
	Code         string         `json:"code"`
	HostID       string         `json:"hostId"`
	Players      []playerView   `json:"players"`
	Timelines    []timelineView `json:"timelines"`
	Phase        string         `json:"phase"`
	Target       int            `json:"target"`
	TurnID       string         `json:"turnId"`
	NextTurnID   string         `json:"nextTurnId,omitempty"`
	Round        *roundView     `json:"round,omitempty"`
	TrackCount   int            `json:"trackCount"`
	Remaining    int            `json:"remaining"`
	Winners      []string       `json:"winners"`
	FinishReason string         `json:"finishReason"`
}

func (r *room) viewFor(trackCount int, p *player) roomView {
	v := r.view(trackCount)
	if r.Phase == "playing" && r.Round != nil && r.isTurn(p) {
		draft := r.Round.Draft
		v.Draft = &draft
		v.DraftAck = p.DraftAck
	}
	return v
}

func (r *room) view(trackCount int) roomView {
	v := roomView{Code: r.Code, HostID: r.HostID, Phase: r.Phase, Target: r.Target, TrackCount: trackCount, Remaining: len(r.Deck), Winners: r.Winners, FinishReason: r.FinishReason, Players: []playerView{}}
	for _, p := range r.Players {
		v.Players = append(v.Players, playerView{p.ID, p.Name, p.Team, p.Client != nil, r.Round != nil && p.ReadyGeneration == r.Round.Generation})
	}
	timelines := r.Timelines
	if r.Phase == "lobby" {
		timelines = r.lobbyTimelines()
	}
	v.Timelines = []timelineView{}
	for _, t := range timelines {
		name := teamName(t.Team)
		if t.Team == "" {
			name = t.Members[0].Name
		}
		ids := []string{}
		for _, p := range t.Members {
			ids = append(ids, p.ID)
		}
		v.Timelines = append(v.Timelines, timelineView{t.ID, name, t.Team, ids, append([]card{}, t.Cards...), t.online(), t.Tokens})
	}
	if t := r.currentTimeline(); t != nil && r.Phase != "lobby" {
		v.TurnID = t.ID
		if r.Phase == "reveal" && len(r.Winners) == 0 && len(r.Deck) > 0 {
			v.NextTurnID = r.Timelines[r.nextTurn()].ID
		}
	}
	if r.Round != nil {
		q := r.Round
		v.Round = &roundView{ID: q.ID, Number: q.Number, Generation: q.Generation, StartAt: q.StartAt, Offset: q.Track.Start, Result: q.Result,
			StealEligible: append([]string{}, q.StealEligible...), Steals: append([]stealView{}, q.Steals...), Passed: append([]string{}, q.Passed...)}
		if q.Locked != nil {
			position := q.Locked.Position
			v.Round.LockedPosition = &position
		}
		if !q.StealUntil.IsZero() {
			v.Round.StealUntil = q.StealUntil.UnixMilli()
		}
	}
	return v
}
