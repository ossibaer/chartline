package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestChronologicalPlacement(t *testing.T) {
	cards := []card{{Year: 1970}, {Year: 1980}, {Year: 1980}, {Year: 2000}}
	for _, tt := range []struct {
		name           string
		year, position int
		want           bool
	}{
		{"before everything", 1960, 0, true}, {"after everything", 2020, 4, true},
		{"between years", 1990, 3, true}, {"wrong gap", 1990, 1, false},
		{"equal before", 1980, 1, true}, {"equal between", 1980, 2, true}, {"equal after", 1980, 3, true},
		{"negative index", 1980, -1, false}, {"past the end", 1980, 5, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := validPlacement(cards, tt.year, tt.position); got != tt.want {
				t.Fatalf("got %v; want %v", got, tt.want)
			}
		})
	}
}

func TestGameOutcomeAndStaleActions(t *testing.T) {
	a := &app{}
	p := &player{ID: "a", Cards: []card{{Year: 1960}, {Year: 1980}, {Year: 1990}, {Year: 2000}}}
	q := &player{ID: "b", Cards: []card{{Year: 1975}}}
	r := &room{HostID: p.ID, Players: []*player{p, q}, Phase: "playing", Target: 5, Round: &round{ID: "round-one", Track: track{Title: "Secret", Year: 1970}, StartAt: time.Now().Add(-time.Second).UnixMilli()}}
	if err := a.applyAction(r, q, action{Type: "place", RoundID: r.Round.ID, Position: 0}, time.Now()); err == nil {
		t.Fatal("another player was allowed to place")
	}
	if err := a.applyAction(r, p, action{Type: "place", RoundID: "old-round", Position: 1}, time.Now()); err == nil {
		t.Fatal("stale placement accepted")
	}
	if err := a.applyAction(r, p, action{Type: "place", RoundID: r.Round.ID, Position: 1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.Phase != "reveal" || !r.Round.Result.Correct || len(p.Cards) != 5 || len(r.Winners) != 1 {
		t.Fatalf("bad winning reveal: %+v", r)
	}
	if !sort.SliceIsSorted(p.Cards, func(i, j int) bool { return p.Cards[i].Year < p.Cards[j].Year }) {
		t.Fatal("timeline was not sorted")
	}
	if err := a.applyAction(r, p, action{Type: "place", RoundID: r.Round.ID, Position: 1}, time.Now()); err == nil {
		t.Fatal("duplicate placement accepted")
	}
	if err := a.applyAction(r, p, action{Type: "next", RoundID: r.Round.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.Phase != "finished" {
		t.Fatal("winning game did not finish")
	}
	// Wrong guesses do not award cards; deck exhaustion shares tied wins.
	r.Phase = "playing"
	r.Winners = nil
	r.Target = 10
	r.Round.StartAt = time.Now().Add(-time.Second).UnixMilli()
	if err := r.place(p, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.Round.Result.Correct || len(p.Cards) != 5 {
		t.Fatal("incorrect guess awarded a card")
	}
	q.Cards = append([]card{}, p.Cards...)
	r.advance(time.Now())
	if r.Phase != "finished" || len(r.Winners) != 2 {
		t.Fatal("deck exhaustion should share tied wins")
	}
}

func testApp(t *testing.T) (*app, *httptest.Server) {
	t.Helper()
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		t.Fatal(err)
	}
	a, err := newApp(t.TempDir(), assets)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.handler())
	ctx, cancel := context.WithCancel(context.Background())
	go a.runClock(ctx)
	t.Cleanup(func() { cancel(); a.closeConnections(); server.Close() })
	return a, server
}

func apiTest(t *testing.T, server *httptest.Server, path, token string, body any, status int) []byte {
	t.Helper()
	var reader io.Reader
	method := "GET"
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
		method = "POST"
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, status, data)
	}
	return data
}

type entry struct {
	Token    string `json:"token"`
	PlayerID string `json:"playerId"`
	Code     string `json:"code"`
}

func enterTest(t *testing.T, s *httptest.Server, code, name string) entry {
	t.Helper()
	path := "/api/rooms"
	if code != "" {
		path = "/api/join"
	}
	data := apiTest(t, s, path, "", entryRequest{Name: name, Code: code, Library: "demo", Target: 5}, 201)
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	return e
}
func connectTest(t *testing.T, s *httptest.Server, e entry) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	if err := wsjson.Write(ctx, c, action{Type: "auth", Token: e.Token}); err != nil {
		t.Fatal(err)
	}
	return c
}

