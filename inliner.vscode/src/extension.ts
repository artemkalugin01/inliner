import * as childProcess from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as readline from 'readline';
import * as vscode from 'vscode';

const MESSAGE_PREFIX = 'INLINER-MESSAGE ';
const ACCEPT_COMMAND = 'inliner.internal.accept';

type CoreMessage =
  | { kind: 'connection_status'; is_connected: boolean; status_text?: string | null }
  | { kind: 'response'; stateId: string; items: Array<{ kind: string; text?: string; verify?: string }> }
  | { kind: 'health_response'; [key: string]: unknown }
  | { kind: 'error'; message: string };

type PendingCompletion = {
  resolve: (value: string) => void;
  reject: (reason: Error) => void;
  timer: NodeJS.Timeout;
};

type AcceptedSuggestion = {
  stateId: string;
  path: string;
  text: string;
};

class CoreClient implements vscode.Disposable {
  private process?: childProcess.ChildProcessWithoutNullStreams;
  private output = vscode.window.createOutputChannel('Inliner');
  private pending = new Map<string, PendingCompletion>();
  private pendingHealth?: { resolve: (value: Record<string, unknown>) => void; reject: (reason: Error) => void; timer: NodeJS.Timeout };
  private seq = 0;
  private starting?: Promise<void>;

  constructor(private readonly context: vscode.ExtensionContext) {}

  isRunning(): boolean {
    return this.process !== undefined && !this.process.killed;
  }

  async start(): Promise<void> {
    if (this.isRunning()) {
      return;
    }
    if (this.starting) {
      return this.starting;
    }

    this.starting = this.startProcess();
    try {
      await this.starting;
    } finally {
      this.starting = undefined;
    }
  }

  stop(): void {
    if (!this.process) {
      return;
    }
    this.send({ kind: 'shutdown' });
    this.process.kill();
    this.process = undefined;
    this.rejectAll(new Error('inliner-core stopped'));
  }

