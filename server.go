package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type session struct {
	Room   string
	Player string
}
type client struct {
	conn   *websocket.Conn
	send   chan []byte
	cancel context.CancelFunc
}

type app struct {
	mu           sync.Mutex
	rooms        map[string]*room
	sessions     map[string]session
	tickets      map[string]discordTicket
	instances    map[string]string
	library      []track
	demos        []track
	dataDir      string
	assets       fs.FS
	accessKey    string
	publicOrigin string
	discord      discordConfig
}

func newApp(dir string, assets fs.FS) (*app, error) {
	library, err := loadLibrary(dir)
	if err != nil {
		return nil, err
	}
	return &app{rooms: map[string]*room{}, sessions: map[string]session{}, tickets: map[string]discordTicket{}, instances: map[string]string{}, library: library, demos: demoLibrary(), dataDir: dir, assets: assets}, nil
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", a.config)
	mux.HandleFunc("POST /api/rooms", a.createRoom)
	mux.HandleFunc("POST /api/join", a.joinRoom)
	mux.HandleFunc("POST /api/leave", a.leaveRoom)
	mux.HandleFunc("GET /api/library", a.listLibrary)
	mux.HandleFunc("POST /api/library", a.uploadTrack)
	mux.HandleFunc("GET /api/audio/{round}", a.audio)
	mux.HandleFunc("POST /api/discord/token", a.discordToken)
	mux.HandleFunc("POST /api/discord/join", a.discordJoin)
	mux.HandleFunc("GET /ws", a.socket)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "That endpoint does not exist.") })
	files := http.FileServer(http.FS(a.assets))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fail(w, http.StatusMethodNotAllowed, "Use GET to load the game.")
			return
		}
		if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors https://discord.com https://*.discord.com https://*.discordapp.com https://*.discordsays.com")
		// Discord's /.proxy prefix may be preserved by a URL mapping.
		if strings.HasPrefix(r.URL.Path, "/.proxy/") {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/.proxy")
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		fail(w, 400, "The request could not be read.")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "Send one JSON object.")
		return false
	}
	return true
}
func cleanName(name string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
}
func (a *app) validKey(key string) bool {
	return a.accessKey == "" || subtle.ConstantTimeCompare([]byte(key), []byte(a.accessKey)) == 1
}
func (a *app) tracks(r *room) []track {
	if r.Library == "custom" {
		return a.library
	}
	return a.demos
}

func (a *app) config(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	count := len(a.library)
	a.mu.Unlock()
	writeJSON(w, 200, map[string]any{"maxPlayers": maxPlayers, "customTrackCount": count, "demoTrackCount": len(a.demos), "accessKeyRequired": a.accessKey != "", "discordClientId": a.discord.ClientID, "discordEnabled": a.discord.ClientID != "" && a.discord.Secret != ""})
}

type entryRequest struct {
	Name      string `json:"name"`
	Code      string `json:"code"`
	Library   string `json:"library"`
	Target    int    `json:"target"`
	AccessKey string `json:"accessKey"`
}

