import { embedded, endpoint, discordEntry } from './platform.js';
import { Playback } from './audio.js';

const root = document.querySelector('#app');
const dialog = document.querySelector('#dialog');
const params = new URLSearchParams(location.search);
const state = { config: null, session: null, room: null, connected: false, tab: params.get('room') ? 'join' : 'create', selected: null, error: '', library: [], loading: false, offset: 0, bestRTT: Infinity };
let ws, reconnectTimer, toastTimer, heartbeat, intentionalClose = false;
const icons = {
  arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
  play: '<path d="m9 5 11 7-11 7Z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  headphones: '<path d="M4 14v-3a8 8 0 0 1 16 0v3M4 13H3v7h4v-7ZM20 13h1v7h-4v-7Z"/>',
  users: '<circle cx="9" cy="8" r="3"/><path d="M3 20v-2a6 6 0 0 1 12 0v2M16 5a3 3 0 0 1 0 6m2 4a5 5 0 0 1 3 5"/>',
  copy: '<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M15 8V3H3v12h5"/>',
  music: '<path d="M9 17V5l11-2v12M9 8l11-2"/><ellipse cx="6" cy="18" rx="3" ry="2"/><ellipse cx="17" cy="16" rx="3" ry="2"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  replay: '<path d="M4 10a8 8 0 1 1 1 8M4 3v7h7"/>',
  skip: '<path d="m5 5 11 7L5 19ZM19 5v14"/>',
  upload: '<path d="M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9 9a3 3 0 0 1 6 0c0 2-3 2-3 5m0 3h.01"/>',
  exit: '<path d="M10 4H4v16h6m4-12 4 4-4 4m-6-4h14"/>',
  crown: '<path d="m3 6 5 5 4-7 4 7 5-5-2 13H5Z"/>',
};
const icon = (name, cls = '') => `<svg class="icon ${cls}" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.music}</svg>`;
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const me = () => state.room?.players.find(p => p.id === state.session?.playerId);
const isHost = () => state.room?.hostId === state.session?.playerId;
const myTurn = () => state.room?.turnId === state.session?.playerId;
const currentPlayer = () => state.room?.players.find(p => p.id === state.room?.turnId);
const now = () => Date.now() + state.offset;
const playback = new Playback({ token: () => state.session?.token, send, now, changed: updatePlayback });

async function request(path, body, options = {}) {
  const headers = { ...options.headers };
  if (state.session) headers.Authorization = `Bearer ${state.session.token}`;
  if (body !== undefined && !(body instanceof FormData)) headers['Content-Type'] = 'application/json';
  const response = await fetch(endpoint(path), { ...options, headers, method: body === undefined ? 'GET' : 'POST', body: body instanceof FormData ? body : body === undefined ? undefined : JSON.stringify(body) });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || 'The server could not complete that request.');
  return data;
}

function toast(message) {
  const element = document.querySelector('#toast');
  element.textContent = message;
  element.classList.add('visible');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => element.classList.remove('visible'), 4500);
}

function brand() {
  return `<a class="brand" href="/" aria-label="Chartline home" data-action="brand"><span class="brand-mark"><i></i><i></i><i></i><i></i></span>chartline<span class="brand-period">.</span></a>`;
}

function header() {
  return `<header class="site-header">${brand()}<span class="header-tag">A GOOD TIME, IN GOOD ORDER</span><nav aria-label="Game controls">${state.room ? `<button class="room-chip" data-action="copy" title="Copy invitation">${icon('copy')}<span>${esc(state.room.code)}</span></button><button class="sound-button" data-action="sound" aria-label="Enable sound">${icon('headphones')}<span id="sound-label">${playback.enabled ? 'Sound on' : 'Enable sound'}</span></button><label class="volume-label"><span class="sr-only">Music volume</span><input id="volume" type="range" min="0" max="100" value="${Math.round(playback.volume * 100)}" aria-label="Music volume"></label>` : `<span class="private-pill"><span class="status-dot"></span>Made for friends</span>`}<button class="icon-button" data-action="help" aria-label="How to play">${icon('help')}</button>${state.room ? `<button class="icon-button" data-action="leave" aria-label="Leave room">${icon('exit')}</button>` : ''}</nav></header>`;
}

