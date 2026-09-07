import { embedded, endpoint, discordEntry } from './platform.js';
import { Playback } from './audio.js';

const root = document.querySelector('#app');
const dialog = document.querySelector('#dialog');
const params = new URLSearchParams(location.search);
const state = { config: null, session: null, room: null, connected: false, tab: params.get('room') ? 'join' : 'create', selected: null, artistGuess: '', titleGuess: '', error: '', library: [], loading: false, offset: 0, bestRTT: Infinity };
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
const belongsToMe = timeline => timeline?.playerIds.includes(state.session?.playerId);
const currentTimeline = () => state.room?.timelines.find(t => t.id === state.room?.turnId);
const myTurn = () => belongsToMe(currentTimeline());
const ownTimeline = () => state.room?.timelines.find(belongsToMe);
const canSteal = () => {
  const t = ownTimeline(), round = state.room?.round;
  return state.room?.phase === 'stealing' && t?.tokens > 0 && round.stealEligible.includes(t.id) && !round.passed.includes(t.id) && !round.steals.some(s => s.timelineId === t.id) && now() < round.stealUntil;
};
const tokenLabel = n => `${n} ${n === 1 ? 'token' : 'tokens'}`;
const teamColors = ['blue', 'red', 'green', 'yellow'];
const teamLabel = color => color ? `${color[0].toUpperCase()}${color.slice(1)} team` : 'Solo';
const teamClass = color => teamColors.includes(color) ? `team-${color}` : 'team-solo';
const teamBadge = color => `<span class="team-badge ${teamClass(color)}"><i class="team-swatch" aria-hidden="true"></i>${teamLabel(color)}</span>`;
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

function header() {
  return `<header class="site-header"><a class="brand" href="/" data-action="brand">Chartline</a><nav aria-label="Game controls">${state.room ? `<button class="room-chip" data-action="copy" title="Copy invitation">Room <span>${esc(state.room.code)}</span></button><button class="sound-button" data-action="sound">${icon('headphones')}<span id="sound-label">${playback.enabled ? 'Sound on' : 'Enable sound'}</span></button><label class="volume-label"><span class="sr-only">Music volume</span><input id="volume" type="range" min="0" max="100" value="${Math.round(playback.volume * 100)}" aria-label="Music volume"></label>` : ''}<button class="text-button" data-action="help">Rules</button>${state.room ? '<button class="text-button" data-action="leave">Leave</button>' : ''}</nav></header>`;
}

function home() {
  const join = state.tab === 'join';
  return `${header()}<main class="home"><section class="entry-panel" aria-label="Start playing"><h1>Play Chartline</h1><p class="muted">Place songs in release-year order.</p><div class="tab-switch" role="tablist" aria-label="Create or join a room"><button role="tab" aria-selected="${!join}" data-action="tab" data-tab="create" class="${!join ? 'active' : ''}">Create room</button><button role="tab" aria-selected="${join}" data-action="tab" data-tab="join" class="${join ? 'active' : ''}">Join room</button></div><form id="entry-form"><label class="field">Name<input name="name" id="player-name" maxlength="24" autocomplete="nickname" value="${esc(sessionStorage.getItem('chartline.name') || '')}" required></label>${join ? `<label class="field">Room code<input name="code" class="code-input" maxlength="6" value="${esc(params.get('room') || '')}" required autocomplete="off"></label>` : '<fieldset class="library-choice"><legend>Song library</legend><label><input type="radio" name="library" value="demo" checked>Demo <span class="muted">(fictional years)</span></label><label><input type="radio" name="library" value="custom">Your MP3s</label></fieldset>'}${state.config?.accessKeyRequired ? '<label class="field">Shared password<input name="accessKey" type="password" autocomplete="current-password" required></label>' : ''}<p id="entry-error" class="form-error" role="alert">${esc(state.error)}</p><button class="button button-dark button-full" type="submit" ${state.loading ? 'disabled' : ''}>${state.loading ? 'Connecting…' : join ? 'Join room' : 'Create room'}</button></form></section></main>`;
}

