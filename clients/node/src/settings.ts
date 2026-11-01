// The environment this client reads (§9.7.2, §9.7.3). Settings are read
// here, once per connect, and nowhere else.

import { homedir } from 'node:os';
import { join } from 'node:path';

export interface Settings {
  /** ATLAS_URL: the gateway. */
  url?: string;
  /** ATLAS_HOME, else ~/.atlas: the sign-in under state/, the profile cache under cache/. */
  home: string;
  /** ATLAS_TOKEN: a workload's access token, used instead of the store. */
  token?: string;
}

export function readSettings(env: NodeJS.ProcessEnv = process.env): Settings {
  const blank = (v: string | undefined) => (v === undefined || v === '' ? undefined : v);
  return {
    url: blank(env.ATLAS_URL),
    home: blank(env.ATLAS_HOME) ?? join(homedir(), '.atlas'),
    token: blank(env.ATLAS_TOKEN),
  };
}
