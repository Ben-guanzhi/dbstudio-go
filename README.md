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
- **The management tools** — sessions list with kill (two-step confirm),
  server variables (read-only), user management (create/drop/grant through the
  kernel's dialect-correct DDL), data transfer between saved connections,
  schema refresh after a rename.
- **Build and review** — a query builder that assembles SELECT (joins,
  aggregates, GROUP BY, OR groups) into the editor; a Review Changes panel
  that shows every staged insert/update/delete before the commit; SQL
  snippets by name; script files open/save/save-as per console tab.
- **A model designer** — entities, columns and 1:N / N:M relations edited as
  a versionable JSON file, forward-engineered into dialect-correct DDL.
- **Yours** — connections live beside the binary with the password in the OS
  keyring, the session and the column layout survive a restart, nothing is
  uploaded anywhere.

## Engines

**Built in**: MySQL · SQLite (zero-config local engine)

**As plugins** — drop the executable beside the app, the connection form picks
it up; each connection runs its own plugin process:

| Plugin | Download |
|---|---|
| `dbstudio-plugin-postgres` | the repo's Releases page |
| more engines | build from `cmd/dbstudio-plugin-<kind>` — the SDK (`internal/pluginsdk`) is ~300 lines to implement |

A connection whose kind has no built-in driver and no plugin beside the
executable reports exactly that, with the naming convention to fix it.

## Build

```powershell
.\build.ps1 check            # gofmt + build + vet + test
.\build.ps1                  # development binary at build\dbstudio-go.exe
.\build.ps1 release          # stripped binaries in dist\ + sha256
.\build.ps1 release -Target all -Version 0.1.0
```

#### Oracle 是可选驱动

默认构建（约 25MB）带 PostgreSQL / MySQL / SQL Server / SQLite / Redis。Oracle
的纯 Go 驱动（go-ora）一项就要 ~13MB——绝大多数用户不会为它买单，所以它是
构建可选项：

```powershell
go install github.com/tc-hib/go-winres@latest   # （release 脚本用）exe 资源
go build -tags oracle -o dbstudio-go-oracle.exe # ~38MB，带 Oracle
```

带 `-tags oracle` 构建时连接表单出现 Oracle 选项；默认构建里一个已保存的
Oracle 连接会得到一句怎么把它带回来的明确提示，而不是内核的 "unknown kind"。

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
