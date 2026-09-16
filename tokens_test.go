package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestGuessSimilarity(t *testing.T) {
	for _, tt := range []struct {
		guess, answer string
		want          bool
	}{
		{"  HELLO\tWORLD\n", "hello world", true},
		{"abcdx", "abcde", true}, // Exactly 80% after one substitution.
		{"abcxx", "abcde", false},
		{"abcd", "abcde", true}, // One deletion.
		{"abcde", "abcd", true}, // One insertion.
		{"abx", "abc", false},
		{"世界音樂錯", "世界音樂好", true}, // Count characters, not UTF-8 bytes.
		{"B J Ö R K", "björk", true},
		{"", "", false}, {" \t", "Artist", false},
		{"abcd!", "abcd?", true}, {"a!", "a?", false},
		{"actual title plus many guesses", "actual title", false},
	} {
		if got := closeGuess(tt.guess, tt.answer); got != tt.want {
			t.Errorf("closeGuess(%q, %q) = %v; want %v", tt.guess, tt.answer, got, tt.want)
		}
	}
}

func TestFeaturedArtistRecognition(t *testing.T) {
	for _, tt := range []struct {
		artist, title, guessArtist, guessTitle string
		want                                   bool
	}{
		{"Lead feat. Guest", "Song Title", "Lead", "songtitle", true},
		{"Lead FT. Guest", "Song Title", "guest", "Song Title", true},
		{"Lead featuring Guest & Another", "Song Title", "Another", "Song Title", true},
		{"Lead (feat. Guest, Another)", "Song Title", "Guest", "Song Title", true},
		{"Lead", "Song Title (feat. Guest)", "Guest", "Song Title", true},
		{"Lead", "Song Title [ft. Guest]", "Lead", "Song Title [ft. Guest]", true},
		{"Earth, Wind & Fire", "September", "Earth, Wind & Fire", "September", true},
		{"Earth, Wind & Fire", "September", "Fire", "September", true},
		{"AC/DC", "Thunderstruck", "DC", "Thunderstruck", false},
		{"Lead feat. Guest", "Song Title", "Stranger", "Song Title", false},
		{"Lead feat. Guest", "Song Title", "Guest", "Wrong Song", false},
	} {
		if got := recognizedSong(track{Artist: tt.artist, Title: tt.title}, tt.guessArtist, tt.guessTitle); got != tt.want {
			t.Errorf("recognition of %q / %q with %q / %q = %v; want %v", tt.artist, tt.title, tt.guessArtist, tt.guessTitle, got, tt.want)
		}
	}
}

func TestArtistSubsets(t *testing.T) {
	for _, tt := range []struct {
		guess string
		want  bool
	}{
		{"Alpha", true}, {"Bravo", true}, {"Charlie", true}, {"Delta", true},
		{"Alphx", true}, {"Alpxx", false},
		{"Alpha & Charlie", true}, {"Charlie; Alpha", true},
		{"Alphx, Bravx", true}, {"Alphx, Braxx", false},
		{"Alpha; Bravo & Charlie, Delta", true},
		{"Alpha; Bravo & Charlxx, Delta", false},
		{"Alpha, Stranger", false}, {"Stranger; Alpha", false},
		{"", false}, {",;&", false},
	} {
		t.Run(tt.guess, func(t *testing.T) {
			track := track{Artist: "Alpha, Bravo & Charlie; Delta", Title: "Song"}
			if got := recognizedSong(track, tt.guess, "Song"); got != tt.want {
				t.Fatalf("artist %q: got %v, want %v", tt.guess, got, tt.want)
			}
		})
	}
	if !recognizedSong(track{Artist: "Alpha, Bravo feat. Charlie & Delta", Title: "Song"}, "Delta; Alpha", "Song") {
		t.Fatal("could not combine main and featured artists")
	}
}