function vinyl(className = '', center = 'SIDE A') {
  return `<div class="vinyl ${className}" aria-hidden="true"><div class="vinyl-grooves"></div><div class="vinyl-label"><span>CHARTLINE SESSIONS</span><strong>${center}</strong><i></i><small>33⅓ RPM · GOOD COMPANY</small></div></div>`;
}

function home() {
  const join = state.tab === 'join';
  return `${header()}<main class="home"><section class="hero"><div class="eyebrow"><span class="tiny-line"></span>THE MUSIC TIMELINE GAME</div><h1>Good music.<br>Questionable<br><em>memories.</em></h1><p class="hero-copy">You know the song. But do you know when?<br>Put your music memory on the line with your favourite people.</p><div class="hero-art">${vinyl('hero-vinyl')}<div class="floating-card card-one"><span>THE ONE YOU GREW UP WITH</span><strong>1998</strong><div class="mini-wave">▂▅▃▇▅▂▆▃▅</div></div><div class="floating-card card-two"><span>WAIT, THAT WAS WHEN?</span><strong>1984</strong>${icon('music')}</div><div class="art-caption">A LITTLE NOSTALGIA.<br>A LOT OF “NO WAY.”<span>↗</span></div></div></section><section class="entry-panel" aria-label="Start playing"><div class="entry-heading"><span class="eyebrow">GET THE GROUP TOGETHER</span><h2>Your next<br>listening party.</h2><p>2–10 friends. One very opinionated timeline.<br><span class="muted">Solo practice welcome, too.</span></p></div><div class="tab-switch" role="tablist" aria-label="Create or join a room"><button role="tab" aria-selected="${!join}" data-action="tab" data-tab="create" class="${!join ? 'active' : ''}">Create a room</button><button role="tab" aria-selected="${join}" data-action="tab" data-tab="join" class="${join ? 'active' : ''}">Join friends</button></div><form id="entry-form"><label class="field">YOUR NAME<input name="name" id="player-name" maxlength="24" autocomplete="nickname" placeholder="What should we call you?" value="${esc(sessionStorage.getItem('chartline.name') || '')}" required></label>${join ? `<label class="field">ROOM CODE<input name="code" class="code-input" placeholder="ABC123" maxlength="6" value="${esc(params.get('room') || '')}" required autocomplete="off"></label>` : `<fieldset class="library-choice"><legend>WHAT'S ON THE RECORD?</legend><label><input type="radio" name="library" value="demo" checked><span>${icon('music')}<strong>Try the demo</strong><small>24 practice clips</small></span></label><label><input type="radio" name="library" value="custom"><span>${icon('upload')}<strong>Your MP3s</strong><small>Add songs in the lobby</small></span></label></fieldset>`}${state.config?.accessKeyRequired ? `<label class="field">SHARED PASSWORD<input name="accessKey" type="password" autocomplete="current-password" required placeholder="From your host"></label>` : ''}<p id="entry-error" class="form-error" role="alert">${esc(state.error)}</p><button class="button button-dark button-full" type="submit" ${state.loading ? 'disabled' : ''}>${state.loading ? 'Getting things ready…' : join ? 'Join the party' : 'Let’s make some memories'}${icon('arrow')}</button><p class="entry-note">${join ? 'Ask your host for the six-character room code.' : 'Your room, your rules. Only people you invite can join.'}</p></form></section></main><section class="how-strip" aria-label="How to play"><div><span class="step-num">01</span><p><strong>Listen closely.</strong><span>A familiar tune. A mystery year.</span></p></div><div><span class="step-num">02</span><p><strong>Find its moment.</strong><span>Place it where it belongs in time.</span></p></div><div><span class="step-num">03</span><p><strong>Own the timeline.</strong><span>Collect the cards. Claim the bragging rights.</span></p></div></section><footer class="site-footer"><span>GOOD SONGS BRING PEOPLE TOGETHER.</span><span>Built for the group chat. ${icon('music')}</span></footer>`;
}

