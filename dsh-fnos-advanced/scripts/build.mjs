/**
 * Build the dsh client bundle.
 *
 * dsh's client module system loads a browser bundle as a lazy CJS factory:
 *
 *   window.__ModuleLoader__.load({ id, factory: (require) => module.exports })
 *
 * The official fnOS SDK is bundled into the factory. Nothing is left as an
 * external, so the installed package has no runtime dependency.
 */
import { build } from 'esbuild';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const entry = path.join(root, 'src/client/index.js');
const outDir = path.join(root, 'lib');
const outFile = path.join(outDir, 'client.js');

const result = await build({
  entryPoints: [entry],
  bundle: true,
  write: false,
  format: 'cjs',
  platform: 'browser',
  target: ['es2020'],
  charset: 'utf8',
  legalComments: 'none',
  external: ['react'],
  logLevel: 'info',
});

const bundled = result.outputFiles[0].text;
const banner = `(function () {\n  if (!window.__ModuleLoader__ || typeof window.__ModuleLoader__.load !== "function") {\n    console.warn("[dsh-fnos-advanced] client module loader is not available");\n    return;\n  }\n  window.__ModuleLoader__.load({\n  id: "dsh-fnos-advanced",\n  factory: (require) => {\n    var module = { exports: {} };\n    var exports = module.exports;\n`;
const footer = `\n    return module.exports;\n  },\n  });\n})();\n`;
const output = `${banner}${bundled}${footer}`;

await mkdir(outDir, { recursive: true });
await writeFile(outFile, output, 'utf8');

// A cheap post-build guard: the bundle must not request the SDK at runtime.
const check = await readFile(outFile, 'utf8');
if (/require\(["']@trimjs\/web-app["']\)/.test(check)) {
  throw new Error('client bundle still requires @trimjs/web-app at runtime');
}

console.log(`built ${path.relative(root, outFile)} (${Buffer.byteLength(output)} bytes)`);