type wireMessage struct {
	Type    string   `json:"type"`
	Room    roomView `json:"room"`
	Message string   `json:"message"`
}

func readUntil(t *testing.T, c *websocket.Conn, predicate func(wireMessage) bool) wireMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var m wireMessage
		if err := wsjson.Read(ctx, c, &m); err != nil {
			t.Fatal(err)
		}
		if predicate(m) {
			return m
		}
	}
}
func writeAction(t *testing.T, c *websocket.Conn, m action) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, m); err != nil {
		t.Fatal(err)
	}
}

func TestLiveRoomAudioAndReconnect(t *testing.T) {
	a, s := testApp(t)
	host := enterTest(t, s, "", "Oskar")
	c1 := connectTest(t, s, host)
	readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" })
	guest := enterTest(t, s, host.Code, "Maya")
	c2 := connectTest(t, s, guest)
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" && len(m.Room.Players) == 2 })
	writeAction(t, c2, action{Type: "start"})
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "error" })
	writeAction(t, c1, action{Type: "start"})
	playing := readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "playing" })
	roundID := playing.Room.Round.ID
	a.mu.Lock()
	hidden := a.rooms[host.Code].Round.Track
	a.mu.Unlock()
	encoded, _ := json.Marshal(playing.Room)
	if bytes.Contains(encoded, []byte(hidden.Title)) || bytes.Contains(encoded, []byte(`"file"`)) || bytes.Contains(encoded, []byte(`"deck"`)) {
		t.Fatal("unrevealed answer leaked into state")
	}
	apiTest(t, s, "/api/audio/"+roundID, "", nil, 401)
	wav := apiTest(t, s, "/api/audio/"+roundID, guest.Token, nil, 200)
	if string(wav[:4]) != "RIFF" || len(wav) != 44+22050*8*2 {
		t.Fatal("invalid demo audio")
	}
	apiTest(t, s, "/api/library", host.Token, nil, 403)
	writeAction(t, c1, action{Type: "ready", RoundID: roundID, Generation: 1})
	readyOne := readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && m.Room.Players[0].Ready })
	if readyOne.Room.Round.StartAt != 0 {
		t.Fatal("playback started before both clients were ready")
	}
	writeAction(t, c2, action{Type: "ready", RoundID: roundID, Generation: 1})
	scheduled := readUntil(t, c1, func(m wireMessage) bool { return m.Type == "state" && m.Room.Round.StartAt > 0 })
	if scheduled.Room.Round.StartAt < time.Now().UnixMilli() {
		t.Fatal("start was not scheduled in the future")
	}
	time.Sleep(time.Until(time.UnixMilli(scheduled.Room.Round.StartAt)) + 10*time.Millisecond)
	position := 0
	for position < len(scheduled.Room.Players[0].Cards) && scheduled.Room.Players[0].Cards[position].Year < hidden.Year {
		position++
	}
	writeAction(t, c1, action{Type: "place", RoundID: roundID, Position: position})
	reveal := readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" && m.Room.Phase == "reveal" })
	if !reveal.Room.Round.Result.Correct || reveal.Room.Round.Result.Card.Title != hidden.Title || len(reveal.Room.Players[0].Cards) != 2 {
		t.Fatal("clients did not see a correct reveal")
	}
	writeAction(t, c1, action{Type: "next", RoundID: roundID})
	next := readUntil(t, c2, func(m wireMessage) bool {
		return m.Type == "state" && m.Room.Phase == "playing" && m.Room.TurnID == guest.PlayerID
	})
	if next.Room.Round.ID == roundID {
		t.Fatal("round ID was reused")
	}
	apiTest(t, s, "/api/audio/"+roundID, guest.Token, nil, 404)
	c1.CloseNow()
	readUntil(t, c2, func(m wireMessage) bool { return m.Type == "state" && m.Room.HostID == guest.PlayerID })
	reconnected := connectTest(t, s, host)
	restored := readUntil(t, reconnected, func(m wireMessage) bool { return m.Type == "state" })
	if len(restored.Room.Players) != 2 || len(restored.Room.Players[0].Cards) != 2 || restored.Room.HostID != guest.PlayerID {
		t.Fatal("reconnect did not preserve player state and host transfer")
	}
}