function sidebar() {
  const room = state.room;
  const playersById = new Map(room.players.map(p => [p.id, p]));
  return `<aside class="sidebar" aria-label="Players and scores"><h2>Players <span class="muted">(${room.players.length})</span></h2><div class="players">${room.timelines.map(t => `<div class="party-group ${teamClass(t.team)} ${room.phase !== 'lobby' && room.turnId === t.id ? 'current' : ''}"><div class="party-group-heading">${teamBadge(t.team)}</div>${t.playerIds.map(id => { const p = playersById.get(id); return `<div class="player"><span class="player-name">${esc(p.name)}</span><span class="player-status">${[p.id === state.session.playerId ? 'you' : '', p.id === room.hostId ? 'host' : '', !p.online ? 'offline' : ''].filter(Boolean).join(', ')}</span></div>`; }).join('')}${room.phase !== 'lobby' ? `<div class="player-score"><span>${t.cards.length} / ${room.target} cards</span><span class="token-balance">${tokenLabel(t.tokens)}</span></div>` : ''}</div>`).join('')}</div></aside>`;
}

function teamPicker() {
  const selected = me()?.team || '';
  return `<fieldset class="team-picker" aria-describedby="team-hint"><legend>Team</legend><p id="team-hint" class="muted">Choose the same color to play together, or stay solo.</p><div class="team-options">${['', ...teamColors].map(color => `<label class="team-option ${teamClass(color)}"><input id="team-${color || 'solo'}" type="radio" name="team" value="${color}" ${selected === color ? 'checked' : ''} ${!state.connected ? 'disabled' : ''}><span><i class="team-swatch" aria-hidden="true"></i>${color ? teamLabel(color).replace(' team', '') : 'Solo'}</span></label>`).join('')}</div></fieldset>`;
}

function lobby() {
  const room = state.room, enough = room.trackCount >= room.timelines.length + 1;
  return `<div class="game-heading"><h1>Lobby</h1><button class="button button-outline" data-action="copy">Copy invitation</button></div><section class="lobby-stage">${teamPicker()}<div class="lobby-settings"><label class="field">Song library<select id="library-setting" ${!isHost() ? 'disabled' : ''}><option value="demo" ${room.library === 'demo' ? 'selected' : ''}>Demo · ${state.config.demoTrackCount} songs</option><option value="custom" ${room.library === 'custom' ? 'selected' : ''}>Your MP3s · ${room.library === 'custom' ? room.trackCount : state.config.customTrackCount} songs</option></select></label><label class="field">Cards to win<select id="target-setting" ${!isHost() ? 'disabled' : ''}>${[5, 7, 10].map(n => `<option value="${n}" ${room.target === n ? 'selected' : ''}>${n}</option>`).join('')}</select></label>${isHost() ? '<button class="button button-outline library-manage" data-action="library">Manage MP3s</button>' : ''}</div><div class="lobby-bottom"><p class="muted">${!enough ? `Add ${room.timelines.length + 1 - room.trackCount} more songs or join a team.` : room.library === 'demo' ? 'Demo songs use fictional release years.' : `${room.trackCount} songs available.`}</p><button class="button button-dark" data-action="start" ${!isHost() || !enough || !state.connected ? 'disabled' : ''}>${isHost() ? 'Start game' : 'Waiting for host'}</button></div></section>`;
}

function songStage() {
  const room = state.room, round = room.round, active = currentTimeline();
  if (room.phase === 'stealing') {
    const own = ownTimeline(), decided = round.steals.some(s => s.timelineId === own?.id) || round.passed.includes(own?.id);
    return `<section class="song-stage steal-stage"><div class="stage-heading"><h2>Steal the song</h2><span class="steal-clock" role="timer" aria-label="Time left to steal"><strong id="steal-countdown"></strong> left</span></div><p>${myTurn() ? 'Your placement is locked. Waiting for opponents.' : decided ? 'Decision submitted. Waiting for opponents.' : canSteal() ? `Choose a different gap on ${esc(active.name)}’s timeline. A steal costs 1 token.` : 'Waiting for opponents to steal or pass.'}</p>${canSteal() ? '<button class="button button-outline" data-action="pass">Pass</button>' : ''}</section>`;
  }
  if (room.phase === 'reveal') {
    const result = round.result, recipient = room.timelines.find(t => t.id === result.awardedTo);
    const stolen = recipient && recipient.id !== active.id;
    return `<section class="song-stage reveal-stage ${result.correct || stolen ? 'reveal-correct' : 'reveal-miss'}"><div class="reveal-copy"><h2>${result.skipped ? 'Turn skipped' : stolen ? `${esc(recipient.name)} stole the card` : result.correct ? 'Correct placement' : 'Incorrect placement'}</h2><h3>${esc(result.card.title)}</h3><p>${esc(result.card.artist)}</p>${!result.skipped ? `<p class="token-result ${result.tokenEarned ? 'token-earned' : ''}">${result.tokenEarned ? `+1 token for ${esc(active.name)}` : 'No token earned'}</p>` : ''}<button class="button button-dark" data-action="next" ${!isHost() && !myTurn() ? 'disabled' : ''}>${room.winners?.length ? 'Results' : 'Next turn'}</button></div><div class="reveal-year"><strong>${result.card.year}</strong>${room.library === 'demo' ? '<span class="muted">Demo year</span>' : ''}</div></section>`;
  }
  return `<section class="song-stage"><div class="audio-status"><span id="playback-label">Loading audio…</span><span id="playback-time">0:00</span></div><div class="audio-progress" role="presentation"><div id="audio-progress-fill"></div></div><div class="audio-actions"><button class="button button-outline" data-action="replay" ${!isHost() && !myTurn() ? 'disabled' : ''}>Replay</button>${myTurn() ? `<button class="button button-outline" data-action="skip" ${active.tokens < 2 || !room.remaining || !state.connected ? 'disabled' : ''} title="Spend 2 tokens to replace the song and keep your turn">Skip · 2 tokens</button>` : ''}${isHost() ? '<button class="text-button host-discard" data-action="discard" title="Host control for broken clips or disconnected players. Gives up this turn without awarding a card or token.">End turn</button>' : ''}<button class="text-button" data-action="retry-audio" id="retry-audio" hidden>Retry audio</button></div><p id="audio-error" class="form-error" role="status"></p></section>`;
}

