# Chartline

A private music timeline game for 1–10 players. Go serves the game, WebSocket rooms and local MP3 library; the frontend uses browser JavaScript with no Node build step.

## Run

Requires Go 1.26.5 or newer:

```sh
go run .
```

Open **http://localhost:8080**. Create a room and choose the demo to try it immediately. Open another browser tab and join using the room code to test multiplayer. Each tab has its own player session.

For real songs, open **Manage MP3s** in the lobby, upload songs with their artist, release year and optional clip start, then choose **Your MP3s** as the song library. The game plays up to 20 seconds per song. You need at least one starting card per player plus one song to guess. A larger collection makes a longer game possible.

The 24 built-in demo tracks are original synthesized practice loops with **fictional years**, intended to demonstrate the rules rather than test music knowledge.

## Playing

- Everyone starts with one revealed song card. On your turn, listen and click a gap in your timeline, then **Lock it in**.
- Correct guesses add the card; incorrect guesses do not. Either side of a song from the same year counts.
- First to 5, 7 or 10 cards wins. If the deck runs out, the highest card count wins, with shared wins for ties.
- The current player or host can replay for everyone and advance after a reveal. The host can skip broken clips or disconnected players’ turns.
- Reconnecting preserves your cards. Hosting transfers to an online player when the host disconnects.
- Click **Enable sound** after reloading the page. Everyone plays the audio locally; the server coordinates readiness and a shared start time.

## Private hosting

Copy `.env.example` to `.env`. Variables already set in the environment take precedence.

| Variable | Purpose |
| --- | --- |
| `ADDR` | Defaults to `127.0.0.1:8080`. Use `0.0.0.0:8080` to listen on your LAN. |
| `DATA_DIR` | Defaults to `data`. Contains `library.json` and `audio/`. |
| `ACCESS_KEY` | Shared password for browser room creation and joining. Set it before exposing the server. |
| `PUBLIC_ORIGIN` | Your external HTTPS origin, if your reverse proxy changes the request host. |

Use HTTPS through a reverse proxy or tunnel for remote friends. Forward both HTTP and WebSockets to the Go server. On another computer, `localhost` points to that computer, so use the host computer’s address or your HTTPS URL instead. For LAN HTTP, modern browser audio/security features may vary; HTTPS is the supported remote setup.

Uploaded songs survive restarts. Rooms and player sessions are in memory and reset on restart; idle rooms expire after 12 hours. Back up the data directory to preserve your library. The application serves only embedded web assets and authenticated current-round audio, not the data directory itself. Common ID3 tags are removed from uploaded files. Use audio you have permission to use.

## Discord Activity

The same frontend includes Discord initialization, OAuth login, a server-side user allowlist, and automatic room selection by Activity instance. No voice bot is needed. The SDK is bundled locally, so clients need no external JavaScript CDN.

1. Create an application in the [Discord Developer Portal](https://discord.com/developers/applications) and enable Activities. Enable your desired supported platforms; start with desktop/web.
2. Host Chartline at an HTTPS URL. Configure the Activity URL mapping `/` to your host name, without `https://`. The same mapping serves assets, API calls, audio and `/ws`.
3. In OAuth2 settings, register a redirect URI as required by Discord’s setup flow. The Embedded App SDK performs authorization inside Discord and the backend exchanges the resulting code.
4. Set `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, and `DISCORD_ALLOWED_USER_IDS` (comma-separated IDs for you and your friends) in `.env`. Keep the secret on the server. Restart Chartline.
5. Invite friends as **App Testers**, have them accept their invitations, enable the required Discord development/test settings, and launch the Activity in your small server. Everyone joining that Activity instance enters the same room.
6. Check that two players can join, enable sound and hear a demo clip before importing the full library.

Discord login grants access only to the configured user allowlist. The Activity instance ID routes players to a room; it is not used as an authorization credential. Browser entry additionally uses `ACCESS_KEY` when configured.

The Discord integration is implemented but requires your application credentials and an actual Discord client for live validation. Mobile audio and background/resume behavior still need testing on your devices.

References: [first Activity](https://docs.discord.com/developers/activities/building-an-activity), [networking](https://docs.discord.com/developers/activities/development-guides/networking), [inviting testers](https://support-dev.discord.com/hc/en-us/articles/21204493235991-How-Can-My-Friends-Play-My-Activity).

## Development

```sh
go test ./...
go vet ./...
go build .
```

Web assets are embedded at compilation: restart `go run .` after editing frontend files. `game.go` owns rules and public snapshots; `server.go` owns rooms, WebSockets and media access; `library.go` owns MP3 storage and demo synthesis; `discord.go` and `web/platform.js` isolate Discord integration; `web/audio.js` coordinates local audio playback.

Tests cover placements and same-year ties, turn authorization, stale actions, wins, deck exhaustion, two-client synchronization, answer hiding, audio access, host transfer, reconnects, room limits, MP3 validation/persistence and Discord ticket/instance handling.

Current draft limits: no mid-game joins, no persistent game history, no library deletion UI, and no song recognition or release-year lookup. Players can inspect audio or use recognition tools; this is a game for trusted friends.
