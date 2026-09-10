# Chartline

A private music timeline game with no fixed player limit. Play solo or form up to four teams: blue, red, green, and yellow. Go serves the game, WebSocket rooms and local MP3 library; the frontend uses browser JavaScript with no Node build step.

## Run

Requires Go 1.26.5 or newer:

```sh
go run .
```

Put MP3 files in **`data/audio`** before starting the server, then open **http://localhost:8080** and create a room. Open another browser tab and join using the room code to test multiplayer. Each tab has its own player session.

Every room uses the songs in `data/audio` (or `audio/` inside `DATA_DIR`). Artist, title and year come exclusively from the MP3's embedded ID3 metadata; filenames and any old `library.json` are ignored. ID3v1 and ID3v2.2/2.3/2.4 tags are supported. Files without a title, artist or valid year are skipped with a server log message. Restart the server after changing files or tags to reload the collection.

The game plays each song from the beginning through to the end, unless a player locks in a guess, skips the song, or the host ends the turn. Replay restarts the song for everyone. You need at least one starting card per team or solo player plus one song to guess. A larger collection makes a longer game possible.

## Playing

- Choose **Blue**, **Red**, **Green**, or **Yellow** in the lobby to join that team. Players who keep **Solo** selected play individually. Teams can have any number of players, and solo players can play alongside teams. Change your choice until the host starts the game.
- Each team or solo player starts with one revealed song card and gets one turn per cycle. On your turn, listen and click a gap in your timeline, then **Lock it in**. Teams share their timeline; any teammate can submit, and the first submission counts.
- Correct guesses add the card; incorrect guesses do not. Either side of a song from the same year counts.
- Each team or solo player starts with zero tokens. Only the playing team can earn one token per song by submitting both the artist and title with its placement. The token is awarded even if the card is misplaced or stolen; leaving either field blank earns none.
- Artist and title must each match at least 80% after removing whitespace and ignoring capitalization. Similarity is `1 − edit distance / longer answer length`; inserted, missing, and substituted characters each count as one error. One credited artist is enough, including names separated by `,`, `;`, or `&`. Multiple names may be submitted in any order using those separators; each submitted name must match a credited artist at 80%. Featured credits using `feat.`, `ft.`, or `featuring` in artist or title metadata also count.
- Full titles with parentheses are valid. Parentheses at the beginning or in the middle contain optional words: include or omit them, but those words alone do not count. A final parenthesized phrase is an alternative title: either the main title or that phrase is enough. The same 80% threshold applies to each accepted title variant.
- Spend **two tokens** on **New song** during your turn to immediately draw a replacement and keep the turn. If there is no replacement song left, skipping is disabled and tokens are kept.
- After a placement locks, online opponents with tokens get **20 seconds** to place a **one-token steal** in a different gap on the playing team's timeline, or **Pass** for free. The answer remains hidden until all eligible opponents decide or time expires. Each opponent gets one decision per song; teammates share it.
- If the playing team is wrong, the first correct steal wins the card, which is inserted in year order into the stealer's own timeline. The playing team keeps the card whenever its placement is correct, including same-year ties. Every placed token is lost, even for failed or later correct steals. A stolen card can win the game.
- First team or solo player to 5, 7 or 10 cards wins. If the deck runs out, the highest card count wins, with shared wins for ties.
- Any member of the current team, the current solo player, or the host can replay for everyone and advance after a reveal. The host can end a turn without a guess for broken clips or disconnected players; this awards no card or token and gives up the turn. Turn rotation skips teams with no online members and offline solo players.
- Reconnecting preserves your team, cards, tokens, and any pending steal decision. Hosting transfers to an online player when the host disconnects. Returning to the lobby keeps online players’ colors and clears cards and tokens for a new game.
- Click **Enable sound** after reloading the page. Everyone plays the audio locally; the server coordinates readiness and a shared start time.

## Private hosting

Copy `.env.example` to `.env`. Variables already set in the environment take precedence.