func TestPrivateAccessCapacityAndAssets(t *testing.T) {
	a, s := testApp(t)
	a.accessKey = "friends-only"
	apiTest(t, s, "/api/rooms", "", entryRequest{Name: "Stranger"}, 403)
	data := apiTest(t, s, "/api/rooms", "", entryRequest{Name: "Host", AccessKey: "friends-only"}, 201)
	var host entry
	_ = json.Unmarshal(data, &host)
	apiTest(t, s, "/api/join", "", entryRequest{Name: "Stranger", Code: host.Code}, 403)
	for i := 1; i < 10; i++ {
		apiTest(t, s, "/api/join", "", entryRequest{Name: fmt.Sprintf("Friend %d", i), Code: host.Code, AccessKey: "friends-only"}, 201)
	}
	apiTest(t, s, "/api/join", "", entryRequest{Name: "Eleventh", Code: host.Code, AccessKey: "friends-only"}, 409)
	apiTest(t, s, "/.proxy/api/config", "", nil, 200)
	for _, path := range []string{"/data/library.json", "/.env", "/server.go"} {
		apiTest(t, s, path, "", nil, 404)
	}
	res, err := s.Client().Get(s.URL + "/vendor/discord-sdk.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/javascript") {
		t.Fatal("SDK was not served as JavaScript")
	}
}

func uploadTest(t *testing.T, s *httptest.Server, token string, audio []byte, status int) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"title": "Private title", "artist": "Private artist", "year": "1998", "start": "0"} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", "Artist - Secret.mp3")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(audio)
	_ = writer.Close()
	req, _ := http.NewRequest("POST", s.URL+"/api/library", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("upload got %d want %d: %s", res.StatusCode, status, data)
	}
}

func TestMP3PersistenceAndValidation(t *testing.T) {
	a, s := testApp(t)
	host := enterTest(t, s, "", "Host")
	guest := enterTest(t, s, host.Code, "Guest")
	uploadTest(t, s, guest.Token, []byte("bad"), 403)
	uploadTest(t, s, host.Token, []byte("not an MP3"), 400)
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0xc4})
	// An ID3 tag containing an answer must not remain in the stored audio.
	tag := append([]byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 6}, []byte("Secret")...)
	uploadTest(t, s, host.Token, append(tag, bytes.Repeat(frame, 80)...), 201)
	loaded, err := loadLibrary(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Year != 1998 || strings.Contains(loaded[0].File, "Secret") {
		t.Fatalf("unexpected library: %+v", loaded)
	}
	cleaned, err := cleanMP3(append(tag, frame...))
	if err != nil || bytes.Contains(cleaned, []byte("Secret")) {
		t.Fatal("ID3 removal failed")
	}
	if _, err := cleanMP3([]byte{'I', 'D', '3', 3, 0, 0, 127, 127, 127, 127}); err == nil {
		t.Fatal("malformed ID3 accepted")
	}
	if err := saveLibrary(a.dataDir, loaded); err != nil {
		t.Fatal("replacing the library failed:", err)
	}
}

func TestDiscordTicketsAndIsolation(t *testing.T) {
	a, s := testApp(t)
	apiTest(t, s, "/api/discord/token", "", map[string]string{"code": "missing"}, 503)
	apiTest(t, s, "/api/discord/join", "", map[string]string{"ticket": "forged", "instanceId": "instance-one"}, 401)
	login := func(user, instance string) entry {
		t.Helper()
		ticket := newID()
		a.mu.Lock()
		a.tickets[ticket] = discordTicket{UserID: user, Name: user, Expires: time.Now().Add(time.Minute)}
		a.mu.Unlock()
		data := apiTest(t, s, "/api/discord/join", "", map[string]string{"ticket": ticket, "instanceId": instance}, 200)
		apiTest(t, s, "/api/discord/join", "", map[string]string{"ticket": ticket, "instanceId": instance}, 401)
		var result entry
		_ = json.Unmarshal(data, &result)
		return result
	}
	first := login("Alice", "instance-one")
	second := login("Bob", "instance-one")
	third := login("Alice", "instance-two")
	if first.Code != second.Code || first.Code == third.Code {
		t.Fatal("Discord instance routing was incorrect")
	}
	again := login("Alice", "instance-one")
	if again.PlayerID != first.PlayerID {
		t.Fatal("Discord reconnect created a duplicate player")
	}
	apiTest(t, s, "/api/library", first.Token, nil, 401)
}