func (a *app) enter(w http.ResponseWriter, r *http.Request, create bool) {
	var input entryRequest
	if !readJSON(w, r, &input) {
		return
	}
	if !a.validKey(input.AccessKey) {
		fail(w, 403, "The shared password is incorrect.")
		return
	}
	input.Name = cleanName(input.Name)
	if len([]rune(input.Name)) < 1 || len([]rune(input.Name)) > 24 {
		fail(w, 400, "Choose a name between 1 and 24 characters.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var game *room
	if create {
		if len(a.rooms) >= 100 {
			fail(w, 503, "The server is full. Try again later.")
			return
		}
		if input.Library != "custom" {
			input.Library = "demo"
		}
		if input.Target != 5 && input.Target != 7 && input.Target != 10 {
			input.Target = 5
		}
		code := roomCode()
		for a.rooms[code] != nil {
			code = roomCode()
		}
		game = &room{Code: code, Phase: "lobby", Library: input.Library, Target: input.Target, Updated: time.Now()}
		a.rooms[code] = game
	} else {
		game = a.rooms[strings.ToUpper(strings.TrimSpace(input.Code))]
		if game == nil {
			fail(w, 404, "That room was not found. Check the six-character code.")
			return
		}
		if game.Phase != "lobby" {
			fail(w, 409, "This game is in progress. Join when the host returns to the lobby.")
			return
		}
		if len(game.Players) >= maxPlayers {
			fail(w, 409, "This room already has 10 players.")
			return
		}
	}
	p := &player{ID: newID(), Name: input.Name, Cards: []card{}}
	game.Players = append(game.Players, p)
	if game.HostID == "" {
		game.HostID = p.ID
	}
	token := newID() + newID()
	a.sessions[token] = session{game.Code, p.ID}
	a.broadcast(game)
	writeJSON(w, 201, map[string]any{"token": token, "playerId": p.ID, "code": game.Code})
}
func (a *app) createRoom(w http.ResponseWriter, r *http.Request) { a.enter(w, r, true) }
func (a *app) joinRoom(w http.ResponseWriter, r *http.Request)   { a.enter(w, r, false) }

// Caller holds a.mu. Credentials never appear in media URLs or room snapshots.
func (a *app) authenticate(token string) (*room, *player) {
	s, ok := a.sessions[token]
	if !ok {
		return nil, nil
	}
	r := a.rooms[s.Room]
	if r == nil {
		return nil, nil
	}
	p := r.findPlayer(s.Player)
	if p == nil {
		return nil, nil
	}
	return r, p
}
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (a *app) leaveRoom(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	game, p := a.authenticate(bearer(r))
	if p == nil {
		fail(w, 401, "Your session has expired.")
		return
	}
	if p.Client != nil {
		p.Client.cancel()
		p.Client = nil
	}
	for token, s := range a.sessions {
		if s.Player == p.ID {
			delete(a.sessions, token)
		}
	}
	if game.Phase == "lobby" {
		for i, member := range game.Players {
			if member.ID == p.ID {
				game.Players = append(game.Players[:i], game.Players[i+1:]...)
				break
			}
		}
		game.Turn = 0
	}
	a.reassignHost(game)
	a.broadcast(game)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *app) reassignHost(r *room) {
	if p := r.findPlayer(r.HostID); p != nil && p.Client != nil {
		return
	}
	for _, p := range r.Players {
		if p.Client != nil {
			r.HostID = p.ID
			return
		}
	}
	if len(r.Players) > 0 {
		r.HostID = r.Players[0].ID
	}
}

func (a *app) broadcast(r *room) {
	r.Updated = time.Now()
	message, _ := json.Marshal(map[string]any{"type": "state", "room": r.view(len(a.tracks(r))), "serverTime": time.Now().UnixMilli()})
	for _, p := range r.Players {
		if p.Client != nil {
			p.Client.enqueue(message)
		}
	}
}
func (c *client) enqueue(message []byte) {
	select {
	case c.send <- message:
	default:
		c.cancel()
	}
}
func (c *client) message(value any) { data, _ := json.Marshal(value); c.enqueue(data) }

type action struct {
	Type       string `json:"type"`
	Token      string `json:"token,omitempty"`
	RoundID    string `json:"roundId,omitempty"`
	Position   int    `json:"position"`
	Generation int    `json:"generation,omitempty"`
	Library    string `json:"library,omitempty"`
	Target     int    `json:"target,omitempty"`
	ClientTime int64  `json:"clientTime,omitempty"`
}

func (a *app) socket(w http.ResponseWriter, r *http.Request) {
	patterns := []string{}
	if a.discord.ClientID != "" {
		patterns = append(patterns, a.discord.ClientID+".discordsays.com")
	}
	if u, err := url.Parse(a.publicOrigin); err == nil && u.Host != "" {
		patterns = append(patterns, u.Host)
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: patterns})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4096)
	authCtx, cancelAuth := context.WithTimeout(context.Background(), 5*time.Second)
	var auth action
	err = wsjson.Read(authCtx, conn, &auth)
	cancelAuth()
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &client{conn: conn, send: make(chan []byte, 32), cancel: cancel}
	a.mu.Lock()
	game, p := a.authenticate(auth.Token)
	if auth.Type != "auth" || p == nil {
		a.mu.Unlock()
		_ = conn.Close(websocket.StatusPolicyViolation, "Session expired")
		return
	}
	if p.Client != nil {
		p.Client.cancel()
	}
	p.Client = c
	a.reassignHost(game)
	a.broadcast(game)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if p.Client == c {
			p.Client = nil
			a.reassignHost(game)
			a.broadcast(game)
		}
	}()
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-c.send:
				writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, data)
				stop()
				if err != nil {
					return
				}
			}
		}
	}()
	window := time.Now()
	count := 0
	for {
		readCtx, stop := context.WithTimeout(ctx, 45*time.Second)
		var message action
		err := wsjson.Read(readCtx, conn, &message)
		stop()
		if err != nil {
			return
		}
		if time.Since(window) > time.Second {
			window = time.Now()
			count = 0
		}
		count++
		if count > 30 {
			_ = conn.Close(websocket.StatusPolicyViolation, "Too many messages")
			return
		}
		a.mu.Lock()
		if p.Client != c {
			a.mu.Unlock()
			return
		}
		if message.Type == "ping" {
			game.Updated = time.Now()
			c.message(map[string]any{"type": "pong", "clientTime": message.ClientTime, "serverTime": time.Now().UnixMilli()})
			a.mu.Unlock()
			continue
		}
		err = a.applyAction(game, p, message, time.Now())
		if err != nil {
			c.message(map[string]string{"type": "error", "message": err.Error()})
		} else {
			a.broadcast(game)
		}
		a.mu.Unlock()
	}
}

