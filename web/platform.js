// The game only needs an authenticated player and a room. Discord-specific
// initialization stays here, leaving the browser version independent of its SDK.
export const embedded = new URLSearchParams(location.search).has('frame_id');

export function endpoint(path) {
  const prefix = embedded && location.hostname.endsWith('.discordsays.com') ? '/.proxy/' : '/';
  return new URL(prefix + path.replace(/^\//, ''), location.origin).toString();
}

export async function discordEntry(config, request) {
  if (!config.discordEnabled) {
    throw new Error('Discord setup is incomplete. Add the application credentials and allowed user IDs to the server’s .env file, then restart it.');
  }
  const { DiscordSDK } = await import('./vendor/discord-sdk.js');
  const sdk = new DiscordSDK(config.discordClientId, { disableConsoleLogOverride: true });
  await Promise.race([
    sdk.ready(),
    new Promise((_, reject) => setTimeout(() => reject(new Error('Discord did not respond. Close and reopen the Activity.')), 15000)),
  ]);
  const { code } = await sdk.commands.authorize({
    client_id: config.discordClientId,
    response_type: 'code',
    state: crypto.randomUUID(),
    prompt: 'none',
    scope: ['identify'],
  });
  const credentials = await request('api/discord/token', { code });
  await sdk.commands.authenticate({ access_token: credentials.access_token });
  return request('api/discord/join', { ticket: credentials.ticket, instanceId: sdk.instanceId });
}