function sidebar() {
  const room = state.room;
  return `<aside class="sidebar"><div class="sidebar-title"><span class="eyebrow">THE LISTENING PARTY</span><span class="count-pill">${room.players.length}/10</span></div><div class="players">${room.players.map((p, i) => `<div class="player ${room.phase !== 'lobby' && room.turnId === p.id ? 'current' : ''}"><div class="avatar color-${i % 6}">${esc(p.name.slice(0, 2).toUpperCase())}<i class="presence ${p.online ? 'online' : ''}"></i></div><div class="player-info"><div class="player-name">${esc(p.name)}${p.id === state.session.playerId ? '<span>you</span>' : ''}${p.id === room.hostId ? icon('crown') : ''}</div><span class="player-status">${!p.online ? 'Reconnecting…' : room.phase === 'lobby' ? 'In the room' : `${p.cards.length} / ${room.target} cards`}</span>${room.phase !== 'lobby' ? `<div class="score-ticks">${Array.from({ length: room.target }, (_, n) => `<i class="${n < p.cards.length ? 'filled' : ''}"></i>`).join('')}</div>` : ''}</div>${room.phase === 'playing' && room.turnId === p.id ? '<span class="turn-dot" title="Current turn"></span>' : ''}</div>`).join('')}</div>${room.phase === 'lobby' ? `<button class="invite-button" data-action="copy">${icon('plus')}Invite your people</button>` : `<div class="sidebar-note">${icon('music')}<p>First to <strong>${room.target} cards</strong> wins.<br>Trust your ears. Debate the years.</p></div>`}<div class="library-badge"><span class="status-dot"></span>${room.library === 'demo' ? 'Practice session' : 'Your MP3 collection'}<small>${room.library === 'demo' ? 'Original loops · fictional years' : `${room.trackCount} songs in the library`}</small></div></aside>`;
}

function lobby() {
  const room = state.room;
  const enough = room.trackCount >= room.players.length + 1;
  return `<div class="game-heading"><div><span class="eyebrow">LET THE GOOD TIMES PLAY</span><h1>The gang’s getting together.</h1></div><span class="subtle-tag">${embedded ? 'Inside Discord' : 'Private room'}</span></div><section class="lobby-stage"><div class="lobby-visual">${vinyl('lobby-vinyl')}<span class="record-sticker">GOOD<br>COMPANY<br>ONLY ✳</span></div><div class="lobby-intro"><span class="eyebrow">SIDE A · THE WARM-UP</span><h2>Ready when<br><em>you are.</em></h2><p>Share your room with friends, pick your songs, and find out who really remembers the good old days.</p><button class="button button-outline" data-action="copy">${icon('copy')}Copy invitation</button></div><div class="lobby-settings"><label class="field">SONG LIBRARY<select id="library-setting" ${!isHost() ? 'disabled' : ''}><option value="demo" ${room.library === 'demo' ? 'selected' : ''}>Demo · 24 practice clips</option><option value="custom" ${room.library === 'custom' ? 'selected' : ''}>Your MP3s · ${room.library === 'custom' ? room.trackCount : state.config.customTrackCount} songs</option></select></label><label class="field">CARDS TO WIN<select id="target-setting" ${!isHost() ? 'disabled' : ''}>${[5, 7, 10].map(n => `<option value="${n}" ${room.target === n ? 'selected' : ''}>${n} cards${n === 5 ? ' · a quick game' : n === 7 ? ' · a little longer' : ' · the full session'}</option>`).join('')}</select></label>${isHost() ? `<button class="button button-outline library-manage" data-action="library">${icon('upload')}Manage MP3s</button>` : ''}</div><div class="lobby-bottom"><p>${room.library === 'demo' ? '<strong>A practice run.</strong> These are synthesized loops with fictional release years. Add your MP3s for a real music quiz.' : enough ? `<strong>${room.trackCount} songs, ready to play.</strong> We’ll deal one starting card to everyone, then take turns.` : `<strong>Add ${Math.max(0, room.players.length + 1 - room.trackCount)} more songs.</strong> You need one starting card per player and at least one song to guess.`}</p><button class="button button-dark" data-action="start" ${!isHost() || !enough || !state.connected ? 'disabled' : ''}>${isHost() ? room.players.length === 1 ? 'Start solo practice' : 'Start the party' : 'Waiting for the host'}${icon('arrow')}</button></div></section>`;
}