func (a *app) applyAction(r *room, p *player, m action, now time.Time) error {
	host := p.ID == r.HostID
	if m.Type == "ready" || m.Type == "place" || m.Type == "replay" || m.Type == "skip" || m.Type == "next" {
		if r.Round == nil || r.Round.ID != m.RoundID {
			return errors.New("The round changed. Try again.")
		}
	}
	switch m.Type {
	case "settings":
		if !host || r.Phase != "lobby" {
			return errors.New("Only the host can change lobby settings.")
		}
		if m.Library != "demo" && m.Library != "custom" {
			return errors.New("Choose a song library.")
		}
		if m.Target != 5 && m.Target != 7 && m.Target != 10 {
			return errors.New("Choose 5, 7 or 10 cards.")
		}
		r.Library = m.Library
		r.Target = m.Target
	case "start":
		if !host {
			return errors.New("The host starts the game.")
		}
		return r.start(a.tracks(r), now)
	case "ready":
		if r.Phase != "playing" || m.Generation != r.Round.Generation {
			return nil
		}
		p.ReadyGeneration = m.Generation
		r.schedule(now)
	case "place":
		return r.place(p, m.Position, now)
	case "replay":
		if r.Phase != "playing" || (!host && p.ID != r.Players[r.Turn].ID) {
			return errors.New("Only the host or current player can replay.")
		}
		if r.Round.StartAt == 0 {
			return errors.New("The clip is already loading.")
		}
		r.prepare(now)
	case "skip":
		if !host || r.Phase != "playing" {
			return errors.New("Only the host can skip this song.")
		}
		r.Round.Result = &resultView{Card: r.Round.Track.card(), Skipped: true, PlayerID: r.Players[r.Turn].ID}
		r.Phase = "reveal"
	case "next":
		if r.Phase != "reveal" || (!host && p.ID != r.Players[r.Turn].ID) {
			return errors.New("Wait for the host or current player to continue.")
		}
		r.advance(now)
	case "reset":
		if !host || r.Phase != "finished" {
			return errors.New("Return to the lobby when the game is finished.")
		}
		players := []*player{}
		for _, member := range r.Players {
			if member.Client != nil {
				member.Cards = []card{}
				players = append(players, member)
			} else {
				for token, s := range a.sessions {
					if s.Player == member.ID {
						delete(a.sessions, token)
					}
				}
			}
		}
		r.Players = players
		r.Phase = "lobby"
		r.Round = nil
		r.Deck = nil
		r.Turn = 0
		r.Winners = nil
		r.FinishReason = ""
	default:
		return errors.New("Unknown game action.")
	}
	return nil
}

func (a *app) runClock(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.mu.Lock()
			for code, r := range a.rooms {
				if r.schedule(now) {
					a.broadcast(r)
				}
				if now.Sub(r.Updated) > 12*time.Hour {
					for _, p := range r.Players {
						if p.Client != nil {
							p.Client.cancel()
						}
					}
					delete(a.rooms, code)
					for token, s := range a.sessions {
						if s.Room == code {
							delete(a.sessions, token)
						}
					}
					for instance, roomCode := range a.instances {
						if roomCode == code {
							delete(a.instances, instance)
						}
					}
				}
			}
			for id, ticket := range a.tickets {
				if now.After(ticket.Expires) {
					delete(a.tickets, id)
				}
			}
			a.mu.Unlock()
		}
	}
}
func (a *app) closeConnections() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.rooms {
		for _, p := range r.Players {
			if p.Client != nil {
				p.Client.cancel()
			}
		}
	}
}

