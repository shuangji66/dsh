/**
 * The parts of dsh's native context menu this plugin relies on.
 *
 * dsh 0.2.2 renders the shared popup as `<div role="menu" data-context-menu
 * data-menu-phase="enter">` and each row as `<button role="menuitem">` with an
 * icon slot. The plugin re-wires three of dsh's own rows ("Show in folder",
 * "Open in default application" and "Copy path") instead of adding duplicates,
 * so it has to recognise them:
 *
 *  - the icon is the stable anchor: `<svg data-icon="dsw-icon-…"><use href="#…">`;
 *  - the label is the fallback, localized by dsh in all eight UI languages.
 *
 * Everything here is plain data so `test/native-menu-contract.test.ts` can pin
 * it against the strings dsh actually ships. Keep both lists in sync with
 * `@deepseek-ai/dsh-client-ui-open-in-app` (`menu.reveal`, `menu.finder`,
 * `menu.copyPath`, `menu.copyDirectoryPath`) when dsh adds a language.
 */

/** `IconFolderOpenOutlineRegular`, used by dsh's reveal row. */
export const REVEAL_ICON = 'dsw-icon-folder-open-outline-regular';

/** `IconCopyOutlineRegular`, used by dsh's copy-path row. */
export const COPY_ICON = 'dsw-icon-copy-outline-regular';

/** `menu.reveal` / `menu.finder` in every UI language dsh 0.2.2 ships. */
export const REVEAL_LABELS = [
  'Show in folder',
  'Show in Finder',
  'Reveal in folder',
  '在文件夹显示',
  '在 Finder 中显示',
  '在資料夾顯示',
  'Im Ordner anzeigen',
  'Im Finder anzeigen',
  'Afficher dans le dossier',
  'Afficher dans le Finder',
  'Mostrar en la carpeta',
  'Mostrar en Finder',
  'Mostrar na pasta',
  'Mostrar no Finder',
  'フォルダーに表示',
  'Finder に表示',
  'Показать в папке',
  'Показать в Finder',
  // Legacy spellings kept from earlier dsh releases / zh-TW builds.
  'Reveal in folder',
  '在文件夹中显示',
  '在資料夾中顯示',
  'フォルダーで表示',
  'Открыть папку',
  'Abrir na pasta',
] as const;

/**
 * `menu.copyPath` / `menu.copyDirectoryPath` (0.2.2) and `Copy path` (0.2.1).
 *
 * The file and folder labels differ, so both spellings are listed per language.
 */
export const COPY_LABELS = [
  'Copy path',
  'Copy file path',
  'Copy folder path',
  '复制路径',
  '复制文件路径',
  '复制文件夹路径',
  'パスをコピー',
  'ファイルパスをコピー',
  'フォルダーパスをコピー',
  'Pfad kopieren',
  'Dateipfad kopieren',
  'Ordnerpfad kopieren',
  'Copier le chemin',
  'Copier le chemin du fichier',
  'Copier le chemin du dossier',
  'Copiar ruta',
  'Copiar ruta del archivo',
  'Copiar ruta de la carpeta',
  'Copiar caminho',
  'Copiar caminho do arquivo',
  'Copiar caminho da pasta',
  'Копировать путь',
  'Копировать путь к файлу',
  'Копировать путь к папке',
] as const;

/**
 * `menu.defaultApp` in every UI language dsh 0.2.2 ships.
 *
 * 文件菜单里的「使用默认应用打开」：dsh 只有在宿主机（它自己那台机器）解析出桌面
 * 应用时才给这一行 `run`，否则渲染成 disabled。fnOS 上永远解析不出来（dsh 跑在
 * NAS 上，没有桌面），但飞牛自己的 `openFile` 就是「用默认应用打开」，所以这一行
 * 由插件接管 —— 接管后插件不再重复提供「在飞牛中打开」。
 *
 * 只在**禁用**状态下按标签匹配：一旦 dsh 真的解析出应用（`open.title`，例如
 * “用 Visual Studio Code 打开”），那是它自己的动作，插件不碰。
 */
export const DEFAULT_APP_LABELS = [
  '使用默认应用打开',
  'Open in default application',
  '既定のアプリで開く',
  'Abrir en la aplicación predeterminada',
  'Ouvrir dans l’application par défaut',
  'In der Standardanwendung öffnen',
  'Открыть в приложении по умолчанию',
  'Abrir no aplicativo padrão',
] as const;

export const REVEAL_LABEL_RE = new RegExp(`^(?:${REVEAL_LABELS.join('|')})$`, 'i');
export const COPY_LABEL_RE = new RegExp(`^(?:${COPY_LABELS.join('|')})$`, 'i');
export const DEFAULT_APP_LABEL_RE = new RegExp(`^(?:${DEFAULT_APP_LABELS.join('|')})$`, 'i');

/**
 * Whether one `data-menu-phase` value belongs to the menu the user is using.
 *
 * dsh 0.2.2 phases are `enter` / `dismiss` / `replace` (see the context-menu
 * CSS). Only `enter` is the live popup: a dismissing or replacing menu is
 * fading out with `pointer-events: none` and still carries the outgoing rows.
 * A missing attribute means an older dsh (or a non-contextual menu) without
 * transition phases, which is always live.
 */
export function isLiveMenuPhase(phase: string | null | undefined): boolean {
  return phase === null || phase === undefined || phase === '' || phase === 'enter';
}