function recognitionForm() {
  return `<form id="recognition-form" class="recognition-form" aria-labelledby="recognition-title"><h3 id="recognition-title">Song guess <span class="muted">(optional)</span></h3><p class="muted">Match the artist and title at least 80% to earn 1 token, even if the card is misplaced.</p><div class="form-grid"><label class="field">Artist<input id="guess-artist" maxlength="100" autocomplete="off" value="${esc(state.artistGuess)}"></label><label class="field">Song title<input id="guess-title" maxlength="100" autocomplete="off" value="${esc(state.titleGuess)}"></label></div></form>`;
}

function timeline() {
  const room = state.room, active = currentTimeline(), stealing = room.phase === 'stealing';
  const placing = room.phase === 'playing' && myTurn(), interactive = placing || canSteal();
  const gap = n => {
    const label = n === 0 ? 'before ' + active.cards[0].year : n === active.cards.length ? 'after ' + active.cards[n - 1].year : 'between ' + active.cards[n - 1].year + ' and ' + active.cards[n].year;
    let markers = '';
    if (stealing) {
      if (n === room.round.lockedPosition) return '<div class="timeline-marker locked-marker"><strong>Locked</strong></div>';
      markers = room.round.steals.filter(s => s.position === n).map(steal => {
        const t = room.timelines.find(t => t.id === steal.timelineId);
        return `<div class="timeline-marker ${teamClass(t.team)}"><strong>${esc(t.name)}</strong><span>1 token</span></div>`;
      }).join('');
    }
    if (!interactive) return markers;
    return `${markers}<button type="button" class="timeline-gap ${state.selected === n ? 'selected' : ''}" data-action="gap" data-position="${n}" aria-label="${stealing ? 'Place steal token' : 'Place song'} ${label}" aria-pressed="${state.selected === n}">${icon(state.selected === n ? 'check' : 'plus')}</button>`;
  };
  return `<section class="timeline-section"><div class="timeline-heading"><h2>${myTurn() ? 'Your timeline' : `${esc(active.name)}’s timeline`}</h2>${placing ? '<span class="muted">Select a gap</span>' : ''}</div><div class="timeline-scroll" id="timeline-scroll"><div class="timeline-track">${gap(0)}${active.cards.map((c, i) => `<article class="song-card"><strong class="card-year">${c.year}</strong><h3>${esc(c.title)}</h3><p>${esc(c.artist)}</p></article>${gap(i + 1)}`).join('')}</div></div>${placing ? recognitionForm() : ''}<div class="timeline-bottom">${placing ? `<button id="lock-button" type="submit" form="recognition-form" class="button button-dark" ${state.selected === null || !state.connected || !room.round.startAt || now() < room.round.startAt ? 'disabled' : ''}>Lock in</button>` : canSteal() ? `<button id="steal-button" class="button button-dark" data-action="steal" ${state.selected === null || !state.connected ? 'disabled' : ''}>Steal · 1 token</button>` : ''}</div></section>`;
}

