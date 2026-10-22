/**
 * Build both halves of the plugin.
 *
 *   src/  →  lib/            host half (tsc, ESM + .d.ts)
 *   src/client/index.ts  →  lib/client.js  (esbuild, dsh lazy CJS factory)
 *
 * dsh's client module system loads a browser bundle as a lazy CJS factory:
 *
 *   window.__ModuleLoader__.load({ id, factory: (require) => module.exports })
 *
 * The official fnOS SDK is bundled into the factory. Only `react` stays
 * external (it comes from the loader module table), so the installed package
 * has no runtime dependency.
 */
import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const libDir = path.join(root, 'lib');
const entry = path.join(root, 'src/client/index.ts');
const outFile = path.join(libDir, 'client.js');

/** Run the repository's own TypeScript compiler; fall back to PATH. */
function emitHost() {
  const localTsc = path.join(root, 'node_modules/typescript/bin/tsc');
  const useLocal = existsSync(localTsc);
  const command = useLocal ? process.execPath : 'tsc';
  const args = useLocal ? [localTsc, '-p', 'tsconfig.json'] : ['-p', 'tsconfig.json'];
  const result = spawnSync(command, args, { cwd: root, stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`tsc exited with ${result.status}`);
}

/**
 * tsc rewrites `.ts` import extensions in emitted JavaScript but leaves them in
 * `.d.ts`, where a consumer without `allowImportingTsExtensions` cannot resolve
 * them. Rewrite the relative specifiers to their emitted `.js` names.
 */
async function rewriteDeclarationExtensions() {
  const entries = await readdir(libDir, { recursive: true, withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.endsWith('.d.ts')) continue;
    const file = path.join(entry.parentPath, entry.name);
    const source = await readFile(file, 'utf8');
    const rewritten = source.replace(
      /(\bfrom\s+['"])(\.{1,2}\/[^'"]+)\.ts(['"])/g,
      '$1$2.js$3',
    );
    if (rewritten !== source) await writeFile(file, rewritten, 'utf8');
    if (/(\bfrom\s+['"])\.{1,2}\/[^'"]+\.ts(['"])/.test(rewritten)) {
      throw new Error(`declaration still imports a .ts path: ${path.relative(root, file)}`);
    }
  }
}

async function bundleClient() {
  const result = await build({
    entryPoints: [entry],
    bundle: true,
    write: false,
    format: 'cjs',
    platform: 'browser',
    target: ['es2020'],
    charset: 'utf8',
    legalComments: 'none',
    // Classic JSX: the emitted calls are React.createElement, the only thing a
    // loader that predates `react/jsx-runtime` can run.
    jsx: 'transform',
    jsxFactory: 'React.createElement',
    jsxFragment: 'React.Fragment',
    external: ['react'],
    logLevel: 'info',
  });

  const bundled = result.outputFiles[0].text;
  const banner = `(function () {\n  if (!window.__ModuleLoader__ || typeof window.__ModuleLoader__.load !== "function") {\n    console.warn("[dsh-fnos-advanced] client module loader is not available");\n    return;\n  }\n  window.__ModuleLoader__.load({\n  id: "dsh-fnos-advanced",\n  factory: (require) => {\n    var module = { exports: {} };\n    var exports = module.exports;\n`;
  const footer = `\n    return module.exports;\n  },\n  });\n})();\n`;
  const output = `${banner}${bundled}${footer}`;

  await mkdir(libDir, { recursive: true });
  await writeFile(outFile, output, 'utf8');

  // Cheap post-build guards: the bundle must stay in the loader contract and
  // must not ask the runtime for anything but the single external.
  const check = await readFile(outFile, 'utf8');
  if (!check.includes('window.__ModuleLoader__.load(')) {
    throw new Error('client bundle is not wrapped in the dsh module loader');
  }
  if (/require\(["']@trimjs\/web-app["']\)/.test(check)) {
    throw new Error('client bundle still requires @trimjs/web-app at runtime');
  }
  if (/require\(["']react\/jsx-runtime["']\)/.test(check)) {
    throw new Error('client bundle requires react/jsx-runtime, which older hosts do not provide');
  }
  const requires = new Set([...check.matchAll(/require\(["']([^"']+)["']\)/g)].map((match) => match[1]));
  for (const id of requires) {
    if (id !== 'react') throw new Error(`client bundle requires an unexpected external: ${id}`);
  }

  console.log(`built ${path.relative(root, outFile)} (${Buffer.byteLength(output)} bytes)`);
}

await rm(libDir, { recursive: true, force: true });
emitHost();
await rewriteDeclarationExtensions();
await bundleClient();
