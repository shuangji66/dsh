import assert from 'node:assert/strict';
import test from 'node:test';
import {
  COPY_ICON,
  COPY_LABELS,
  COPY_LABEL_RE,
  DEFAULT_APP_LABELS,
  DEFAULT_APP_LABEL_RE,
  REVEAL_ICON,
  REVEAL_LABELS,
  REVEAL_LABEL_RE,
  isLiveMenuPhase,
} from '../src/client/native-menu-contract.ts';

/**
 * The labels below are copied verbatim from dsh 0.2.2's `open-in-app` locale
 * dictionaries (`menu.reveal`, `menu.finder`, `menu.copyPath`,
 * `menu.copyDirectoryPath`, `menu.defaultApp`). They are the fallback anchor
 * when the icon is not enough, so a dsh rename must fail here instead of
 * silently leaving the row un-adapted in one language.
 */
const DSH_022_REVEAL = [
  'Show in folder', 'Show in Finder',
  '在文件夹显示', '在 Finder 中显示',
  'Im Ordner anzeigen', 'Im Finder anzeigen',
  'Afficher dans le dossier', 'Afficher dans le Finder',
  'Mostrar en la carpeta', 'Mostrar en Finder',
  'Mostrar na pasta', 'Mostrar no Finder',
  'フォルダーに表示', 'Finder に表示',
  'Показать в папке', 'Показать в Finder',
];

/** `menu.defaultApp`, verbatim from the same dictionaries (fr uses U+2019). */
const DSH_022_DEFAULT_APP = [
  '使用默认应用打开',
  'Open in default application',
  '既定のアプリで開く',
  'Abrir en la aplicación predeterminada',
  'Ouvrir dans l’application par défaut',
  'In der Standardanwendung öffnen',
  'Открыть в приложении по умолчанию',
  'Abrir no aplicativo padrão',
];

const DSH_022_COPY = [
  'Copy path', 'Copy file path', 'Copy folder path',
  '复制路径', '复制文件路径', '复制文件夹路径',
  'パスをコピー', 'ファイルパスをコピー', 'フォルダーパスをコピー',
  'Pfad kopieren', 'Dateipfad kopieren', 'Ordnerpfad kopieren',
  'Copier le chemin', 'Copier le chemin du fichier', 'Copier le chemin du dossier',
  'Copiar ruta', 'Copiar ruta del archivo', 'Copiar ruta de la carpeta',
  'Copiar caminho', 'Copiar caminho do arquivo', 'Copiar caminho da pasta',
  'Копировать путь', 'Копировать путь к файлу', 'Копировать путь к папке',
];

test('every dsh 0.2.2 reveal label is recognised', () => {
  for (const label of DSH_022_REVEAL) {
    assert.match(label, REVEAL_LABEL_RE, `reveal label not matched: ${label}`);
  }
  // Older dsh releases / zh-TW builds spelled the same row differently; those
  // stay as compatibility anchors, so the reverse check allows them explicitly.
  const legacy = [
    'Reveal in folder',
    '在文件夹中显示',
    '在資料夾顯示',
    '在資料夾中顯示',
    'フォルダーで表示',
    'Открыть папку',
    'Abrir na pasta',
  ];
  for (const label of REVEAL_LABELS) {
    assert.ok(
      DSH_022_REVEAL.includes(label) || legacy.includes(label),
      `unexpected reveal label kept: ${label}`,
    );
  }
});

test('every dsh 0.2.2 copy-path label is recognised', () => {
  for (const label of DSH_022_COPY) {
    assert.match(label, COPY_LABEL_RE, `copy label not matched: ${label}`);
  }
  for (const label of COPY_LABELS) {
    assert.ok(DSH_022_COPY.includes(label), `unexpected copy label kept: ${label}`);
  }
});

test('every dsh 0.2.2 default-app label is recognised', () => {
  for (const label of DSH_022_DEFAULT_APP) {
    assert.match(label, DEFAULT_APP_LABEL_RE, `default-app label not matched: ${label}`);
  }
  for (const label of DEFAULT_APP_LABELS) {
    assert.ok(DSH_022_DEFAULT_APP.includes(label), `unexpected default-app label kept: ${label}`);
  }
});

test('the label fallback does not match unrelated rows', () => {
  for (const label of ['Open in sidebar', 'Copy', 'Copy image', '在侧边栏打开', '打开', '']) {
    assert.doesNotMatch(label, REVEAL_LABEL_RE);
    assert.doesNotMatch(label, COPY_LABEL_RE);
    assert.doesNotMatch(label, DEFAULT_APP_LABEL_RE);
  }
  // A resolved desktop application renames the row (`open.title`); that one is
  // dsh's own action and must never be taken over.
  for (const label of ['Open with Visual Studio Code', '用 Visual Studio Code 打开']) {
    assert.doesNotMatch(label, DEFAULT_APP_LABEL_RE);
  }
});

test('the icon anchors are the ids dsh 0.2.2 renders into the menu rows', () => {
  // `<svg data-icon="dsw-icon-…"><use href="#dsw-icon-…">` (contextMenu.icon slot).
  assert.equal(REVEAL_ICON, 'dsw-icon-folder-open-outline-regular');
  assert.equal(COPY_ICON, 'dsw-icon-copy-outline-regular');
});

test('only the live menu phase is augmented', () => {
  assert.equal(isLiveMenuPhase(null), true, 'older dsh has no phase');
  assert.equal(isLiveMenuPhase(undefined), true);
  assert.equal(isLiveMenuPhase(''), true);
  assert.equal(isLiveMenuPhase('enter'), true);
  assert.equal(isLiveMenuPhase('dismiss'), false);
  assert.equal(isLiveMenuPhase('replace'), false);
});