function finished() {
  const room = state.room, winners = room.timelines.filter(t => room.winners?.includes(t.id));
  const ranking = [...room.timelines].sort((a, b) => b.cards.length - a.cards.length);
  return `<section class="results"><h1>Results</h1><p class="winner-name">${winners.map(p => esc(p.name)).join(' & ')} ${winners.length > 1 ? 'tie.' : 'wins.'}</p><div class="leaderboard">${ranking.map((p, i) => `<div><span class="rank">${i + 1}</span><strong>${esc(p.name)}${belongsToMe(p) ? ' <small>(you)</small>' : ''}</strong><span>${p.cards.length} ${p.cards.length === 1 ? 'card' : 'cards'}</span></div>`).join('')}</div><button class="button button-dark" data-action="reset" ${!isHost() ? 'disabled' : ''}>${isHost() ? 'Return to lobby' : 'Waiting for host'}</button></section>`;
}

function game() {
  const room = state.room;
  return `${header()}${!state.connected ? '<div class="connection-banner" role="status">Reconnecting… Your progress is saved.</div>' : ''}<main class="game-layout">${sidebar()}<div class="game-main">${room.phase === 'lobby' ? lobby() : room.phase === 'finished' ? finished() : `<div class="game-heading"><div><h1>Round ${room.round.number}</h1><p class="muted">${myTurn() ? 'Your turn' : `${esc(currentTimeline().name)}’s turn`}${room.library === 'demo' ? ' · Demo' : ''}</p></div><span class="muted">${room.remaining} songs left</span></div>${songStage()}${timeline()}`}</div></main>`;
}
function render() {
  const focus = document.activeElement?.id;
  const selection = ['guess-artist', 'guess-title'].includes(focus) ? [document.activeElement.selectionStart, document.activeElement.selectionEnd] : null;
  const scroll = document.querySelector('#timeline-scroll')?.scrollLeft || 0;
  const roster = document.querySelector('.players');
  const rosterScroll = { left: roster?.scrollLeft || 0, top: roster?.scrollTop || 0 };
  if (state.room) root.innerHTML = game();
  else if (embedded || (state.session && !state.error)) root.innerHTML = `${header()}<main class="boot"><span class="brand-mark">≋</span><h1>${state.error ? 'The record hasn’t started yet.' : 'Joining the listening party…'}</h1><p>${esc(state.error || 'Getting everyone in the same room.')}</p>${state.error ? '<button class="button button-dark" data-action="reload">Try again</button>' : ''}</main>`;
  else root.innerHTML = home();
  const timelineEl = document.querySelector('#timeline-scroll');
  if (timelineEl) timelineEl.scrollLeft = scroll;
  document.querySelector('.players')?.scrollTo(rosterScroll);
  if (focus && !dialog.open) document.getElementById(focus)?.focus({ preventScroll: true });
  if (selection) document.getElementById(focus)?.setSelectionRange(...selection);
  updatePlayback();
}

