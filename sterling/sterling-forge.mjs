#!/usr/bin/env node
//
// Launch a locally-built Cope and Drag (Sterling) web UI from this repo's
// flake. The `sterling-forge` wrapper can also run a Forge model; the
// `sterling-alloy` wrapper serves the Alloy build only.
//
// The script intentionally uses only Node's standard library.

import fs from 'node:fs';
import fsp from 'node:fs/promises';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const APP_NAME = process.env.STERLING_APP_NAME || 'sterling-forge';
const ALLOW_MODEL = process.env.STERLING_ALLOW_MODEL !== '0';
const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const INSTALLED_STATIC_DIR = path.resolve(
  SCRIPT_DIR,
  '..',
  'share',
  APP_NAME
);
const SOURCE_STATIC_DIR = path.resolve(SCRIPT_DIR, '..', 'dist');

function defaultStaticDir() {
  const configured =
    process.env.STERLING_DIST || process.env.STERLING_FORGE_DIST;
  if (configured) return configured;
  if (fs.existsSync(path.join(INSTALLED_STATIC_DIR, 'index.html'))) {
    return INSTALLED_STATIC_DIR;
  }
  if (fs.existsSync(path.join(SOURCE_STATIC_DIR, 'index.html'))) {
    return SOURCE_STATIC_DIR;
  }
  return INSTALLED_STATIC_DIR;
}

const DEFAULT_TIMEOUT_SECONDS = 120;

const FORGE_OPTIONS = `  -p, --provider-port PORT   Port for Forge's Sterling provider (default: random)
      --timeout SECONDS      How long to wait for Forge (default: 120)
      --racket PATH          Racket executable to use (default: racket)
      --serve-only           Serve the UI without starting a Forge model
`;
const FORGE_ENVIRONMENT = `  STERLING_MODEL             Model to run when no positional argument is given
  STERLING_PROVIDER_PORT     Same as --provider-port
  STERLING_TIMEOUT           Same as --timeout
  STERLING_RACKET            Same as --racket
`;

const HELP = `Usage: ${APP_NAME} [options]${
  ALLOW_MODEL ? ' MODEL.frg [TAG]' : ''
} [-- RACKET_ARGS...]

Serve the locally-built Cope and Drag (Sterling) UI. ${
  ALLOW_MODEL
    ? 'When a MODEL is supplied, start Forge in provider-only (`serve`) mode and print the visualizer URL once its provider port is ready. Use --serve-only to skip Forge.'
    : 'This launcher serves the UI only; the Alloy provider connects using the WebSocket endpoint embedded in the build.'
}
This does not download a release and never opens a browser. The URL is printed
to stdout; diagnostics go to stderr.

${
  ALLOW_MODEL
    ? 'An optional second positional argument (the release tag used by open-sterling-simple.sh) is accepted for compatibility and ignored, because the UI is always the locally-built one.'
    : ''
}

Options:
${ALLOW_MODEL ? FORGE_OPTIONS : ''}      --static-port PORT     Port for the local static file server (default: random)
      --host HOST            Address to bind (default: 127.0.0.1)
      --static-dir DIR       Directory to serve (default: built-in dist)
      --ready-file FILE      Write the visualizer URL to FILE when ready
      --json                 Print one JSON object on stdout instead of the URL
  -q, --quiet                Suppress informational messages on stderr
  -v, --verbose              Log HTTP requests and child process output
  -h, --help                 Show this help
  -V, --version              Show the version

Environment:
  STERLING_DIST              Same as --static-dir (legacy: STERLING_FORGE_DIST)
${ALLOW_MODEL ? FORGE_ENVIRONMENT : ''}  STERLING_STATIC_PORT       Same as --static-port
  STERLING_HOST              Same as --host
  STERLING_READY_FILE        Same as --ready-file
  STERLING_APP_NAME          Name used in help and diagnostics

Exit status:
  0 on normal shutdown, 1 on setup/timeout errors, and 128+signal when
  terminated by a signal.
`;