func (a *app) listLibrary(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	game, p := a.authenticate(bearer(r))
	if p == nil {
		fail(w, 401, "Your session has expired.")
		return
	}
	if game.Phase != "lobby" || game.HostID != p.ID {
		fail(w, 403, "The host can manage songs in the lobby.")
		return
	}
	type libraryItem struct {
		Title  string  `json:"title"`
		Artist string  `json:"artist"`
		Year   int     `json:"year"`
		Start  float64 `json:"start"`
	}
	items := []libraryItem{}
	for _, t := range a.library {
		items = append(items, libraryItem{t.Title, t.Artist, t.Year, t.Start})
	}
	writeJSON(w, 200, items)
}

func (a *app) uploadTrack(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	game, p := a.authenticate(bearer(r))
	allowed := p != nil && p.ID == game.HostID && game.Phase == "lobby"
	a.mu.Unlock()
	if !allowed {
		fail(w, 403, "The host can upload songs in the lobby.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 33<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail(w, 400, "Choose an MP3 smaller than 32 MB.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	title, artist := cleanName(r.FormValue("title")), cleanName(r.FormValue("artist"))
	year, err := strconv.Atoi(r.FormValue("year"))
	if err != nil || year < 1800 || year > time.Now().Year()+1 {
		fail(w, 400, "Enter a valid release year.")
		return
	}
	if title == "" || artist == "" || len([]rune(title)) > 100 || len([]rune(artist)) > 100 {
		fail(w, 400, "Add a title and artist, up to 100 characters each.")
		return
	}
	offset := 0.0
	if value := r.FormValue("start"); value != "" {
		offset, err = strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(offset) || math.IsInf(offset, 0) || offset < 0 || offset > 3600 {
			fail(w, 400, "Clip start must be between 0 and 3600 seconds.")
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "Choose an MP3 file.")
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".mp3") {
		fail(w, 400, "Choose an MP3 file.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil || len(data) > 32<<20 {
		fail(w, 400, "Choose an MP3 smaller than 32 MB.")
		return
	}
	data, err = cleanMP3(data)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	t := track{ID: newID(), Title: title, Artist: artist, Year: year, Start: offset, Duration: 20}
	t.File = t.ID + ".mp3"
	a.mu.Lock()
	defer a.mu.Unlock()
	game, p = a.authenticate(bearer(r))
	if p == nil || p.ID != game.HostID || game.Phase != "lobby" {
		fail(w, 409, "The lobby changed. Upload the song before starting a game.")
		return
	}
	if len(a.library) >= 500 {
		fail(w, 400, "The library limit is 500 songs.")
		return
	}
	path := filepath.Join(a.dataDir, "audio", t.File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("save audio: %v", err)
		fail(w, 500, "The song could not be saved.")
		return
	}
	updated := append(append([]track{}, a.library...), t)
	if err := saveLibrary(a.dataDir, updated); err != nil {
		_ = os.Remove(path)
		log.Printf("save library: %v", err)
		fail(w, 500, "The library could not be saved.")
		return
	}
	a.library = updated
	for _, room := range a.rooms {
		if room.Phase == "lobby" {
			a.broadcast(room)
		}
	}
	writeJSON(w, 201, map[string]any{"ok": true, "count": len(a.library)})
}

func (a *app) audio(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	game, p := a.authenticate(bearer(r))
	if p == nil {
		a.mu.Unlock()
		fail(w, 401, "Your session has expired.")
		return
	}
	if game.Round == nil || game.Round.ID != r.PathValue("round") || (game.Phase != "playing" && game.Phase != "reveal") {
		a.mu.Unlock()
		fail(w, 404, "This clip is no longer available.")
		return
	}
	t := game.Round.Track
	a.mu.Unlock()
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", `inline; filename="clip"`)
	if t.Demo > 0 {
		w.Header().Set("Content-Type", "audio/wav")
		http.ServeContent(w, r, "clip.wav", time.Time{}, bytes.NewReader(demoAudio(t.Demo)))
		return
	}
	f, err := os.Open(filepath.Join(a.dataDir, "audio", t.File))
	if err != nil {
		fail(w, 404, "The MP3 could not be found. The host can skip this song.")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "audio/mpeg")
	http.ServeContent(w, r, "clip.mp3", time.Time{}, f)
}