function songStage() {
  const room = state.room, round = room.round, active = currentPlayer();
  if (room.phase === 'reveal') {
    const result = round.result;
    return `<section class="song-stage reveal-stage ${result.correct ? 'reveal-correct' : 'reveal-miss'}"><div class="reveal-copy"><span class="eyebrow">${result.skipped ? 'A LITTLE SKIP IN THE RECORD' : result.correct ? 'RIGHT ON TIME' : 'A GOOD SONG. A DIFFERENT YEAR.'}</span><h2>${result.skipped ? 'On to the next one.' : result.correct ? myTurn() ? 'You know your stuff.' : `${esc(active.name)} nailed it.` : 'Almost a classic.'}</h2><h3>${esc(result.card.title)}</h3><p>${esc(result.card.artist)}</p><span class="result-note">${result.skipped ? 'This song was skipped. No cards change hands.' : result.correct ? 'One more memory on the timeline.' : 'No card this time. There’s always the next song.'}</span><button class="button button-dark" data-action="next" ${!isHost() && !myTurn() ? 'disabled' : ''}>${room.winners?.length ? 'See the results' : 'Next turn'}${icon('arrow')}</button></div><div class="reveal-year"><span>RELEASED IN</span><strong>${result.card.year}</strong><small>${room.library === 'demo' ? 'FICTIONAL DEMO YEAR' : 'ANOTHER MOMENT IN MUSIC'}</small><div class="year-ring"></div></div></section>`;
  }
  return `<section class="song-stage"><div class="song-record">${vinyl('playing-vinyl', '?')}<div class="record-shadow"></div></div><div class="song-copy"><span class="eyebrow">${myTurn() ? 'YOUR EARS. YOUR CALL.' : `${esc(active.name.toUpperCase())} IS ON THE DECKS`}</span><h2>Where does<br>this one <em>belong?</em></h2><p>${myTurn() ? 'Listen, take a guess, and find its place in your timeline.' : `Listen along while ${esc(active.name)} finds a place for this song.`}</p><div class="audio-status"><span class="equalizer"><i></i><i></i><i></i><i></i></span><span id="playback-label">Loading the clip…</span><span id="playback-time">0:00</span></div><div class="audio-progress"><div id="audio-progress-fill"></div></div><div class="audio-actions"><button class="button button-small button-outline" data-action="replay" ${!isHost() && !myTurn() ? 'disabled' : ''}>${icon('replay')}Replay for everyone</button>${isHost() ? `<button class="text-button" data-action="skip">${icon('skip')}Skip song</button>` : ''}<button class="text-button" data-action="retry-audio" id="retry-audio" hidden>Retry audio</button></div><p id="audio-error" class="form-error" role="status"></p></div></section>`;
}

function timeline() {
  const room = state.room, active = currentPlayer();
  const interactive = room.phase === 'playing' && myTurn();
  const gap = n => interactive ? `<button class="timeline-gap ${state.selected === n ? 'selected' : ''}" data-action="gap" data-position="${n}" aria-label="Place song ${n === 0 ? 'before ' + active.cards[0].year : n === active.cards.length ? 'after ' + active.cards[n - 1].year : 'between ' + active.cards[n - 1].year + ' and ' + active.cards[n].year}" aria-pressed="${state.selected === n}">${state.selected === n ? `<span>YOUR PICK</span>${icon('music')}<strong>New song</strong><small>Lock it in below</small>` : icon('plus')}</button>` : '';
  return `<section class="timeline-section"><div class="timeline-heading"><div><h2>${myTurn() ? 'Your timeline' : `${esc(active.name)}’s timeline`}<span>${active.cards.length} / ${room.target}</span></h2><p>${interactive ? 'Tap a gap. Let your music memory do the rest.' : room.phase === 'reveal' ? 'A little more music history, settled.' : 'The oldest memories on the left. The newest on the right.'}</p></div><span class="timeline-direction">THEN <span>⟶</span> NOW</span></div><div class="timeline-scroll" id="timeline-scroll"><div class="timeline-track">${gap(0)}${active.cards.map((c, i) => `<article class="song-card card-color-${i % 4}"><span class="card-label">IN THE COLLECTION</span><strong class="card-year">${c.year}</strong><div class="card-divider"></div><h3>${esc(c.title)}</h3><p>${esc(c.artist)}</p><span class="card-corner">${icon('music')}</span></article>${gap(i + 1)}`).join('')}</div></div><div class="timeline-bottom"><p>${icon('help')}Same year? Either side counts.</p>${interactive ? `<button id="lock-button" class="button button-dark" data-action="place" ${state.selected === null || !state.connected || !room.round.startAt || now() < room.round.startAt ? 'disabled' : ''}>Lock it in${icon('check')}</button>` : `<span class="turn-caption">${room.phase === 'reveal' ? 'The stories behind the songs are half the fun.' : `It’s ${esc(active.name)}’s turn.`}</span>`}</div></section>`;
}