const MIME_TYPES = new Map([
  ['.css', 'text/css; charset=utf-8'],
  ['.gif', 'image/gif'],
  ['.html', 'text/html; charset=utf-8'],
  ['.ico', 'image/x-icon'],
  ['.jpeg', 'image/jpeg'],
  ['.jpg', 'image/jpeg'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.map', 'application/json; charset=utf-8'],
  ['.mjs', 'text/javascript; charset=utf-8'],
  ['.png', 'image/png'],
  ['.svg', 'image/svg+xml'],
  ['.ttf', 'font/ttf'],
  ['.txt', 'text/plain; charset=utf-8'],
  ['.wasm', 'application/wasm'],
  ['.webmanifest', 'application/manifest+json; charset=utf-8'],
  ['.woff', 'font/woff'],
  ['.woff2', 'font/woff2'],
  ['.xml', 'application/xml; charset=utf-8']
]);

class UsageError extends Error {}
class SetupError extends Error {}

function envPort(name) {
  const value = process.env[name];
  if (value === undefined || value === '') return null;
  const port = Number(value);
  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new UsageError(`${name} must be a port number between 0 and 65535`);
  }
  return port;
}

function envNumber(name, fallback) {
  const value = process.env[name];
  if (value === undefined || value === '') return fallback;
  const number = Number(value);
  if (!Number.isFinite(number) || number <= 0) {
    throw new UsageError(`${name} must be a positive number`);
  }
  return number;
}

function parsePort(value, option) {
  const port = Number(value);
  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new UsageError(`${option} must be a port number between 0 and 65535`);
  }
  return port;
}

function parseArgs(argv) {
  const opts = {
    host: process.env.STERLING_HOST || '127.0.0.1',
    providerPort: envPort('STERLING_PROVIDER_PORT'),
    staticPort: envPort('STERLING_STATIC_PORT'),
    timeoutSeconds: envNumber('STERLING_TIMEOUT', DEFAULT_TIMEOUT_SECONDS),
    staticDir: defaultStaticDir(),
    racket: process.env.STERLING_RACKET || 'racket',
    readyFile: process.env.STERLING_READY_FILE || null,
    model:
      process.env.STERLING_MODEL || process.env.STERLING_FORGE_MODEL || null,
    json: false,
    quiet: false,
    verbose: false,
    serveOnly:
      !ALLOW_MODEL ||
      process.env.STERLING_SERVE_ONLY === '1' ||
      process.env.STERLING_SERVE_ONLY === 'true',
    extraRacketArgs: []
  };

  const positionals = [];
  let afterDoubleDash = false;

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];

    if (afterDoubleDash) {
      opts.extraRacketArgs.push(arg);
      continue;
    }
    if (arg === '--') {
      afterDoubleDash = true;
      continue;
    }
    if (arg === '-h' || arg === '--help') return { help: true };
    if (arg === '-V' || arg === '--version') return { version: true };
    if (arg === '--json') {
      opts.json = true;
      continue;
    }
    if (arg === '-q' || arg === '--quiet') {
      opts.quiet = true;
      continue;
    }
    if (arg === '-v' || arg === '--verbose') {
      opts.verbose = true;
      continue;
    }
    if (arg === '--serve-only' || arg === '--static-only') {
      opts.serveOnly = true;
      continue;
    }

    if (arg.startsWith('-')) {
      const equals = arg.indexOf('=');
      const name = equals >= 0 ? arg.slice(0, equals) : arg;
      let value = equals >= 0 ? arg.slice(equals + 1) : undefined;

      const takeValue = () => {
        if (value !== undefined) return value;
        i += 1;
        if (i >= argv.length) throw new UsageError(`${name} requires a value`);
        return argv[i];
      };

      switch (name) {
        case '-p':
        case '--provider-port':
          opts.providerPort = parsePort(takeValue(), name);
          break;
        case '--static-port':
          opts.staticPort = parsePort(takeValue(), name);
          break;
        case '--host':
          opts.host = takeValue();
          break;
        case '--static-dir':
          opts.staticDir = takeValue();
          break;
        case '--timeout':
          opts.timeoutSeconds = Number(takeValue());
          if (!Number.isFinite(opts.timeoutSeconds) || opts.timeoutSeconds <= 0) {
            throw new UsageError('--timeout must be a positive number of seconds');
          }
          break;
        case '--ready-file':
          opts.readyFile = takeValue();
          break;
        case '--racket':
          opts.racket = takeValue();
          break;
        case '--model':
          opts.model = takeValue();
          break;
        default:
          throw new UsageError(`unknown option: ${name}`);
      }
      continue;
    }

    positionals.push(arg);
  }

  if (positionals.length > 2) {
    throw new UsageError(
      `expected a model and optional legacy tag, got: ${positionals.join(' ')}`
    );
  }
  if (positionals.length >= 1) {
    if (opts.model) {
      throw new UsageError('model specified both positionally and with --model');
    }
    opts.model = positionals[0];
  }
  if (positionals.length === 2) {
    opts.legacyTag = positionals[1];
  }

  if (!opts.serveOnly && !opts.model) {
    throw new UsageError('missing MODEL.frg (or use --serve-only)');
  }
  if (opts.serveOnly && opts.model) {
    throw new UsageError(
      ALLOW_MODEL
        ? '--serve-only cannot be combined with a model'
        : `${APP_NAME} serves the UI only and does not accept a model`
    );
  }
  opts.legacyTag = opts.legacyTag || null;

  return opts;
}

