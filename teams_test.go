package main

import (
	"fmt"
	"reflect"
	"strings"
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
	a := &app{library: testTracks()}
	blue1 := &player{ID: "blue1", Team: "blue"}
	red := &player{ID: "red1", Team: "red", Client: &client{}}
	blue2 := &player{ID: "blue2", Team: "blue", Client: &client{}}
	solo := &player{ID: "solo", Client: &client{}}
	offlineSolo := &player{ID: "offline-solo"}
	green := &player{ID: "green1", Team: "green"}
	r := &room{HostID: solo.ID, Phase: "lobby", Target: 10, Players: []*player{blue1, red, blue2, solo, offlineSolo, green}}
	if err := r.start(a.library, now); err != nil {
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
	if err := a.applyAction(r, blue2, action{Type: "next", RoundID: r.Round.ID}, now); err == nil {
		t.Fatal("previous team started the next team's song")
	}
	if err := a.applyAction(r, solo, action{Type: "next", RoundID: r.Round.ID}, now); err == nil {
		t.Fatal("host started another team's song")
	}
	if r.Phase != "reveal" || r.view(0).NextTurnID != "team-red" || r.schedule(now.Add(time.Minute)) {
		t.Fatal("reveal did not wait for the next team")
	}
	if err := a.applyAction(r, red, action{Type: "next", RoundID: r.Round.ID}, now); err != nil {
		t.Fatal("next team could not start its song:", err)
	}
	if r.currentTimeline().ID != "team-red" {
		t.Fatal("red team did not get the next turn")
	}
	for _, want := range []string{solo.ID, "team-blue"} {
		if err := a.applyAction(r, solo, action{Type: "discard", RoundID: r.Round.ID}, now); err != nil {
			t.Fatal(err)
		}
		if r.view(0).NextTurnID != want {
			t.Fatalf("next team = %s, want %s", r.view(0).NextTurnID, want)
		}
		nextPlayer := solo
		if want == "team-blue" {
			nextPlayer = blue2
		}
		if err := a.applyAction(r, nextPlayer, action{Type: "next", RoundID: r.Round.ID}, now); err != nil {
			t.Fatal(err)
		}
		if got := r.currentTimeline().ID; got != want {
			t.Fatalf("next turn = %s, want %s (one turn per timeline, skipping fully offline timelines)", got, want)
		}
	}
}

func TestTeamWinsTiesAndReset(t *testing.T) {
	now := time.Now()
	a := &app{library: testTracks(), sessions: map[string]session{"offline-token": {Room: "room", Player: "offline"}}}
	p := &player{ID: "a", Team: "yellow", Client: &client{}}
	q := &player{ID: "b", Team: "yellow", Client: &client{}}
	solo := &player{ID: "solo", Client: &client{}}
	offline := &player{ID: "offline", Team: "yellow"}
	r := &room{HostID: p.ID, Phase: "lobby", Target: 2, Players: []*player{p, q, solo, offline}}
	if err := r.start(a.library, now); err != nil {
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
	v := r.view(len(a.library))
	if len(r.Players) != 3 || len(a.sessions) != 0 || len(r.Timelines) != 0 || v.TurnID != "" || r.Round != nil || len(r.Winners) != 0 {
		t.Fatal("reset did not clear game state and remove offline players")
	}
	if p.Team != "yellow" || q.Team != "yellow" || len(v.Timelines) != 2 || len(v.Timelines[0].Cards) != 0 {
		t.Fatal("reset should preserve colors and empty shared cards")
	}
	if err := a.applyAction(r, q, action{Type: "team", Team: ""}, now); err != nil {
		t.Fatal(err)
	}
	if err := r.start(a.library, now); err != nil {
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
	positionDraft, artistDraft, titleDraft := 1, "Team artist", "Team title"
	writeAction(t, c1, action{Type: "draft", RoundID: playing.Room.Round.ID, DraftID: "draft-one", Draft: &guessDraft{Position: &positionDraft, Artist: &artistDraft}})
	shared := readUntil(t, c2, func(m wireMessage) bool {
		return m.Type == "state" && m.Room.Draft != nil && m.Room.Draft.Artist != nil
	})
	if *shared.Room.Draft.Position != positionDraft || *shared.Room.Draft.Artist != artistDraft {
		t.Fatal("teammate did not receive live draft")
	}
	writeAction(t, c2, action{Type: "draft", RoundID: playing.Room.Round.ID, DraftID: "draft-two", Draft: &guessDraft{Title: &titleDraft}})
	shared = readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && m.Room.Draft != nil && m.Room.Draft.Title != nil })
	if *shared.Room.Draft.Title != titleDraft || *shared.Room.Draft.Artist != artistDraft {
		t.Fatal("independent teammate edits were lost")
	}
	private := readUntil(t, c3, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "playing" })
	if private.Room.Draft != nil {
		t.Fatal("opponent received team draft")
	}
	c2.CloseNow()
	c2 = connectTest(t, s, guest)
	shared = readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" })
	if shared.Room.Draft == nil || shared.Room.Draft.Title == nil || *shared.Room.Draft.Title != titleDraft || *shared.Room.Draft.Position != positionDraft {
		t.Fatal("reconnect did not restore the shared draft")
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
	writeAction(t, c3, action{Type: "next", RoundID: playing.Room.Round.ID})
	readUntil(t, c3, func(m wireMessage) bool {
		return m.Type == "state" && m.Room.Phase == "playing" && m.Room.TurnID == solo.PlayerID
	})
}

func TestTeamDraftUpdates(t *testing.T) {
	now := time.Now()
	a := &app{}
	first := &player{ID: "first", Team: "blue", Client: &client{}}
	teammate := &player{ID: "teammate", Team: "blue", Client: &client{}}
	opponent := &player{ID: "opponent", Client: &client{}}
	r := &room{Phase: "playing", Players: []*player{first, teammate, opponent}, Target: 10,
		Round: &round{ID: "song", Track: track{Year: 1990}, StartAt: now.Add(-time.Second).UnixMilli()}}
	r.Timelines = r.lobbyTimelines()
	r.Timelines[0].Cards = []card{{Year: 1980}}
	position, artist, title := 1, "An artist", "A title"
	update := action{Type: "draft", RoundID: "song", DraftID: "first-edit", Draft: &guessDraft{Position: &position, Artist: &artist}}
	if err := a.applyAction(r, first, update, now); err != nil {
		t.Fatal(err)
	}
	if err := a.applyAction(r, teammate, action{Type: "draft", RoundID: "song", DraftID: "second-edit", Draft: &guessDraft{Title: &title}}, now); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*player{first, teammate} {
		v := r.viewFor(0, p)
		if v.Draft == nil || *v.Draft.Position != position || *v.Draft.Artist != artist || *v.Draft.Title != title {
			t.Fatal("teammates did not share field updates")
		}
	}
	if r.viewFor(0, opponent).Draft != nil || r.view(0).Draft != nil {
		t.Fatal("draft leaked to opponents or public view")
	}
	if err := a.applyAction(r, opponent, update, now); err == nil {
		t.Fatal("opponent edited team draft")
	}
	update.RoundID = "stale"
	if err := a.applyAction(r, first, update, now); err == nil {
		t.Fatal("stale draft accepted")
	}
	update.RoundID = "song"
	invalid := 2
	update.Draft = &guessDraft{Position: &invalid}
	if err := a.applyAction(r, first, update, now); err == nil {
		t.Fatal("invalid gap accepted")
	}
	long := strings.Repeat("x", 101)
	update.Draft = &guessDraft{Artist: &long}
	if err := a.applyAction(r, first, update, now); err == nil {
		t.Fatal("oversized draft accepted")
	}
	empty := ""
	update.Draft = &guessDraft{Artist: &empty}
	if err := a.applyAction(r, teammate, update, now); err != nil {
		t.Fatal(err)
	}
	if *r.Round.Draft.Artist != "" || *r.Round.Draft.Title != title {
		t.Fatal("clearing artist overwrote title")
	}
	if err := r.place(teammate, position, now); err != nil {
		t.Fatal(err)
	}
	if err := a.applyAction(r, first, update, now); err == nil {
		t.Fatal("draft changed after lock-in")
	}
	r.Deck = []track{{Year: 2000}}
	r.nextRound(now)
	if v := r.viewFor(0, first); v.Draft.Position != nil || v.Draft.Artist != nil || v.Draft.Title != nil {
		t.Fatal("new song retained old draft")
	}
}