function finished() {
  const room = state.room;
  const winners = room.players.filter(p => room.winners?.includes(p.id));
  const ranking = [...room.players].sort((a, b) => b.cards.length - a.cards.length);
  return `<section class="results"><span class="eyebrow">SIDE B · THE ENCORE</span><div class="winner-icon">${icon('crown')}</div><h1>${winners.some(p => p.id === state.session.playerId) ? 'Take a bow.' : 'A round of applause.'}</h1><p class="winner-name">${winners.map(p => esc(p.name)).join(' & ')} ${winners.length > 1 ? 'share the spotlight.' : 'owns the timeline.'}</p><p class="muted">${esc(room.finishReason)}</p><div class="leaderboard">${ranking.map((p, i) => `<div><span class="rank">${String(i + 1).padStart(2, '0')}</span><strong>${esc(p.name)}${p.id === state.session.playerId ? ' <small>(you)</small>' : ''}</strong><span>${p.cards.length} cards</span></div>`).join('')}</div><button class="button button-dark" data-action="reset" ${!isHost() ? 'disabled' : ''}>${isHost() ? 'One more session?' : 'Waiting for the host'}${icon('replay')}</button><p class="small muted">New shuffle. Fresh start. Same good company.</p></section>`;
}

function game() {
  const room = state.room;
  return `${header()}${!state.connected ? '<div class="connection-banner" role="status">Reconnecting to your room… Your timeline is saved on the server.</div>' : ''}<main class="game-layout">${sidebar()}<div class="game-main">${room.phase === 'lobby' ? lobby() : room.phase === 'finished' ? finished() : `<div class="game-heading"><div><span class="eyebrow">${room.library === 'demo' ? 'PRACTICE SESSION · FICTIONAL YEARS' : 'THE SOUNDTRACK TO YOUR MEMORIES'}</span><h1>Round ${String(room.round.number).padStart(2, '0')}<span class="round-subtitle">${myTurn() ? 'Make it a good one.' : 'Everyone’s listening.'}</span></h1></div><span class="subtle-tag">${room.remaining} songs to come</span></div>${songStage()}${timeline()}`}</div></main><footer class="game-footer"><span>${icon('headphones')}A LITTLE MUSIC. A LITTLE NOSTALGIA. A LOT OF GOOD COMPANY.</span><button class="text-button" data-action="help">The rules, please ${icon('arrow')}</button></footer>`;
}

function render() {
  const focus = document.activeElement?.id;
  const scroll = document.querySelector('#timeline-scroll')?.scrollLeft || 0;
  if (state.room) root.innerHTML = game();
  else if (embedded || (state.session && !state.error)) root.innerHTML = `${header()}<main class="boot"><span class="brand-mark">≋</span><h1>${state.error ? 'The record hasn’t started yet.' : 'Joining the listening party…'}</h1><p>${esc(state.error || 'Getting everyone in the same room.')}</p>${state.error ? '<button class="button button-dark" data-action="reload">Try again</button>' : ''}</main>`;
  else root.innerHTML = home();
  const timelineEl = document.querySelector('#timeline-scroll');
  if (timelineEl) timelineEl.scrollLeft = scroll;
  if (focus && !dialog.open) document.getElementById(focus)?.focus({ preventScroll: true });
  updatePlayback();
}

function updatePlayback() {
  const soundLabel = document.querySelector('#sound-label');
  if (soundLabel) soundLabel.textContent = playback.enabled && playback.ctx?.state === 'running' ? 'Sound on' : 'Enable sound';
  const round = state.room?.round;
  if (!round || state.room.phase !== 'playing') return;
  const label = document.querySelector('#playback-label');
  if (!label) return;
  const elapsed = round.startAt ? Math.max(0, (now() - round.startAt) / 1000) : 0;
  const before = round.startAt && now() < round.startAt;
  const ended = round.startAt && elapsed >= round.duration || playback.status === 'ended';
  label.textContent = playback.error ? 'A little trouble with the record.' : !playback.enabled || playback.ctx?.state !== 'running' ? 'Click Enable sound to listen.' : !playback.buffer ? 'Loading the clip…' : !round.startAt ? 'Getting everyone ready…' : before ? 'Needle dropping…' : ended ? 'Got a year in mind?' : 'Now playing · mystery track';
  document.querySelector('#playback-time').textContent = `0:${String(Math.min(Math.floor(elapsed), round.duration)).padStart(2, '0')}`;
  document.querySelector('#audio-progress-fill').style.width = `${Math.min(100, elapsed / round.duration * 100)}%`;
  document.querySelector('.playing-vinyl')?.classList.toggle('spinning', Boolean(playback.enabled && round.startAt && !before && !ended && !playback.error));
  document.querySelector('.equalizer')?.classList.toggle('active', Boolean(playback.enabled && round.startAt && !before && !ended));
  document.querySelector('#audio-error').textContent = playback.error || '';
  document.querySelector('#retry-audio').hidden = !playback.error;
  const lock = document.querySelector('#lock-button');
  if (lock) lock.disabled = state.selected === null || !state.connected || !round.startAt || now() < round.startAt;
}