func TestParenthesizedTitles(t *testing.T) {
	for _, tt := range []struct {
		title, guess string
		want         bool
	}{
		{"(Optional Words) Main Title", "Main Title", true},
		{"(Optional Words) Main Title", "Optional Words Main Title", true},
		{"(Optional Words) Main Title", "(Optional Words) Main Title", true},
		{"(Optional Words) Main Title", "Optional Words", false},
		{"Main (Optional Words) Title", "Main Title", true},
		{"Main (Optional Words) Title", "Main Optional Words Title", true},
		{"Main (Optional Words) Title", "Main (Optional Words) Title", true},
		{"Main (Optional Words) Title", "Optional Words", false},
		{"(A very long optional phrase) X", "A very long optional phrase", false},
		{"(A very long optional phrase) X", "A very long optional phrasq", false},
		{"(A very long optional phrase) X", "A very long optional phrasq X", true},
		{"(A very long optional phrase) X", "(A very long optional phrase) X", true},
		{"(Dance) Dance", "Dance", true},
		{"(First optional phrase) X (Second optional phrase) Y", "First optional phrase Second optional phrasx", false},
		{"Main Title (Alternative Title)", "Main Title", true},
		{"Main Title (Alternative Title)", "Alternative Title", true},
		{"Main Title (Alternative Title)", "Main Title (Alternative Title)", true},
		{"Main Title (Alternative Title)", "Main Title Alternative Title", true},
		{"Main Title (Alternative Title)  ", "Alternative Title", true},
		{"(Optional) abcde", "abcdx", true},
		{"(Optional) abcde", "abcxx", false},
		{"ab (Optional) cde", "abcdx", true},
		{"ab (Optional) cde", "abcxx", false},
		{"abcde (fghij)", "abcdx", true},
		{"abcde (fghij)", "abcxx", false},
		{"abcde (fghij)", "fghix", true},
		{"abcde (fghij)", "fghxx", false},
		{"(Intro) Main (Middle) Title (Alternative)", "Main Title", true},
		{"(Intro) Main (Middle) Title (Alternative)", "Intro Main Title", true},
		{"(Intro) Main (Middle) Title (Alternative)", "Main Middle Title", true},
		{"(Intro) Main (Middle) Title (Alternative)", "Alternative", true},
		{"(Intro) Main (Middle) Title (Alternative)", "Middle", false},
		{"(Optional) Main Title", "", false},
		{"Main Title ()", "", false},
		{"Main Title (Alternative Title)", "Unrelated Song", false},
		{"(Optional) Main Title", "(Optional) Main Titlx", true},
	} {
		t.Run(tt.title+"/"+tt.guess, func(t *testing.T) {
			if got := recognizedSong(track{Artist: "Artist", Title: tt.title}, "Artist", tt.guess); got != tt.want {
				t.Fatalf("title %q guessed as %q: got %v, want %v", tt.title, tt.guess, got, tt.want)
			}
		})
	}
	for _, guess := range []string{"Main Title", "Alternative Title", "Main Title (Alternative Title) (feat. Guest)"} {
		if !recognizedSong(track{Artist: "Lead", Title: "Main Title (Alternative Title) (feat. Guest)"}, "Guest", guess) {
			t.Errorf("featured credit interfered with title variant %q", guess)
		}
	}
	if recognizedSong(track{Artist: "Lead", Title: "Main Title (feat. Guest)"}, "Lead", "Guest") {
		t.Fatal("featured artist was treated as an alternative title")
	}
}

func TestTitlePartsSimilarity(t *testing.T) {
	// Compare the merged edit-distance rows with explicitly expanded variants.
	parts := [][]string{{"ab", "cd", ""}, {"e"}, {"fg", "(fg)", ""}}
	for _, guess := range []string{"", "e", "abefg", "cde(fg)", "abexg", "abexx", "zabefg", "(fg)", "cdef", "unrelated"} {
		want := false
		for _, prefix := range parts[0] {
			for _, suffix := range parts[2] {
				want = want || closeGuess(guess, prefix+"e"+suffix)
			}
		}
		if got := closeTitleParts(guess, parts); got != want {
			t.Errorf("variant similarity for %q: got %v, want %v", guess, got, want)
		}
	}
	if !recognizedTitle("Required", strings.Repeat("(optional) ", 30)+"Required") {
		t.Fatal("could not omit many optional phrases")
	}
}