function log(opts, message) {
  if (!opts.quiet) process.stderr.write(`${APP_NAME}: ${message}\n`);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function listen(server, host, port) {
  return new Promise((resolve, reject) => {
    const onError = (error) => {
      server.off('listening', onListening);
      reject(error);
    };
    const onListening = () => {
      server.off('error', onError);
      resolve(server.address().port);
    };
    server.once('error', onError);
    server.once('listening', onListening);
    server.listen(port, host);
  });
}

function closeServer(server) {
  if (!server) return Promise.resolve();
  return new Promise((resolve) => server.close(() => resolve()));
}

function createStaticServer(root, opts) {
  const indexFile = path.join(root, 'index.html');

  return http.createServer(async (request, response) => {
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      response.writeHead(405, { Allow: 'GET, HEAD' });
      response.end('Method Not Allowed\n');
      return;
    }

    let pathname;
    try {
      pathname = decodeURIComponent(
        new URL(request.url || '/', 'http://localhost').pathname
      );
    } catch {
      response.writeHead(400);
      response.end('Bad Request\n');
      return;
    }

    if (pathname.endsWith('/')) pathname += 'index.html';
    if (pathname === '') pathname = '/index.html';

    let filePath = path.resolve(root, `.${pathname}`);
    const relative = path.relative(root, filePath);
    if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
      response.writeHead(403);
      response.end('Forbidden\n');
      return;
    }

    let stat;
    try {
      stat = await fsp.stat(filePath);
      if (stat.isDirectory()) {
        filePath = path.join(filePath, 'index.html');
        stat = await fsp.stat(filePath);
      }
    } catch {
      // A missing asset should be a 404, but a missing root should fall back
      // to index.html for extensionless client-side routes.
      if (!path.extname(pathname)) {
        filePath = indexFile;
        try {
          stat = await fsp.stat(filePath);
        } catch {
          stat = null;
        }
      } else {
        stat = null;
      }
    }

    if (!stat || !stat.isFile()) {
      response.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
      response.end('Not Found\n');
      return;
    }

    const contentType =
      MIME_TYPES.get(path.extname(filePath).toLowerCase()) ||
      'application/octet-stream';

    response.writeHead(200, {
      'Content-Type': contentType,
      'Content-Length': stat.size,
      'Cache-Control': 'no-cache',
      'X-Content-Type-Options': 'nosniff'
    });

    if (opts.verbose) {
      process.stderr.write(
        `${APP_NAME}: ${request.method} ${pathname} -> ${path.relative(root, filePath)}\n`
      );
    }

    if (request.method === 'HEAD') {
      response.end();
      return;
    }

    const stream = fs.createReadStream(filePath);
    stream.on('error', () => response.destroy());
    stream.pipe(response);
  });
}

