import { endpoint } from './platform.js';

export class Playback {
  constructor({ token, send, now, changed }) {
    this.token = token;
    this.send = send;
    this.now = now;
    this.changed = changed;
    this.volume = 0.65;
    this.enabled = false;
    this.status = 'off';
    this.round = null;
    this.buffer = null;
    this.loadedID = null;
    this.scheduled = null;
    this.loadSerial = 0;
  }

  context() {
    if (!this.ctx) {
      this.ctx = new AudioContext();
      this.gain = this.ctx.createGain();
      this.gain.gain.value = this.volume;
      this.gain.connect(this.ctx.destination);
      this.ctx.addEventListener('statechange', () => {
        if (this.ctx.state === 'running') this.schedule();
        this.changed();
      });
    }
    return this.ctx;
  }

  async enable() {
    try {
      await this.context().resume();
      this.enabled = this.ctx.state === 'running';
      this.schedule();
      this.changed();
    } catch {
      this.error = 'Audio could not start. Click Enable sound to try again.';
      this.changed();
    }
  }

  setVolume(value) {
    this.volume = value;
    if (this.gain) this.gain.gain.setTargetAtTime(value, this.ctx.currentTime, 0.03);
  }

  stop() {
    if (this.source) { try { this.source.stop(); } catch { /* Already ended. */ } }
    this.source = null;
    this.scheduled = null;
  }

  clear() {
    this.stop();
    this.abort?.abort();
    this.loadSerial++;
    this.round = null;
    this.loadedID = null;
    this.buffer = null;
    this.error = null;
  }

  sync(room) {
    if (room.phase !== 'playing') {
      this.stop();
      this.round = null;
      return;
    }
    const previous = this.round;
    this.round = room.round;
    if (previous?.id !== this.round.id || previous?.generation !== this.round.generation) this.stop();
    if (this.loadedID !== this.round.id) {
      this.load(this.round.id);
    } else if (this.buffer) {
      if (previous?.generation !== this.round.generation || previous?.id !== this.round.id) this.ready();
      this.schedule();
    }
  }

  async load(id) {
    this.abort?.abort();
    this.abort = new AbortController();
    const serial = ++this.loadSerial;
    this.loadedID = id;
    this.buffer = null;
    this.error = null;
    this.status = 'loading';
    this.changed();
    const timeout = setTimeout(() => this.abort?.abort(), 20000);
    try {
      const response = await fetch(endpoint(`api/audio/${id}`), {
        headers: { Authorization: `Bearer ${this.token()}` }, signal: this.abort.signal,
      });
      if (!response.ok) throw new Error('The song could not be downloaded.');
      const data = await response.arrayBuffer();
      if (serial !== this.loadSerial) return;
      const buffer = await this.context().decodeAudioData(data);
      if (serial !== this.loadSerial || this.round?.id !== id) return;
      if (this.round.offset >= buffer.duration) throw new Error('Playback starts after the song ends. Ask the host to skip it.');
      this.buffer = buffer;
      this.status = 'ready';
      this.ready();
      this.schedule();
    } catch (error) {
      if (serial !== this.loadSerial) return;
      this.status = 'error';
      this.error = error.name === 'AbortError' ? 'The song took too long to load. Try again.' : error.message || 'This MP3 could not be decoded.';
    } finally {
      clearTimeout(timeout);
      this.changed();
    }
  }

  ready() {
    if (this.round && this.buffer) this.send('ready', { roundId: this.round.id, generation: this.round.generation });
  }

  schedule() {
    const round = this.round;
    if (!round || !this.buffer || !round.startAt || !this.enabled || this.ctx?.state !== 'running') return;
    const key = `${round.id}:${round.generation}`;
    if (this.scheduled === key) return;
    this.stop();
    const delay = (round.startAt - this.now()) / 1000;
    const elapsed = Math.max(0, -delay);
    const offset = round.offset + elapsed;
    if (offset >= this.buffer.duration) { this.status = 'ended'; this.changed(); return; }
    const source = this.ctx.createBufferSource();
    source.buffer = this.buffer;
    source.connect(this.gain);
    source.onended = () => { if (this.source === source) { this.status = 'ended'; this.changed(); } };
    source.start(this.ctx.currentTime + Math.max(0, delay), offset);
    this.source = source;
    this.scheduled = key;
    this.status = 'playing';
    this.changed();
  }

  retry() { if (this.round) this.load(this.round.id); }
}
