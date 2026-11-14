# DB Studio Go (dbstudio-go)

![DB Studio icon](resources/icon.png)

A focused SQL query workbench — connect, browse, query, edit, export.

> **命名说明**：GitHub 上同名的 `Ben-guanzhi/dbstudio` 是本项目的 Rust/GPUI
> 前身，已于 2026-10-24 并入 polydb 冻结归档。Go 版以 **dbstudio-go** 发布，
> 用户可见名 **DB Studio Go**。

Built in Go on [mygo](https://github.com/egoist/mygo)'s native UI. The layout
is the one DataGrip taught: connections and the schema tree on the left, the
SQL editor and its result grid on the right, one status line underneath.

## Highlights

- **Query-first** — a SQL editor with highlighting, completion, hover, and
  `Ctrl+Enter` to run. Selection runs, the caret splits a script, parameters
  get asked before a statement is sent.
- **Every result, one grid** — sorting, filtering, column freeze/hide/reorder,
  paging and incremental browse for tables too large to count, numbers lined
  up on the decimal point, `NULL` never mistaken for an empty string.
- **Edit like a spreadsheet** — double-click a cell, stage inserts, updates
  and deletes, review the statements, commit them in one transaction or undo
  the whole working copy with `Ctrl+Z`.
- **Plans when it is slow** — `Ctrl+Shift+E` explains the statement under the
  caret through each engine's own dialect.
- **The cheap guards** — the window title, the colour band and the status line
  always say which server you are on; risky statements are questioned before
  they run.
- **Yours** — connections live beside the binary with the password in the OS
  keyring, the session and the column layout survive a restart, nothing is
  uploaded anywhere.

## Engines

PostgreSQL · MySQL · SQL Server · Oracle · SQLite · Redis (KV) · DM

## Build

```powershell
.\build.ps1 check            # gofmt + build + vet + test
.\build.ps1                  # development binary at build\dbstudio-go.exe
.\build.ps1 release          # stripped binaries in dist\ + sha256
.\build.ps1 release -Target all -Version 0.1.0
```

The release build stamps the version into the binary (`-X main.version`),
which the Help ▸ About box shows.

## User data

| File | Contents |
|---|---|
| `connections.json` | saved connections; passwords are `enc:v1` sealed to the OS keyring |
| `connections.history.json` | the statements that ran |
| `session.json` | open consoles, the split widths, the window size |
| `keyring.bin` | the keyring seal |

All of it lives in the app's own directory, is never uploaded, and is excluded
from the repository — a fresh clone ships none of it.

## Design notes

The long-form development record — layout rationale, the pager's two modes,
the edit pipeline, the accessibility contract — lives in
[docs/DESIGN.md](docs/DESIGN.md).