async function getFreePort(host) {
  const server = net.createServer();
  const port = await listen(server, host, 0);
  await closeServer(server);
  return port;
}

function canConnect(host, port, timeoutMs = 500) {
  return new Promise((resolve) => {
    const socket = net.createConnection({ host, port });
    let settled = false;

    const finish = (value) => {
      if (settled) return;
      settled = true;
      socket.destroy();
      resolve(value);
    };

    socket.setTimeout(timeoutMs);
    socket.once('connect', () => finish(true));
    socket.once('timeout', () => finish(false));
    socket.once('error', () => finish(false));
  });
}

async function waitForProvider(child, host, port, timeoutMs) {
  const deadline = Date.now() + timeoutMs;

  while (Date.now() < deadline) {
    if (child.spawnError) {
      throw new SetupError(
        `failed to start ${child.spawnFileName}: ${child.spawnError.message}`
      );
    }
    if (child.exitCode !== null || child.signalCode !== null) {
      const detail =
        child.signalCode !== null
          ? `signal ${child.signalCode}`
          : `exit code ${child.exitCode}`;
      throw new SetupError(`Forge exited before Sterling came up (${detail})`);
    }
    if (await canConnect(host, port)) return;
    await sleep(400);
  }

  throw new SetupError(
    `timed out after ${Math.round(timeoutMs / 1000)}s waiting for Forge on ` +
      `${host}:${port}`
  );
}

function spawnForge(opts, providerPort) {
  const args = [
    opts.model,
    '-O',
    'run_sterling',
    'serve',
    '-O',
    'sterling_port',
    String(providerPort),
    ...opts.extraRacketArgs
  ];

  // Interactive terminals can press Enter to stop Forge (its documented
  // behaviour). Non-interactive callers get a pipe that stays open until they
  // signal us, so Forge does not immediately see EOF and exit.
  const stdin = process.stdin.isTTY ? 'inherit' : 'pipe';
  const child = spawn(opts.racket, args, {
    stdio: [stdin, 'pipe', 'pipe'],
    env: process.env
  });

  child.stdout.on('data', (chunk) => process.stderr.write(chunk));
  child.stderr.on('data', (chunk) => process.stderr.write(chunk));

  child.spawnFileName = opts.racket;
  child.spawnError = null;
  child.on('error', (error) => {
    child.spawnError = error;
    if (opts.verbose) {
      process.stderr.write(
        `${APP_NAME}: failed to start ${opts.racket}: ${error.message}\n`
      );
    }
  });

  return child;
}

async function writeReadyFile(file, contents) {
  if (!file) return;
  const directory = path.dirname(path.resolve(file));
  await fsp.mkdir(directory, { recursive: true });
  await fsp.writeFile(file, `${contents}\n`);
}

function emitReady(opts, info) {
  const payload = opts.json
    ? JSON.stringify({
        url: info.url,
        host: opts.host,
        staticPort: info.staticPort,
        providerPort: info.providerPort,
        model: opts.model,
        staticDir: info.staticDir
      })
    : info.url;

  process.stdout.write(`${payload}\n`);

  if (!opts.quiet) {
    const provider =
      info.providerPort === null ? '' : ` (provider port ${info.providerPort})`;
    process.stderr.write(
      `${APP_NAME}: serving ${info.staticDir}${provider}\n` +
        `${APP_NAME}: open ${info.url}\n`
    );
  }

  return writeReadyFile(opts.readyFile, info.url);
}