  async complete(document: vscode.TextDocument, position: vscode.Position): Promise<{ stateId: string; text: string } | undefined> {
    await this.ensureStartedForDocument(document);
    if (!this.process) {
      return undefined;
    }

    const stateId = String(++this.seq);
    const content = document.getText();
    const filePath = document.uri.fsPath;
    const offset = byteOffsetAt(document, position, content);
    const timeoutMs = config().get<number>('requestTimeoutMs', 15000);

    const promise = new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(stateId);
        reject(new Error('inliner-core completion timed out'));
      }, timeoutMs);
      this.pending.set(stateId, { resolve, reject, timer });
    });

    this.send({
      kind: 'state_update',
      newId: stateId,
      updates: [
        { kind: 'file_update', path: filePath, content },
        { kind: 'cursor_update', path: filePath, offset },
      ],
    });

    const text = await promise;
    if (text === '') {
      return undefined;
    }
    return { stateId, text };
  }

  accept(suggestion: AcceptedSuggestion): void {
    if (!this.process || suggestion.text === '') {
      return;
    }
    this.send({ kind: 'accept_update', stateId: suggestion.stateId, path: suggestion.path, text: suggestion.text });
  }

  async health(): Promise<Record<string, unknown>> {
    await this.start();
    const timeoutMs = config().get<number>('requestTimeoutMs', 15000);
    const promise = new Promise<Record<string, unknown>>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingHealth = undefined;
        reject(new Error('inliner-core health request timed out'));
      }, timeoutMs);
      this.pendingHealth = { resolve, reject, timer };
    });
    this.send({ kind: 'health_request' });
    return promise;
  }

  showOutput(): void {
    this.output.show();
  }

  dispose(): void {
    this.stop();
    this.output.dispose();
  }

  private async ensureStartedForDocument(document: vscode.TextDocument): Promise<void> {
    if (document.languageId !== 'go') {
      return;
    }
    if (config().get<boolean>('autoStart', true)) {
      await this.start();
    }
  }

  private async startProcess(): Promise<void> {
    const command = resolveCoreCommand(this.context.extensionPath);
    const [cmd, ...args] = command.argv;
    this.output.appendLine(`starting: ${command.argv.join(' ')}`);

    const env = { ...process.env, ...coreEnv() };
    this.process = childProcess.spawn(cmd, args, { cwd: command.cwd, env });
    this.process.on('exit', (code, signal) => {
      this.output.appendLine(`inliner-core exited code=${code ?? ''} signal=${signal ?? ''}`);
      this.process = undefined;
      this.rejectAll(new Error('inliner-core exited'));
    });
    this.process.on('error', (err) => {
      this.output.appendLine(`inliner-core error: ${err.message}`);
      this.rejectAll(err);
    });

    readline.createInterface({ input: this.process.stdout }).on('line', (line) => this.handleStdout(line));
    readline.createInterface({ input: this.process.stderr }).on('line', (line) => this.output.appendLine(`[stderr] ${line}`));

    this.send({ kind: 'greeting', allowGitignore: false });
  }

  private handleStdout(line: string): void {
    if (!line.startsWith(MESSAGE_PREFIX)) {
      this.output.appendLine(line);
      return;
    }

    let message: CoreMessage;
    try {
      message = JSON.parse(line.slice(MESSAGE_PREFIX.length)) as CoreMessage;
    } catch (err) {
      this.output.appendLine(`invalid core message: ${String(err)}`);
      return;
    }

    switch (message.kind) {
      case 'connection_status':
        this.output.appendLine(`connected: ${message.is_connected}`);
        break;
      case 'response':
        this.resolveCompletion(message.stateId, message.items);
        break;
      case 'health_response':
        this.resolveHealth(message as Record<string, unknown>);
        break;
      case 'error':
        this.output.appendLine(`core error: ${message.message}`);
        break;
    }
  }

  private resolveCompletion(stateId: string, items: Array<{ kind: string; text?: string }>): void {
    const pending = this.pending.get(stateId);
    if (!pending) {
      return;
    }
    this.pending.delete(stateId);
    clearTimeout(pending.timer);
    const text = items.filter((item) => item.kind === 'text').map((item) => item.text ?? '').join('');
    pending.resolve(text);
  }

  private resolveHealth(message: Record<string, unknown>): void {
    if (!this.pendingHealth) {
      return;
    }
    clearTimeout(this.pendingHealth.timer);
    this.pendingHealth.resolve(message);
    this.pendingHealth = undefined;
  }

  private send(message: unknown): void {
    if (!this.process) {
      return;
    }
    this.process.stdin.write(`${JSON.stringify(message)}\n`);
  }

  private rejectAll(err: Error): void {
    for (const [stateId, pending] of this.pending) {
      clearTimeout(pending.timer);
      pending.reject(err);
      this.pending.delete(stateId);
    }
    if (this.pendingHealth) {
      clearTimeout(this.pendingHealth.timer);
      this.pendingHealth.reject(err);
      this.pendingHealth = undefined;
    }
  }
}