func TestRelaxedRecognitionAwardsToken(t *testing.T) {
	for _, guess := range []string{"Main Title", "Alternative Title", "(Intro) Main Title (Alternative Title)"} {
		t.Run(guess, func(t *testing.T) {
			a, r, p, _, _, now := tokenGame()
			r.Round.Track.Artist = "Alpha, Bravo & Charlie"
			r.Round.Track.Title = "(Intro) Main Title (Alternative Title)"
			if err := a.applyAction(r, p, action{Type: "place", RoundID: r.Round.ID, Position: 0, Artist: "Charlix; Alphx", Title: guess}, now); err != nil {
				t.Fatal(err)
			}
			if !r.Round.Result.TokenEarned || r.Timelines[0].Tokens != 1 || r.Round.Result.Correct {
				t.Fatalf("relaxed matching did not award exactly one token on a misplaced card: %+v", r.Round.Result)
			}
		})
	}
}

func tokenGame() (*app, *room, *player, *player, *player, time.Time) {
	now := time.Now()
	p := &player{ID: "active", Team: "blue", Client: &client{}}
	q := &player{ID: "opponent", Team: "red", Client: &client{}}
	s := &player{ID: "solo", Client: &client{}}
	r := &room{Phase: "playing", HostID: p.ID, Players: []*player{p, q, s}, Target: 5,
		Timelines: []*timeline{
			{ID: "team-blue", Team: "blue", Members: []*player{p}, Cards: []card{{Year: 1960}, {Year: 1980}, {Year: 2000}}},
			{ID: "team-red", Team: "red", Members: []*player{q}, Cards: []card{{Year: 2010}}},
			{ID: s.ID, Members: []*player{s}, Cards: []card{{Year: 2020}}},
		},
		Round: &round{ID: "mystery", Track: track{Title: "Hidden Title", Artist: "Lead feat. Guest", Year: 1990}, StartAt: now.Add(-time.Second).UnixMilli()},
		Deck:  []track{{Title: "Replacement", Year: 1995}, {Title: "Another", Year: 2005}},
	}
	return &app{}, r, p, q, s, now
}

func TestRecognitionTokenIndependentOfPlacement(t *testing.T) {
	for _, tt := range []struct {
		name, artist, title string
		position, tokens    int
	}{
		{"correct placement and recognition", "Guest", "Hidden Title", 2, 1},
		{"misplaced card still earns token", "Guest", "Hidden Title", 0, 1},
		{"both guesses need 80 percent", "Other", "Hidden Title", 2, 0},
		{"artist alone is insufficient", "Guest", "", 2, 0},
		{"title alone is insufficient", "", "Hidden Title", 2, 0},
		{"blank optional guess", "", "", 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, r, p, q, _, now := tokenGame()
			m := action{Type: "place", RoundID: r.Round.ID, Artist: tt.artist, Title: tt.title, Position: tt.position}
			if err := a.applyAction(r, q, m, now); err == nil {
				t.Fatal("opponent submitted artist/title guess")
			}
			if err := a.applyAction(r, p, m, now); err != nil {
				t.Fatal(err)
			}
			if r.Phase != "reveal" || r.Timelines[0].Tokens != tt.tokens || r.Round.Result.TokenEarned != (tt.tokens == 1) {
				t.Fatalf("bad recognition reward: %+v", r.Round.Result)
			}
			wantCards := 3
			if tt.position == 2 {
				wantCards++
			}
			if len(r.Timelines[0].Cards) != wantCards {
				t.Fatal("recognition changed card scoring")
			}
			if err := a.applyAction(r, p, m, now); err == nil || r.Timelines[0].Tokens != tt.tokens {
				t.Fatal("duplicate answer earned another token")
			}
		})
	}
}