| Variable | Purpose |
| --- | --- |
| `ADDR` | Defaults to `127.0.0.1:8080`. Use `0.0.0.0:8080` to listen on your LAN. |
| `DATA_DIR` | Defaults to `data`. Contains the `audio/` song folder. |
| `ACCESS_KEY` | Shared password for browser room creation and joining. Set it before exposing the server. |
| `PUBLIC_ORIGIN` | Your external HTTPS origin, if your reverse proxy changes the request host. |

Use HTTPS through a reverse proxy or tunnel for remote friends. Forward both HTTP and WebSockets to the Go server. On another computer, `localhost` points to that computer, so use the host computer’s address or your HTTPS URL instead. For LAN HTTP, modern browser audio/security features may vary; HTTPS is the supported remote setup.

Songs stay in the audio folder across restarts. Rooms and player sessions are in memory and reset on restart; idle rooms expire after 12 hours. Back up the data directory to preserve your library. The application serves only embedded web assets and authenticated current-round audio, not the data directory itself. Common ID3 tags are stripped from playback responses to hide answers; the original MP3 files and their metadata remain intact. Use audio you have permission to use.

## Discord Activity

The same frontend includes Discord initialization, OAuth login, a server-side user allowlist, and automatic room selection by Activity instance. No voice bot is needed. The SDK is bundled locally, so clients need no external JavaScript CDN.

1. Create an application in the [Discord Developer Portal](https://discord.com/developers/applications) and enable Activities. Enable your desired supported platforms; start with desktop/web.
2. Host Chartline at an HTTPS URL. Configure the Activity URL mapping `/` to your host name, without `https://`. The same mapping serves assets, API calls, audio and `/ws`.
3. In OAuth2 settings, register a redirect URI as required by Discord’s setup flow. The Embedded App SDK performs authorization inside Discord and the backend exchanges the resulting code.
4. Set `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, and `DISCORD_ALLOWED_USER_IDS` (comma-separated IDs for you and your friends) in `.env`. Keep the secret on the server. Restart Chartline.
5. Invite friends as **App Testers**, have them accept their invitations, enable the required Discord development/test settings, and launch the Activity in your small server. Everyone joining that Activity instance enters the same room.
6. Check that two players can join, enable sound and hear a clip from the audio folder.

Discord login grants access only to the configured user allowlist. The Activity instance ID routes players to a room; it is not used as an authorization credential. Browser entry additionally uses `ACCESS_KEY` when configured.

The Discord integration is implemented but requires your application credentials and an actual Discord client for live validation. Mobile audio and background/resume behavior still need testing on your devices.

References: [first Activity](https://docs.discord.com/developers/activities/building-an-activity), [networking](https://docs.discord.com/developers/activities/development-guides/networking), [inviting testers](https://support-dev.discord.com/hc/en-us/articles/21204493235991-How-Can-My-Friends-Play-My-Activity).

## Development

```sh
go test ./...
go vet ./...
go build .
```

Web assets are embedded at compilation: restart `go run .` after editing frontend files. `game.go` owns rules and public snapshots; `server.go` owns rooms, WebSockets and media access; `library.go` scans the audio folder, reads MP3 metadata and strips tags for playback; `discord.go` and `web/platform.js` isolate Discord integration; `web/audio.js` coordinates local audio playback.

Tests cover placements and same-year ties, team selection and shared timelines, turn authorization, stale actions, team and solo wins, deck exhaustion, multi-client synchronization, answer hiding, audio access, host transfer, reconnects, browser and Discord rooms above ten players, MP3 metadata loading, validation and playback without metadata and Discord ticket/instance handling.

Token tests cover the 80% recognition boundary, whitespace/case handling, artist subsets, optional title words and alternative titles, featured artists, earning on misplaced cards, skip costs and empty decks, competing steals, same-year priority, sorted stolen cards, steal wins, deadlines, passes, duplicate/stale actions, hidden answers, and reconnecting during a steal window.

Current draft limits: no mid-game joins, no persistent game history, no automatic song identification or release-year lookup. Players can inspect audio or use recognition tools; this is a game for trusted friends.
