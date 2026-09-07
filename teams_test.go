package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTeamSelectionAndStartingDeal(t *testing.T) {
	now := time.Now()
	a := &app{}
	r := &room{Phase: "lobby", Target: 5}
	colors := []string{"blue", "red", "green", "yellow"}
	for i := range 30 {
		p := &player{ID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("Player %d", i)}
		r.Players = append(r.Players, p)
		if i < 28 {
			if err := a.applyAction(r, p, action{Type: "team", Team: colors[i%4]}, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	p := r.Players[0]
	for _, color := range []string{"purple", "Blue", "solo", " blue"} {
		if err := a.applyAction(r, p, action{Type: "team", Team: color}, now); err == nil || p.Team != "blue" {
			t.Fatalf("invalid color %q was accepted or changed the team", color)
		}
	}
	for _, color := range []string{"red", "", "blue"} {
		if err := a.applyAction(r, p, action{Type: "team", Team: color}, now); err != nil || p.Team != color {
			t.Fatalf("could not switch to %q: %v", color, err)
		}
	}
	v := r.view(7)
	if len(v.Timelines) != 6 || v.TurnID != "" || len(v.Players) != 30 {
		t.Fatalf("want four teams and two separate solo timelines: %+v", v)
	}
	for i, timeline := range v.Timelines {
		if len(timeline.Cards) != 0 {
			t.Fatal("lobby was dealt cards")
		}
		if i < 4 && (timeline.Team != colors[i] || len(timeline.PlayerIDs) != 7) {
			t.Fatalf("incorrect team grouping: %+v", timeline)
		}
	}
	if v.Timelines[4].ID == v.Timelines[5].ID || v.Timelines[4].Team != "" || v.Timelines[5].Team != "" {
		t.Fatal("uncolored players were grouped together")
	}
	tracks := make([]track, 7)
	for i := range tracks {
		tracks[i] = track{Title: fmt.Sprintf("Track %d", i), Year: 1970 + i}
	}
	if err := r.start(tracks[:6], now); err == nil || r.Phase != "lobby" || len(r.Timelines) != 0 {
		t.Fatal("insufficient library should leave the lobby unchanged")
	}
	if err := r.start(tracks, now); err != nil {
		t.Fatal("30 players in six timelines should need only seven tracks:", err)
	}
	dealt := map[string]bool{r.Round.Track.Title: true}
	for _, timeline := range r.Timelines {
		if len(timeline.Cards) != 1 || dealt[timeline.Cards[0].Title] {
			t.Fatalf("starting cards are missing or duplicated: %+v", timeline)
		}
		dealt[timeline.Cards[0].Title] = true
	}
	for _, phase := range []string{"playing", "reveal", "finished"} {
		r.Phase = phase
		if err := a.applyAction(r, p, action{Type: "team", Team: "red"}, now); err == nil || p.Team != "blue" {
			t.Fatalf("teams changed during %s", phase)
		}
	}
}

func TestTeamTurnsPermissionsAndOfflineMembers(t *testing.T) {
	now := time.Now()
	a := &app{demos: demoLibrary()}
	blue1 := &player{ID: "blue1", Team: "blue"}
	red := &player{ID: "red1", Team: "red", Client: &client{}}
	blue2 := &player{ID: "blue2", Team: "blue", Client: &client{}}
	solo := &player{ID: "solo", Client: &client{}}
	offlineSolo := &player{ID: "offline-solo"}
	green := &player{ID: "green1", Team: "green"}
	r := &room{HostID: solo.ID, Phase: "lobby", Library: "demo", Target: 10, Players: []*player{blue1, red, blue2, solo, offlineSolo, green}}
	if err := r.start(a.demos, now); err != nil {
		t.Fatal(err)
	}
	if r.currentTimeline().ID != "team-blue" || !r.isTurn(blue2) || r.isTurn(red) {
		t.Fatal("online teammate should be able to act with the first member offline")
	}
	r.Round.StartAt = now.Add(-time.Second).UnixMilli()
	for _, p := range []*player{red, solo, offlineSolo} {
		if err := a.applyAction(r, p, action{Type: "place", RoundID: r.Round.ID}, now); err == nil {
			t.Fatalf("%s placed on another timeline", p.ID)
		}
	}
	if err := a.applyAction(r, red, action{Type: "replay", RoundID: r.Round.ID}, now); err == nil {
		t.Fatal("opposing team replayed the clip")
	}
	if err := a.applyAction(r, blue2, action{Type: "replay", RoundID: r.Round.ID}, now); err != nil {
		t.Fatal("teammate could not replay:", err)
	}
	r.Round.StartAt = now.Add(-time.Second).UnixMilli()
	r.Round.Track.Year = 2100
	if err := a.applyAction(r, blue2, action{Type: "place", RoundID: "stale", Position: 1}, now); err == nil {
		t.Fatal("stale team answer accepted")
	}
	if err := a.applyAction(r, blue2, action{Type: "place", RoundID: r.Round.ID, Position: 1}, now); err != nil {
		t.Fatal("teammate could not submit:", err)
	}
	if len(r.Timelines[0].Cards) != 2 || len(r.Timelines[1].Cards) != 1 || r.Round.Result.PlayerID != blue2.ID {
		t.Fatal("answer did not award exactly one card to the team")
	}
	if err := a.applyAction(r, blue1, action{Type: "place", RoundID: r.Round.ID, Position: 2}, now); err == nil {
		t.Fatal("a second teammate submitted the same round")
	}
	if err := a.applyAction(r, red, action{Type: "next", RoundID: r.Round.ID}, now); err == nil {
		t.Fatal("opposing team advanced the round")
	}
	if err := a.applyAction(r, blue2, action{Type: "next", RoundID: r.Round.ID}, now); err != nil {
		t.Fatal("teammate could not advance:", err)
	}
	if r.currentTimeline().ID != "team-red" {
		t.Fatal("red team did not get the next turn")
	}
	for _, want := range []string{solo.ID, "team-blue"} {
		if err := a.applyAction(r, solo, action{Type: "discard", RoundID: r.Round.ID}, now); err != nil {
			t.Fatal(err)
		}
		if err := a.applyAction(r, solo, action{Type: "next", RoundID: r.Round.ID}, now); err != nil {
			t.Fatal(err)
		}
		if got := r.currentTimeline().ID; got != want {
			t.Fatalf("next turn = %s, want %s (one turn per timeline, skipping fully offline timelines)", got, want)
		}
	}
}

func TestTeamWinsTiesAndReset(t *testing.T) {
	now := time.Now()
	a := &app{demos: demoLibrary(), sessions: map[string]session{"offline-token": {Room: "room", Player: "offline"}}}
	p := &player{ID: "a", Team: "yellow", Client: &client{}}
	q := &player{ID: "b", Team: "yellow", Client: &client{}}
	solo := &player{ID: "solo", Client: &client{}}
	offline := &player{ID: "offline", Team: "yellow"}
	r := &room{HostID: p.ID, Phase: "lobby", Target: 2, Players: []*player{p, q, solo, offline}}
	if err := r.start(a.demos, now); err != nil {
		t.Fatal(err)
	}
	r.Round.StartAt = now.Add(-time.Second).UnixMilli()
	r.Round.Track.Year = 2100
	if err := r.place(q, 1, now); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Winners, []string{"team-yellow"}) {
		t.Fatalf("a team reaching the target should win once: %v", r.Winners)
	}
	r.advance(now)
	if r.Phase != "finished" {
		t.Fatal("team win did not finish")
	}
	r.Timelines[1].Cards = append([]card{}, r.Timelines[0].Cards...)
	r.Winners = nil
	r.Deck = nil
	r.advance(now)
	if !reflect.DeepEqual(r.Winners, []string{"team-yellow", solo.ID}) {
		t.Fatalf("team/solo tie counted teammates separately: %v", r.Winners)
	}
	if err := a.applyAction(r, p, action{Type: "reset"}, now); err != nil {
		t.Fatal(err)
	}
	v := r.view(len(a.demos))
	if len(r.Players) != 3 || len(a.sessions) != 0 || len(r.Timelines) != 0 || v.TurnID != "" || r.Round != nil || len(r.Winners) != 0 {
		t.Fatal("reset did not clear game state and remove offline players")
	}
	if p.Team != "yellow" || q.Team != "yellow" || len(v.Timelines) != 2 || len(v.Timelines[0].Cards) != 0 {
		t.Fatal("reset should preserve colors and empty shared cards")
	}
	if err := a.applyAction(r, q, action{Type: "team", Team: ""}, now); err != nil {
		t.Fatal(err)
	}
	if err := r.start(a.demos, now); err != nil {
		t.Fatal(err)
	}
	if len(r.Timelines) != 3 || len(r.Timelines[0].Members) != 1 || len(r.Timelines[0].Cards) != 1 {
		t.Fatal("new game did not use updated teams and fresh cards")
	}
}

func TestLiveTeamSelectionAndReconnect(t *testing.T) {
	a, s := testApp(t)
	host := enterTest(t, s, "", "Host")
	c1 := connectTest(t, s, host)
	readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" })
	guest := enterTest(t, s, host.Code, "Teammate")
	c2 := connectTest(t, s, guest)
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" })
	solo := enterTest(t, s, host.Code, "Solo")
	c3 := connectTest(t, s, solo)
	readUntil(t, c3, func(m wireMessage) bool { return m.Type == "state" })
	writeAction(t, c1, action{Type: "team", Team: "green"})
	writeAction(t, c2, action{Type: "team", Team: "green"})
	for _, c := range []*websocket.Conn{c1, c2, c3} {
		m := readUntil(t, c, func(m wireMessage) bool {
			return m.Type == "state" && len(m.Room.Players) == 3 && len(m.Room.Timelines) == 2
		})
		if len(m.Room.Timelines[0].PlayerIDs) != 2 || m.Room.Players[1].Team != "green" || m.Room.Timelines[1].ID != solo.PlayerID {
			t.Fatal("clients did not see the same team and solo grouping")
		}
	}
	writeAction(t, c1, action{Type: "start"})
	playing := readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "playing" })
	if len(playing.Room.Timelines[0].Cards) != 1 || playing.Room.TurnID != "team-green" {
		t.Fatal("team did not start with one shared card")
	}
	writeAction(t, c2, action{Type: "team", Team: "red"})
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "error" })
	c1.CloseNow()
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" && m.Room.HostID == guest.PlayerID })
	a.mu.Lock()
	r := a.rooms[host.Code]
	r.Round.StartAt = time.Now().Add(-time.Second).UnixMilli()
	position := 0
	if r.Round.Track.Year >= r.Timelines[0].Cards[0].Year {
		position = 1
	}
	a.mu.Unlock()
	writeAction(t, c2, action{Type: "place", RoundID: playing.Room.Round.ID, Position: position})
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "reveal" })
	reconnected := connectTest(t, s, host)
	restored := readUntil(t, reconnected, func(m wireMessage) bool { return m.Type == "state" })
	if len(restored.Room.Players) != 3 || restored.Room.Players[0].Team != "green" || len(restored.Room.Timelines[0].Cards) != 2 || len(restored.Room.Timelines[0].PlayerIDs) != 2 {
		t.Fatal("reconnect did not restore shared team state")
	}
	writeAction(t, reconnected, action{Type: "next", RoundID: playing.Room.Round.ID})
	readUntil(t, c3, func(m wireMessage) bool {
		return m.Type == "state" && m.Room.Phase == "playing" && m.Room.TurnID == solo.PlayerID
	})
}