func TestTokenSkip(t *testing.T) {
	a, r, p, q, _, now := tokenGame()
	m := action{Type: "skip", RoundID: r.Round.ID}
	r.Timelines[0].Tokens = 1
	if err := a.applyAction(r, p, m, now); err == nil || r.Timelines[0].Tokens != 1 {
		t.Fatal("skip accepted fewer than two tokens")
	}
	r.Timelines[0].Tokens = 4
	if err := a.applyAction(r, q, m, now); err == nil {
		t.Fatal("opponent skipped active song")
	}
	if err := a.applyAction(r, p, m, now); err != nil {
		t.Fatal(err)
	}
	if r.Phase != "playing" || r.Turn != 0 || r.Round.ID == m.RoundID || r.Round.Track.Title != "Replacement" || r.Timelines[0].Tokens != 2 || len(r.Timelines[0].Cards) != 3 || r.Round.StartAt != 0 || r.Round.Generation != 1 {
		t.Fatal("skip did not spend exactly two tokens and replace the song on the same turn")
	}
	if err := a.applyAction(r, p, m, now); err == nil || r.Timelines[0].Tokens != 2 {
		t.Fatal("stale skip spent tokens")
	}
	r.Deck = nil
	if err := a.applyAction(r, p, action{Type: "skip", RoundID: r.Round.ID}, now); err == nil || r.Timelines[0].Tokens != 2 {
		t.Fatal("empty deck skip lost tokens")
	}
}

func TestStealOutcomesAndTokenCosts(t *testing.T) {
	for _, tt := range []struct {
		name                                            string
		year, activePosition, redPosition, soloPosition int
		winner                                          string
	}{
		{"first correct steal wins even at same gap", 1990, 0, 2, 2, "team-red"},
		{"later correct steal beats earlier wrong steal", 1990, 0, 1, 2, "solo"},
		{"all steals wrong", 1990, 0, 1, 3, ""},
		{"playing team wins same year tie", 1980, 1, 2, 2, "team-blue"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, r, p, q, s, now := tokenGame()
			r.Round.Track.Year = tt.year
			r.Timelines[1].Tokens, r.Timelines[2].Tokens = 2, 2
			if err := a.applyAction(r, p, action{Type: "place", RoundID: r.Round.ID, Position: tt.activePosition, Artist: "Guest", Title: "Hidden Title"}, now); err != nil {
				t.Fatal(err)
			}
			if r.Phase != "stealing" || r.Round.Result != nil || len(r.Timelines[0].Cards) != 3 || r.Timelines[0].Tokens != 0 {
				t.Fatal("answer or reward resolved before opponents decided")
			}
			encoded, _ := json.Marshal(r.view(0))
			for _, hidden := range []string{"Hidden Title", "Guest", "tokenEarned", "correct"} {
				if bytes.Contains(encoded, []byte(hidden)) {
					t.Fatalf("steal snapshot leaked %s", hidden)
				}
			}
			if err := a.applyAction(r, q, action{Type: "steal", RoundID: r.Round.ID, Position: tt.redPosition}, now); err != nil {
				t.Fatal(err)
			}
			if r.Timelines[1].Tokens != 1 || r.Phase != "stealing" {
				t.Fatal("token not spent immediately or reveal came early")
			}
			if err := a.applyAction(r, q, action{Type: "steal", RoundID: r.Round.ID, Position: tt.redPosition}, now); err == nil || r.Timelines[1].Tokens != 1 {
				t.Fatal("duplicate steal spent token")
			}
			if err := a.applyAction(r, s, action{Type: "steal", RoundID: r.Round.ID, Position: tt.soloPosition}, now); err != nil {
				t.Fatal(err)
			}
			if r.Phase != "reveal" || r.Round.Result.AwardedTo != tt.winner || r.Timelines[0].Tokens != 1 || r.Timelines[1].Tokens != 1 || r.Timelines[2].Tokens != 1 {
				t.Fatalf("bad steal result: %+v", r.Round.Result)
			}
			for i, timeline := range r.Timelines {
				want := 1
				if i == 0 {
					want = 3
				}
				if timeline.ID == tt.winner {
					want++
				}
				if len(timeline.Cards) != want {
					t.Fatalf("wrong card count for %s", timeline.ID)
				}
				if timeline.ID == tt.winner && i > 0 && timeline.Cards[0].Year != tt.year {
					t.Fatal("stolen song not sorted into winner's own timeline")
				}
			}
		})
	}
}