function connect() {
  clearTimeout(reconnectTimer);
  if (!state.session) return;
  intentionalClose = false;
  const url = new URL(endpoint('ws'));
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  const socket = new WebSocket(url);
  ws = socket;
  socket.onopen = () => {
    if (ws !== socket) return socket.close();
    socket.send(JSON.stringify({ type: 'auth', token: state.session.token }));
    send('ping', { clientTime: Date.now() });
    clearInterval(heartbeat);
    heartbeat = setInterval(() => send('ping', { clientTime: Date.now() }), 10000);
  };
  socket.onmessage = event => {
    if (ws !== socket) return;
    const message = JSON.parse(event.data);
    if (message.type === 'state') {
      const oldRound = state.room?.round?.id;
      state.connected = true;
      state.error = '';
      state.room = message.room;
      if (state.bestRTT === Infinity) state.offset = message.serverTime - Date.now();
      if (oldRound !== state.room.round?.id) state.selected = null;
      if (state.room.library === 'custom') state.config.customTrackCount = state.room.trackCount;
      if (dialog.open && state.room.phase !== 'lobby' && dialog.dataset.kind === 'library') dialog.close();
      render();
      playback.sync(state.room);
    } else if (message.type === 'pong') {
      const rtt = Date.now() - message.clientTime;
      if (rtt < state.bestRTT) { state.bestRTT = rtt; state.offset = message.serverTime - (message.clientTime + rtt / 2); }
    } else if (message.type === 'error') toast(message.message);
  };
  socket.onclose = event => {
    if (ws !== socket) return;
    clearInterval(heartbeat);
    state.connected = false;
    if (intentionalClose) return;
    if (event.code === 1008) {
      playback.clear();
      state.room = null;
      state.session = null;
      sessionStorage.removeItem('chartline.session');
      state.error = 'Your room session has expired. Create or join a room again.';
      render();
      return;
    }
    render();
    reconnectTimer = setTimeout(connect, 1800);
  };
  socket.onerror = () => socket.close();
}

function send(type, payload = {}) {
  if (ws?.readyState !== WebSocket.OPEN) { if (type !== 'ready' && type !== 'ping') toast('Reconnecting. Give us a moment.'); return; }
  ws.send(JSON.stringify({ type, ...(state.room?.round ? { roundId: state.room.round.id } : {}), ...payload }));
}

async function enter(form) {
  const data = new FormData(form);
  const name = String(data.get('name')).trim();
  sessionStorage.setItem('chartline.name', name);
  playback.enable();
  state.loading = true;
  state.error = '';
  const button = form.querySelector('[type="submit"]');
  button.disabled = true;
  try {
    state.session = await request(state.tab === 'join' ? 'api/join' : 'api/rooms', { name, code: String(data.get('code') || ''), library: String(data.get('library') || 'demo'), target: 5, accessKey: String(data.get('accessKey') || '') });
    sessionStorage.setItem('chartline.session', JSON.stringify(state.session));
    render();
    connect();
  } catch (error) {
    state.error = error.message;
    document.querySelector('#entry-error').textContent = error.message;
    button.disabled = false;
  } finally { state.loading = false; }
}

async function leave() {
  try { await request('api/leave', {}); } catch { /* Local exit still works offline. */ }
  intentionalClose = true;
  clearTimeout(reconnectTimer);
  clearInterval(heartbeat);
  ws?.close();
  ws = null;
  playback.clear();
  state.room = null;
  state.session = null;
  state.error = embedded ? 'You left the room. Reopen the Activity to join again.' : '';
  sessionStorage.removeItem('chartline.session');
  render();
}

