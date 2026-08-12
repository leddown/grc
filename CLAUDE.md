@Agents.md

## Claude Code session practices

- For any task with 3+ distinct steps, use `TaskCreate`/`TaskUpdate` to
  create and track a todo list before starting work, and keep it current
  (`in_progress` → `completed`) as you go.
- When spinning up a server for manual verification (`go run ./cmd/api`,
  a built binary, etc.), use a throwaway port/db, kill the process, and
  remove any temp db/log files before ending the session.
- `main.go` has repeatedly picked up stray trailing characters (likely from
  the IDE) that break `go build` with a syntax error unrelated to whatever
  you're actually working on. If `go build` fails there, check for garbage
  text after the closing `}` before assuming a real bug.
