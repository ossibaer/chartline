package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type discordConfig struct {
	ClientID string
	Secret   string
	Allowed  []string
}
type discordTicket struct {
	UserID  string
	Name    string
	Expires time.Time
}

var instancePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,160}$`)

func (a *app) discordToken(w http.ResponseWriter, r *http.Request) {
	if a.discord.ClientID == "" || a.discord.Secret == "" {
		fail(w, 503, "Discord is not configured on this server yet.")
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Code == "" {
		fail(w, 400, "Discord did not provide an authorization code.")
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	form := url.Values{"client_id": {a.discord.ClientID}, "client_secret": {a.discord.Secret}, "grant_type": {"authorization_code"}, "code": {input.Code}}
	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://discord.com/api/v10/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		fail(w, 500, "Could not start Discord login.")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		fail(w, 502, "Discord could not be reached. Try again.")
		return
	}
	defer res.Body.Close()
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&tokens) != nil || tokens.AccessToken == "" {
		fail(w, 401, "Discord login expired. Reopen the Activity.")
		return
	}
	req, err = http.NewRequestWithContext(r.Context(), "GET", "https://discord.com/api/v10/users/@me", nil)
	if err != nil {
		fail(w, 500, "Could not check Discord identity.")
		return
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	profile, err := client.Do(req)
	if err != nil {
		fail(w, 502, "Could not check Discord identity.")
		return
	}
	defer profile.Body.Close()
	var user struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
	}
	if profile.StatusCode != 200 || json.NewDecoder(io.LimitReader(profile.Body, 64<<10)).Decode(&user) != nil || user.ID == "" {
		fail(w, 401, "Discord identity could not be verified.")
		return
	}
	allowed := false
	for _, id := range a.discord.Allowed {
		if strings.TrimSpace(id) == user.ID {
			allowed = true
			break
		}
	}
	if !allowed {
		fail(w, 403, "Ask the host to add your Discord user ID to DISCORD_ALLOWED_USER_IDS.")
		return
	}
	name := user.GlobalName
	if name == "" {
		name = user.Username
	}
	ticket := newID() + newID()
	a.mu.Lock()
	if len(a.tickets) > 1000 {
		a.mu.Unlock()
		fail(w, 503, "Too many pending logins. Try again shortly.")
		return
	}
	a.tickets[ticket] = discordTicket{user.ID, cleanName(name), time.Now().Add(5 * time.Minute)}
	a.mu.Unlock()
	writeJSON(w, 200, map[string]string{"access_token": tokens.AccessToken, "ticket": ticket})
}

func (a *app) discordJoin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Ticket     string `json:"ticket"`
		InstanceID string `json:"instanceId"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !instancePattern.MatchString(input.InstanceID) {
		fail(w, 400, "The Discord Activity instance is missing.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, ok := a.tickets[input.Ticket]
	if !ok || time.Now().After(identity.Expires) {
		fail(w, 401, "Discord login expired. Reopen the Activity.")
		return
	}
	delete(a.tickets, input.Ticket)
	// Instance IDs route rooms; authorization is the verified Discord allowlist.
	game := a.rooms[a.instances[input.InstanceID]]
	if game == nil {
		if len(a.rooms) >= 100 {
			fail(w, 503, "The server is full.")
			return
		}
		code := roomCode()
		for a.rooms[code] != nil {
			code = roomCode()
		}
		game = &room{Code: code, Phase: "lobby", Library: "demo", Target: 5, Updated: time.Now()}
		a.rooms[code] = game
		a.instances[input.InstanceID] = code
	}
	var p *player
	for _, member := range game.Players {
		if member.DiscordID == identity.UserID {
			p = member
			break
		}
	}
	if p == nil {
		if game.Phase != "lobby" {
			fail(w, 409, "The game is in progress. Reopen the Activity when the host returns to the lobby.")
			return
		}
		if len(game.Players) >= maxPlayers {
			fail(w, 409, "This room already has 10 players.")
			return
		}
		p = &player{ID: newID(), Name: identity.Name, DiscordID: identity.UserID, Cards: []card{}}
		game.Players = append(game.Players, p)
		if game.HostID == "" {
			game.HostID = p.ID
		}
	}
	for token, s := range a.sessions {
		if s.Player == p.ID {
			delete(a.sessions, token)
		}
	}
	token := newID() + newID()
	a.sessions[token] = session{game.Code, p.ID}
	a.broadcast(game)
	writeJSON(w, 200, map[string]string{"token": token, "playerId": p.ID, "code": game.Code})
}
