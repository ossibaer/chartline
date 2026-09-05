package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const maxPlayers = 10

type track struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Year     int     `json:"year"`
	File     string  `json:"file,omitempty"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	Demo     int     `json:"demo,omitempty"`
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
	Cards           []card
	Client          *client
	ReadyGeneration int
}

type round struct {
	ID           string
	Track        track
	Number       int
	Generation   int
	StartAt      int64
	PrepareUntil time.Time
	Result       *resultView
}

type resultView struct {
	Card     card   `json:"card"`
	Correct  bool   `json:"correct"`
	Skipped  bool   `json:"skipped"`
	PlayerID string `json:"playerId"`
}

type room struct {
	Code         string
	HostID       string
	Players      []*player
	Phase        string
	Library      string
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

func (r *room) start(tracks []track, now time.Time) error {
	if r.Phase != "lobby" {
		return errors.New("This game has already started.")
	}
	if len(tracks) < len(r.Players)+1 {
		return fmt.Errorf("Add at least %d tracks: one starting card per player, plus a song to guess.", len(r.Players)+1)
	}
	r.Deck = append([]track(nil), tracks...)
	shuffle(r.Deck)
	for _, p := range r.Players {
		p.Cards = []card{r.draw().card()}
	}
	r.Turn = 0
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
	if r.Phase != "playing" || r.Players[r.Turn].ID != p.ID {
		return errors.New("It is not your turn to place a song.")
	}
	if r.Round.StartAt == 0 || now.UnixMilli() < r.Round.StartAt {
		return errors.New("Wait for the song to start.")
	}
	if position < 0 || position > len(p.Cards) {
		return errors.New("Choose a gap in the timeline.")
	}
	correct := validPlacement(p.Cards, r.Round.Track.Year, position)
	if correct {
		p.Cards = append(p.Cards, card{})
		copy(p.Cards[position+1:], p.Cards[position:])
		p.Cards[position] = r.Round.Track.card()
	}
	r.Round.Result = &resultView{Card: r.Round.Track.card(), Correct: correct, PlayerID: p.ID}
	r.Phase = "reveal"
	if len(p.Cards) >= r.Target {
		r.Winners = []string{p.ID}
		r.FinishReason = "The timeline is complete."
	}
	return nil
}

func (r *room) advance(now time.Time) {
	if len(r.Winners) > 0 {
		r.Phase = "finished"
		return
	}
	for range r.Players {
		r.Turn = (r.Turn + 1) % len(r.Players)
		if r.Players[r.Turn].Client != nil {
			break
		}
	}
	r.nextRound(now)
}

func (r *room) finish(reason string) {
	r.Phase = "finished"
	r.FinishReason = reason
	best := 0
	r.Winners = nil
	for _, p := range r.Players {
		if len(p.Cards) > best {
			best = len(p.Cards)
			r.Winners = []string{p.ID}
		} else if len(p.Cards) == best {
			r.Winners = append(r.Winners, p.ID)
		}
	}
}

type playerView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Cards  []card `json:"cards"`
	Online bool   `json:"online"`
	Ready  bool   `json:"ready"`
}

type roundView struct {
	ID         string      `json:"id"`
	Number     int         `json:"number"`
	Generation int         `json:"generation"`
	StartAt    int64       `json:"startAt"`
	Offset     float64     `json:"offset"`
	Duration   float64     `json:"duration"`
	Result     *resultView `json:"result,omitempty"`
}

type roomView struct {
	Code         string       `json:"code"`
	HostID       string       `json:"hostId"`
	Players      []playerView `json:"players"`
	Phase        string       `json:"phase"`
	Library      string       `json:"library"`
	Target       int          `json:"target"`
	TurnID       string       `json:"turnId"`
	Round        *roundView   `json:"round,omitempty"`
	TrackCount   int          `json:"trackCount"`
	Remaining    int          `json:"remaining"`
	Winners      []string     `json:"winners"`
	FinishReason string       `json:"finishReason"`
}

func (r *room) view(trackCount int) roomView {
	v := roomView{Code: r.Code, HostID: r.HostID, Phase: r.Phase, Library: r.Library, Target: r.Target, TrackCount: trackCount, Remaining: len(r.Deck), Winners: r.Winners, FinishReason: r.FinishReason, Players: []playerView{}}
	for _, p := range r.Players {
		cards := append([]card{}, p.Cards...)
		v.Players = append(v.Players, playerView{p.ID, p.Name, cards, p.Client != nil, r.Round != nil && p.ReadyGeneration == r.Round.Generation})
	}
	if len(r.Players) > 0 {
		v.TurnID = r.Players[r.Turn].ID
	}
	if r.Round != nil {
		q := r.Round
		v.Round = &roundView{q.ID, q.Number, q.Generation, q.StartAt, q.Track.Start, q.Track.Duration, q.Result}
	}
	return v
}