function openHelp() {
  dialog.dataset.kind = 'help';
  dialog.innerHTML = `<div class="dialog-header"><div><span class="eyebrow">THE RULES OF THE RECORD</span><h2 id="dialog-title">Good ears. Better guesses.</h2></div><button class="icon-button" data-action="close-dialog" aria-label="Close">${icon('close')}</button></div><ol class="rules"><li><strong>Start with a little history.</strong>Everyone gets one song card, with its title and release year revealed.</li><li><strong>Listen to the mystery song.</strong>Everyone hears the same clip. On your turn, choose a gap in your timeline, from oldest to newest.</li><li><strong>Lock it in.</strong>Place the song in the right chronological position to keep the card. Same-year songs can go on either side.</li><li><strong>Build your collection.</strong>The first player to the target wins. If the library runs out, the most cards wins; ties share the win.</li><li><strong>Keep the session moving.</strong>The host can skip a broken song. Disconnected players keep their cards; the host can skip their turn.</li></ol><p class="info-note">Demo clips are original synthesized loops with fictional years. They let you try the game mechanics. Upload MP3s with real release years for a music quiz.</p><button class="button button-dark button-full" data-action="close-dialog">Got it. Drop the needle.${icon('play')}</button>`;
  dialog.showModal();
}

async function openLibrary() {
  try { state.library = await request('api/library'); } catch (error) { toast(error.message); return; }
  dialog.dataset.kind = 'library';
  dialog.innerHTML = `<div class="dialog-header"><div><span class="eyebrow">CURATE THE GOOD TIMES</span><h2 id="dialog-title">Your song collection.</h2></div><button class="icon-button" data-action="close-dialog" aria-label="Close library">${icon('close')}</button></div><p class="muted">Add MP3s and the original release year of each recording. Songs stay in this server’s library for your next session.</p><form id="upload-form"><label class="file-drop" id="file-drop">${icon('upload')}<strong id="file-name">Choose an MP3 or drop it here</strong><span>Up to 32 MB · one song at a time</span><input type="file" name="file" accept=".mp3,audio/mpeg" id="mp3-file" required></label><audio id="upload-preview" controls hidden preload="metadata"></audio><div class="form-grid"><label class="field">SONG TITLE<input name="title" id="song-title" maxlength="100" required placeholder="The name of the song"></label><label class="field">ARTIST<input name="artist" id="song-artist" maxlength="100" required placeholder="Who made it?"></label><label class="field">RELEASE YEAR<input name="year" type="number" min="1800" max="${new Date().getFullYear() + 1}" required placeholder="1998"></label><label class="field">CLIP START (SECONDS)<input name="start" type="number" min="0" max="3600" step="0.1" value="0"><span class="field-hint">We play up to 20 seconds from here.</span></label></div><p class="form-error" id="upload-error" role="alert"></p><button class="button button-dark button-full" type="submit">Add to the collection${icon('plus')}</button></form><div class="library-list-heading"><h3>On the shelf</h3><span id="library-count"></span></div><div id="library-list" class="library-list"></div>`;
  renderLibraryList();
  dialog.showModal();
}

function renderLibraryList() {
  document.querySelector('#library-count').textContent = `${state.library.length} songs`;
  document.querySelector('#library-list').innerHTML = state.library.length ? state.library.map(t => `<div><span class="library-year">${t.year}</span><p><strong>${esc(t.title)}</strong><span>${esc(t.artist)}</span></p><small>${t.start}s →</small></div>`).join('') : '<p class="empty-library">The shelf is waiting for your favourites.</p>';
}

async function upload(form) {
  const button = form.querySelector('[type="submit"]');
  button.disabled = true;
  button.textContent = 'Saving your song…';
  document.querySelector('#upload-error').textContent = '';
  try {
    const result = await request('api/library', new FormData(form));
    state.config.customTrackCount = result.count;
    form.reset();
    clearPreview();
    document.querySelector('#file-name').textContent = 'Choose an MP3 or drop it here';
    state.library = await request('api/library');
    renderLibraryList();
    toast('Song added. A little more history on the shelf.');
  } catch (error) { document.querySelector('#upload-error').textContent = error.message; }
  finally { button.disabled = false; button.innerHTML = `Add to the collection${icon('plus')}`; }
}