async function main() {
  const parsed = parseArgs(process.argv.slice(2));
  if (parsed.help) {
    process.stdout.write(HELP);
    return 0;
  }
  if (parsed.version) {
    const version =
      process.env.STERLING_VERSION ||
      process.env.STERLING_FORGE_VERSION ||
      'unknown';
    process.stdout.write(`${APP_NAME} ${version}\n`);
    return 0;
  }

  const opts = parsed;
  if (opts.legacyTag) {
    log(
      opts,
      `ignoring release tag ${opts.legacyTag}; serving the locally-built UI`
    );
  }
  const staticDir = path.resolve(opts.staticDir);
  try {
    await fsp.access(path.join(staticDir, 'index.html'), fs.constants.R_OK);
  } catch {
    throw new SetupError(
      `no index.html in static directory ${staticDir}; ` +
        `build the project with \`nix build .#${APP_NAME}\` or pass --static-dir`
    );
  }

  const server = createStaticServer(staticDir, opts);
  let staticPort;
  try {
    staticPort = await listen(server, opts.host, opts.staticPort ?? 0);
  } catch (error) {
    throw new SetupError(
      `could not listen on ${opts.host}:${opts.staticPort ?? 0}: ${error.message}`
    );
  }

  const cleanup = async () => {
    await closeServer(server);
  };

  if (opts.serveOnly) {
    const url = `http://${opts.host}:${staticPort}/`;
    await emitReady(opts, {
      url,
      staticPort,
      providerPort: null,
      staticDir
    });
    await new Promise((resolve) => {
      const stop = () => resolve();
      process.once('SIGINT', stop);
      process.once('SIGTERM', stop);
    });
    await cleanup();
    return 0;
  }

  const providerPort =
    opts.providerPort && opts.providerPort > 0
      ? opts.providerPort
      : await getFreePort(opts.host);

  const child = spawnForge(opts, providerPort);
  const childExit = new Promise((resolve) => {
    child.once('exit', (code, signal) => resolve({ code, signal }));
  });

  let shuttingDown = false;
  const terminate = async (signal) => {
    if (shuttingDown) return;
    shuttingDown = true;
    if (child.exitCode === null && child.signalCode === null) {
      child.kill('SIGTERM');
      // Give Forge a moment to shut down, then make sure it is gone.
      const timer = setTimeout(() => {
        if (child.exitCode === null && child.signalCode === null) {
          child.kill('SIGKILL');
        }
      }, 3000);
      await Promise.race([
        childExit,
        new Promise((resolve) => setTimeout(resolve, 3500))
      ]);
      clearTimeout(timer);
    }
    await cleanup();
    const signalNumber = signal === 'SIGINT' ? 2 : 15;
    process.exit(128 + signalNumber);
  };

  process.once('SIGINT', () => void terminate('SIGINT'));
  process.once('SIGTERM', () => void terminate('SIGTERM'));

  try {
    await waitForProvider(
      child,
      opts.host,
      providerPort,
      opts.timeoutSeconds * 1000
    );
  } catch (error) {
    if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM');
    await cleanup();
    throw error;
  }

  const url = `http://${opts.host}:${staticPort}/?${providerPort}`;
  try {
    await emitReady(opts, {
      url,
      staticPort,
      providerPort,
      staticDir
    });
  } catch (error) {
    if (child.exitCode === null && child.signalCode === null) {
      child.kill('SIGTERM');
    }
    await cleanup();
    throw error;
  }

  const result = await childExit;
  await cleanup();

  if (result.signal) {
    const signalNumbers = { SIGINT: 2, SIGTERM: 15, SIGHUP: 1, SIGKILL: 9 };
    return 128 + (signalNumbers[result.signal] || 0);
  }
  return result.code ?? 0;
}

main()
  .then((code) => {
    process.exitCode = code;
  })
  .catch((error) => {
    if (error instanceof UsageError) {
      process.stderr.write(`${APP_NAME}: ${error.message}\n\n${HELP}`);
      process.exitCode = 2;
      return;
    }
    if (error instanceof SetupError) {
      process.stderr.write(`${APP_NAME}: ${error.message}\n`);
      process.exitCode = 1;
      return;
    }
    process.stderr.write(
      `${APP_NAME}: ${error && error.stack ? error.stack : error}\n`
    );
    process.exitCode = 1;
  });