export function activate(context: vscode.ExtensionContext): void {
  const client = new CoreClient(context);
  context.subscriptions.push(client);

  context.subscriptions.push(vscode.commands.registerCommand('inliner.start', async () => {
    await client.start();
    vscode.window.showInformationMessage('inliner-core started');
  }));
  context.subscriptions.push(vscode.commands.registerCommand('inliner.stop', () => {
    client.stop();
    vscode.window.showInformationMessage('inliner-core stopped');
  }));
  context.subscriptions.push(vscode.commands.registerCommand('inliner.health', async () => {
    const health = await client.health();
    vscode.window.showInformationMessage(formatHealth(health), 'Show Output').then((action) => {
      if (action === 'Show Output') {
        client.showOutput();
      }
    });
  }));
  context.subscriptions.push(vscode.commands.registerCommand('inliner.complete', async () => {
    await vscode.commands.executeCommand('editor.action.inlineSuggest.trigger');
  }));
  context.subscriptions.push(vscode.commands.registerCommand(ACCEPT_COMMAND, (suggestion: AcceptedSuggestion) => {
    client.accept(suggestion);
  }));

  context.subscriptions.push(vscode.languages.registerInlineCompletionItemProvider({ language: 'go', scheme: 'file' }, {
    provideInlineCompletionItems: async (document, position, inlineContext, token) => {
      if (token.isCancellationRequested) {
        return undefined;
      }
      try {
        const result = await client.complete(document, position);
        if (!result || token.isCancellationRequested) {
          return undefined;
        }
        const item = new vscode.InlineCompletionItem(result.text, new vscode.Range(position, position), {
          title: 'Inliner: Accept',
          command: ACCEPT_COMMAND,
          arguments: [{ stateId: result.stateId, path: document.uri.fsPath, text: result.text } satisfies AcceptedSuggestion],
        });
        return [item];
      } catch (err) {
        client.showOutput();
        return undefined;
      }
    },
  }));
}

export function deactivate(): void {}

function config(): vscode.WorkspaceConfiguration {
  return vscode.workspace.getConfiguration('inliner');
}

function byteOffsetAt(document: vscode.TextDocument, position: vscode.Position, content: string): number {
  const utf16Offset = document.offsetAt(position);
  return Buffer.byteLength(content.slice(0, utf16Offset), 'utf8');
}

function resolveCoreCommand(extensionPath: string): { argv: string[]; cwd?: string } {
  const configured = config().get<string[]>('coreCommand', []);
  if (configured.length > 0) {
    return { argv: configured };
  }

  const pathBinary = which('inliner-core');
  if (pathBinary) {
    return { argv: [pathBinary, 'stdio'] };
  }

  const repoRoot = path.dirname(extensionPath);
  const suffix = process.platform === 'win32' ? '.exe' : '';
  const siblingBinary = path.join(repoRoot, 'inliner-core', 'bin', `inliner-core${suffix}`);
  if (fs.existsSync(siblingBinary)) {
    return { argv: [siblingBinary, 'stdio'] };
  }

  const siblingCore = path.join(repoRoot, 'inliner-core');
  return { argv: ['go', 'run', './cmd/inliner-core', 'stdio'], cwd: siblingCore };
}

function which(binary: string): string | undefined {
  const paths = (process.env.PATH ?? '').split(path.delimiter);
  const suffixes = process.platform === 'win32' ? ['.exe', '.cmd', '.bat', ''] : [''];
  for (const dir of paths) {
    for (const suffix of suffixes) {
      const candidate = path.join(dir, binary + suffix);
      if (fs.existsSync(candidate)) {
        return candidate;
      }
    }
  }
  return undefined;
}

function coreEnv(): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    INLINER_PROVIDER: config().get<string>('provider', 'ollama'),
    INLINER_OLLAMA_MODEL: config().get<string>('ollamaModel', 'qwen2.5-coder:7b'),
  };
  if (config().get<boolean>('debugVerbose', false)) {
    env.INLINER_DEBUG_VERBOSE = '1';
  }
  const debugDir = config().get<string>('debugDir', '');
  if (debugDir !== '') {
    env.INLINER_DEBUG_DIR = expandHome(debugDir);
  }
  return env;
}

function expandHome(value: string): string {
  if (value === '~') {
    return os.homedir();
  }
  if (value.startsWith(`~${path.sep}`)) {
    return path.join(os.homedir(), value.slice(2));
  }
  return value;
}

function formatHealth(health: Record<string, unknown>): string {
  const provider = String(health.provider ?? 'unknown');
  const model = String(health.ollamaModel ?? '');
  const status = String(health.providerStatus ?? '');
  const reachable = String(health.providerReachable ?? '');
  const error = String(health.providerError ?? '');
  return `inliner-core ${health.coreVersion ?? ''}: ${provider}${model ? `/${model}` : ''} status=${status} reachable=${reachable}${error ? ` error=${error}` : ''}`;
}