let previewURL;
function clearPreview() {
  const preview = document.querySelector('#upload-preview');
  if (preview) { preview.pause(); preview.removeAttribute('src'); preview.load(); preview.hidden = true; }
  if (previewURL) URL.revokeObjectURL(previewURL);
  previewURL = null;
}
function chooseFile(file) {
  if (!file) return;
  clearPreview();
  document.querySelector('#file-name').textContent = file.name;
  const name = file.name.replace(/\.mp3$/i, '');
  const parts = name.split(' - ');
  document.querySelector('#song-title').value = parts.length > 1 ? parts.slice(1).join(' - ') : name;
  document.querySelector('#song-artist').value = parts.length > 1 ? parts[0] : '';
  previewURL = URL.createObjectURL(file);
  const preview = document.querySelector('#upload-preview');
  preview.src = previewURL;
  preview.hidden = false;
}

document.addEventListener('submit', event => {
  if (event.target.id === 'entry-form') { event.preventDefault(); enter(event.target); }
  if (event.target.id === 'upload-form') { event.preventDefault(); upload(event.target); }
});
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-action]');
  if (!button || button.disabled) return;
  const action = button.dataset.action;
  if (action === 'brand') { event.preventDefault(); if (!state.room) { state.tab = 'create'; render(); } return; }
  if (action === 'tab') { const name = document.querySelector('#player-name')?.value; if (name) sessionStorage.setItem('chartline.name', name); state.tab = button.dataset.tab; state.error = ''; render(); }
  else if (action === 'help') openHelp();
  else if (action === 'sound') playback.enable();
  else if (action === 'reload') location.reload();
  else if (action === 'leave') leave();
  else if (action === 'close-dialog') dialog.close();
  else if (action === 'library') openLibrary();
  else if (action === 'retry-audio') playback.retry();
  else if (action === 'copy') {
    const text = embedded ? state.room.code : `${location.origin}/?room=${state.room.code}`;
    try { await navigator.clipboard.writeText(text); toast(embedded ? 'Room code copied.' : 'Invitation copied. Send it to your friends.'); }
    catch { toast(`Your room code is ${state.room.code}`); }
  }
  else if (action === 'gap') { state.selected = Number(button.dataset.position); render(); document.querySelector('.timeline-gap.selected')?.focus({ preventScroll: true }); }
  else if (action === 'place') { if (state.selected !== null) send('place', { position: state.selected }); }
  else if (['start', 'next', 'reset', 'replay', 'skip'].includes(action)) { if (action === 'start' || action === 'replay') playback.enable(); send(action); }
});
document.addEventListener('input', event => {
  if (event.target.id === 'volume') playback.setVolume(Number(event.target.value) / 100);
});
document.addEventListener('change', event => {
  if (event.target.id === 'library-setting' || event.target.id === 'target-setting') send('settings', { library: document.querySelector('#library-setting').value, target: Number(document.querySelector('#target-setting').value) });
  if (event.target.id === 'mp3-file') chooseFile(event.target.files[0]);
});
dialog.addEventListener('close', clearPreview);
dialog.addEventListener('click', event => { if (event.target === dialog) { const rect = dialog.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) dialog.close(); } });
document.addEventListener('dragover', event => { if (event.target.closest('#file-drop')) { event.preventDefault(); event.target.closest('#file-drop').classList.add('dragging'); } });
document.addEventListener('dragleave', event => event.target.closest('#file-drop')?.classList.remove('dragging'));
document.addEventListener('drop', event => {
  const drop = event.target.closest('#file-drop');
  if (!drop) return;
  event.preventDefault();
  drop.classList.remove('dragging');
  const file = event.dataTransfer.files[0];
  if (!file) return;
  const transfer = new DataTransfer(); transfer.items.add(file);
  document.querySelector('#mp3-file').files = transfer.files;
  chooseFile(file);
});
document.addEventListener('visibilitychange', () => { if (!document.hidden && state.room) { send('ping', { clientTime: Date.now() }); playback.schedule(); } });
setInterval(updatePlayback, 200);

async function boot() {
  try {
    state.config = await request('api/config');
    if (embedded) {
      render();
      state.session = await discordEntry(state.config, request);
    } else {
      try { state.session = JSON.parse(sessionStorage.getItem('chartline.session')); } catch { sessionStorage.removeItem('chartline.session'); }
    }
    render();
    if (state.session) connect();
  } catch (error) { state.error = error.message; render(); }
}
boot();