func TestStealAuthorizationPassAndDeadline(t *testing.T) {
	a, r, p, q, s, now := tokenGame()
	r.Timelines[1].Tokens = 2
	if err := a.applyAction(r, q, action{Type: "steal", RoundID: r.Round.ID, Position: 2}, now); err == nil {
		t.Fatal("steal before lock accepted")
	}
	if err := r.lockGuess(p, 0, strings.Repeat("x", 101), "", now); err == nil || r.Phase != "playing" {
		t.Fatal("oversized guess accepted")
	}
	if err := r.place(p, 0, now); err != nil {
		t.Fatal(err)
	}
	for _, m := range []action{{Type: "skip"}, {Type: "next"}, {Type: "discard"}, {Type: "replay"}, {Type: "place"}, {Type: "steal", Position: 2}} {
		m.RoundID = r.Round.ID
		if err := a.applyAction(r, p, m, now); err == nil {
			t.Fatalf("active player could %s during steal window", m.Type)
		}
	}
	if err := a.applyAction(r, s, action{Type: "steal", RoundID: r.Round.ID, Position: 2}, now); err == nil {
		t.Fatal("zero-token opponent stole")
	}
	for _, position := range []int{-1, 0, 4} {
		if err := a.applyAction(r, q, action{Type: "steal", RoundID: r.Round.ID, Position: position}, now); err == nil || r.Timelines[1].Tokens != 2 {
			t.Fatal("invalid steal accepted or charged")
		}
	}
	if err := a.applyAction(r, q, action{Type: "steal", RoundID: "stale", Position: 2}, now); err == nil {
		t.Fatal("stale steal accepted")
	}
	if err := a.applyAction(r, q, action{Type: "steal", RoundID: r.Round.ID, Position: 2}, r.Round.StealUntil); err == nil || r.Timelines[1].Tokens != 2 {
		t.Fatal("late steal accepted or charged")
	}
	q.Client = nil
	if r.resolveSteals(r.Round.StealUntil.Add(-time.Millisecond)) {
		t.Fatal("window ended early after disconnect")
	}
	if !r.resolveSteals(r.Round.StealUntil) || r.Phase != "reveal" || r.Timelines[1].Tokens != 2 {
		t.Fatal("deadline did not resolve without charging a token")
	}
	if r.resolveSteals(r.Round.StealUntil) {
		t.Fatal("deadline resolved twice")
	}

	a, r, p, q, _, now = tokenGame()
	r.Timelines[1].Tokens = 1
	if err := r.place(p, 2, now); err != nil {
		t.Fatal(err)
	}
	if err := a.applyAction(r, q, action{Type: "pass", RoundID: r.Round.ID}, now); err != nil {
		t.Fatal(err)
	}
	if r.Phase != "reveal" || !r.Round.Result.Correct || r.Timelines[1].Tokens != 1 {
		t.Fatal("passing should resolve early and preserve tokens")
	}
}

