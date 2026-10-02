package cli

import "github.com/tokenflux/tf-cli/internal/ui"

const agentReadme = `# tf for agents

Use tf as a process-scoped launcher for Claude Code, Codex, OpenCode, or Pi through TokenFlux or TokenRouter.

## Rules

- Use --json when consuming command results.
- Use --no-tui for scripts, CI, and agent-run commands. It never opens a selector or waits for terminal input.
- Do not pass API keys in logs, prompts, or source files. Prefer TF_API_KEY for one launch or tf login for persistent storage.
- tf never changes harness config globally; injected values live only in the child process.
- Commands that need network access say so explicitly.

## Common commands

  tf auth --json
    Explain the credential that would be used. Read-only and offline by default.

  tf status --json
    Read local config, key bindings, model slots, installed harnesses, and environment conflicts. Offline.

  tf status --check --json
    Also check each stored key's remote usage. Partial failures are in data.check_errors; a readable local state still returns ok=true.

  tf keys --json
    List stored keys with masked values and protocol/harness scopes.

  tf keys --refresh --json
    Refresh model and protocol metadata. Network access; preserves cached metadata on failure.

  tf claude --no-tui -m MODEL -- --help
  tf codex --no-tui -m MODEL -- exec PROMPT
    Launch a harness without TUI selection. Arguments after -- go to the harness.

  TF_API_KEY=... tf codex --no-tui -m MODEL -- exec PROMPT
    Use an ephemeral key. It is not written to disk.

  tf model claude --no-tui --json
    Read model slots. Use tf model claude --edit only in a human terminal.

## Errors and exit codes

- JSON success: {"ok":true,"command":"...","data":...}
- JSON failure: {"ok":false,"command":"...","error":{"code":"...","message":"..."}}
- 0 means the command completed. 1 means command execution failed (including TF_USAGE detected during execution). 2 means command dispatch or argument parsing failed. 130 means cancellation. Launched harness exit codes pass through unchanged.
- --no-tui disables tf prompts only; use the harness's own noninteractive options after --. Harness output is not wrapped in tf's JSON envelope.
- A failed remote status check does not make local status fail; inspect checked and check_errors.

## Login

- Interactive human: tf login
- Existing key or automation: printf '%s' "$KEY" | tf login --with-key --no-tui
- Web import always requires a human terminal confirmation and cannot be bypassed with --json or --no-tui.
- Use --host for a self-hosted gateway.

## Model slots

The first interactive launch asks for the main model and auto-fills auxiliary slots. Automatic slots can update after tf keys --refresh. Manual tf model HARNESS --set slot=MODEL or --edit choices are preserved.

## Completion

Completion is local-only and never performs network requests. Refresh model metadata first when model candidates are missing.
`

func newAgentReadmeCommand() *Command {
	return &Command{
		Name:  "agent-readme",
		Usage: "tf agent-readme",
		Summary: func(u *ui.UI) string {
			return u.T("输出面向 Agent 的使用说明", "Print agent-oriented usage instructions")
		},
		Run: func(c *Context) error {
			c.UI.Emit("agent-readme", map[string]string{"text": agentReadme}, func() {
				c.UI.Printf("%s", agentReadme)
			})
			return nil
		},
	}
}