function updatePlayback() {
  const soundLabel = document.querySelector('#sound-label');
  if (soundLabel) soundLabel.textContent = playback.enabled && playback.ctx?.state === 'running' ? 'Sound on' : 'Enable sound';
  const round = state.room?.round;
  if (round && state.room.phase === 'stealing') {
    const countdown = document.querySelector('#steal-countdown');
    if (countdown) countdown.textContent = `${Math.max(0, Math.min(20, Math.ceil((round.stealUntil - now()) / 1000)))}s`;
    const steal = document.querySelector('#steal-button');
    if (steal) steal.disabled = !canSteal() || !state.connected || state.selected === null;
    return;
  }
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
      const oldPhase = state.room?.phase;
      state.connected = true;
      state.error = '';
      state.room = message.room;
      if (state.bestRTT === Infinity) state.offset = message.serverTime - Date.now();
      if (oldRound !== state.room.round?.id) { state.selected = null; state.artistGuess = ''; state.titleGuess = ''; }
      if (oldPhase !== state.room.phase) state.selected = null;
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
  dialog.innerHTML = `<div class="dialog-header"><div><span class="eyebrow">THE RULES OF THE RECORD</span><h2 id="dialog-title">Good ears. Better guesses.</h2></div><button class="icon-button" data-action="close-dialog" aria-label="Close">${icon('close')}</button></div><ol class="rules"><li><strong>Pick your company.</strong>Choose blue, red, green, or yellow in the lobby to join a team. Leave Solo selected to play on your own. Teams share one timeline and take one turn per cycle.</li><li><strong>Start with a little history.</strong>Each team or solo player gets one song card, with its title and release year revealed.</li><li><strong>Listen to the mystery song.</strong>Everyone hears the same clip. On your turn, choose a gap in your timeline, from oldest to newest.</li><li><strong>Lock it in.</strong>Place the song in the right chronological position to keep the card. Same-year songs can go on either side. Any teammate can submit; the first answer counts.</li><li><strong>Build your collection.</strong>The first team or solo player to the target wins. If the library runs out, the most cards wins; ties share the win.</li><li><strong>Earn and spend tokens.</strong>Only the playing team guesses the artist and title, once, with its placement. Both must match at least 80%, ignoring spaces and capitalization. One credited featured artist is enough. Earn one token even if the card is misplaced. Start each game with zero tokens. Solo players use the same rules.</li><li><strong>Pick a new song.</strong>Spend two tokens during your turn to replace the song immediately and keep your turn. You cannot skip when the deck has no replacement.</li><li><strong>Steal before the reveal.</strong>After lock-in, opponents with tokens have 20 seconds to spend one token on another gap in the playing timeline, or pass. If the playing team is wrong, the first correct steal gets the card, sorted into its own timeline. The playing team wins same-year ties. Every placed token is lost, even if the steal fails.</li><li><strong>Keep the session moving.</strong>The host can end a turn for a broken song or disconnected player, without awarding a card or token. Reconnecting preserves your team and cards. Teams keep playing while any member is online; fully offline teams and solo players are skipped between turns.</li></ol><p class="info-note">Demo clips are original synthesized loops with fictional years. They let you try the game mechanics. Upload MP3s with real release years for a music quiz.</p><button class="button button-dark button-full" data-action="close-dialog">Got it. Drop the needle.${icon('play')}</button>`;
  dialog.showModal();
}

async function openLibrary() {
  try { state.library = await request('api/library'); } catch (error) { toast(error.message); return; }
  dialog.dataset.kind = 'library';
  dialog.innerHTML = `<div class="dialog-header"><div><span class="eyebrow">CURATE THE GOOD TIMES</span><h2 id="dialog-title">Your song collection.</h2></div><button class="icon-button" data-action="close-dialog" aria-label="Close library">${icon('close')}</button></div><p class="muted">Add MP3s and the original release year of each recording. Songs stay in this server’s library for your next session.</p><form id="upload-form"><label class="file-drop" id="file-drop">${icon('upload')}<strong id="file-name">Choose an MP3 or drop it here</strong><span>Up to 32 MB · one song at a time</span><input type="file" name="file" accept=".mp3,audio/mpeg" id="mp3-file" required></label><audio id="upload-preview" controls hidden preload="metadata"></audio><div class="form-grid"><label class="field">SONG TITLE<input name="title" id="song-title" maxlength="100" required placeholder="The name of the song"></label><label class="field">ARTIST<input name="artist" id="song-artist" maxlength="100" required placeholder="Who made it?"><span class="field-hint">Use feat., ft., or featuring for guest artists (e.g. Artist feat. Guest).</span></label><label class="field">RELEASE YEAR<input name="year" type="number" min="1800" max="${new Date().getFullYear() + 1}" required placeholder="1998"></label><label class="field">CLIP START (SECONDS)<input name="start" type="number" min="0" max="3600" step="0.1" value="0"><span class="field-hint">We play up to 20 seconds from here.</span></label></div><p class="form-error" id="upload-error" role="alert"></p><button class="button button-dark button-full" type="submit">Add to the collection${icon('plus')}</button></form><div class="library-list-heading"><h3>On the shelf</h3><span id="library-count"></span></div><div id="library-list" class="library-list"></div>`;
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
  if (event.target.id === 'recognition-form') {
    event.preventDefault();
    if (!document.querySelector('#lock-button')?.disabled) send('place', { position: state.selected, artist: state.artistGuess, title: state.titleGuess });
  }
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
  else if (action === 'steal') { if (canSteal() && state.selected !== null) send('steal', { position: state.selected }); }
  else if (['start', 'next', 'reset', 'replay', 'skip', 'discard', 'pass'].includes(action)) { if (action === 'start' || action === 'replay') playback.enable(); send(action); }
});
document.addEventListener('input', event => {
  if (event.target.id === 'guess-artist') state.artistGuess = event.target.value;
  if (event.target.id === 'guess-title') state.titleGuess = event.target.value;
  if (event.target.id === 'volume') playback.setVolume(Number(event.target.value) / 100);
});
document.addEventListener('change', event => {
  if (event.target.name === 'team') send('team', { team: event.target.value });
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