func TestStealWinAndReset(t *testing.T) {
	a, r, p, q, _, now := tokenGame()
	r.Target = 2
	r.Timelines[1].Tokens = 1
	if err := r.place(p, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := r.steal(q, 2, false, now); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Winners, []string{"team-red"}) {
		t.Fatal("steal reaching target did not win")
	}
	r.advance(now)
	if r.Phase != "finished" {
		t.Fatal("steal win did not finish")
	}
	if err := a.applyAction(r, p, action{Type: "reset"}, now); err != nil {
		t.Fatal(err)
	}
	for _, timeline := range r.view(0).Timelines {
		if timeline.Tokens != 0 {
			t.Fatal("reset retained token balance")
		}
	}
}

func TestLiveStealAndReconnect(t *testing.T) {
	a, s := testApp(t)
	host := enterTest(t, s, "", "Host")
	c1 := connectTest(t, s, host)
	readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" })
	guest := enterTest(t, s, host.Code, "Opponent")
	c2 := connectTest(t, s, guest)
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" })
	writeAction(t, c1, action{Type: "start"})
	playing := readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "playing" })
	a.mu.Lock()
	r := a.rooms[host.Code]
	r.Timelines[0].Cards = []card{{Year: 2000}}
	r.Timelines[1].Cards = []card{{Year: 1980}}
	r.Timelines[1].Tokens = 1
	r.Round.Track.Title = "Unpublished Answer"
	r.Round.Track.Artist = "Private Artist"
	r.Round.Track.Year = 1990
	r.Round.StartAt = time.Now().Add(-time.Second).UnixMilli()
	a.mu.Unlock()
	writeAction(t, c1, action{Type: "place", RoundID: playing.Room.Round.ID, Position: 1, Artist: "Private Artist", Title: "Unpublished Answer"})
	for _, c := range []*websocket.Conn{c1, c2} {
		locked := readUntil(t, c, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "stealing" })
		if locked.Room.Round.Result != nil || locked.Room.Timelines[0].Tokens != 0 {
			t.Fatal("answer/reward leaked before reveal")
		}
	}
	c2.CloseNow()
	readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && !m.Room.Players[1].Online })
	reconnected := connectTest(t, s, guest)
	locked := readUntil(t, reconnected, func(m wireMessage) bool { return m.Type == "state" })
	if locked.Room.Phase != "stealing" || locked.Room.Timelines[1].Tokens != 1 || *locked.Room.Round.LockedPosition != 1 {
		t.Fatal("reconnect lost pending steal state")
	}
	writeAction(t, reconnected, action{Type: "steal", RoundID: playing.Room.Round.ID, Position: 0})
	for _, c := range []*websocket.Conn{c1, reconnected} {
		result := readUntil(t, c, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "reveal" })
		if result.Room.Round.Result.Correct || result.Room.Round.Result.AwardedTo != guest.PlayerID || !result.Room.Round.Result.TokenEarned || result.Room.Timelines[0].Tokens != 1 || result.Room.Timelines[1].Tokens != 0 || len(result.Room.Timelines[1].Cards) != 2 {
			t.Fatal("clients disagreed on steal and independent token reward")
		}
	}
	writeAction(t, reconnected, action{Type: "next", RoundID: playing.Room.Round.ID})
	next := readUntil(t, reconnected, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "playing" })
	a.mu.Lock()
	r.Round.Track.Year = 1995
	r.Round.StartAt = time.Now().Add(-time.Second).UnixMilli()
	a.mu.Unlock()
	writeAction(t, reconnected, action{Type: "place", RoundID: next.Room.Round.ID, Position: 2})
	readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "stealing" })
	a.mu.Lock()
	r.Round.StealUntil = time.Now().Add(-time.Millisecond)
	a.mu.Unlock()
	expired := readUntil(t, reconnected, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "reveal" })
	if !expired.Room.Round.Result.Correct || expired.Room.Timelines[0].Tokens != 1 || len(expired.Room.Round.Steals) != 0 {
		t.Fatal("server clock did not reveal an unanswered steal window without spending tokens")
	}
}
