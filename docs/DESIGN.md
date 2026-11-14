# DB Studio

一个用 [mygo](https://github.com/egoist/mygo) 原生 UI（`ui` 包）编写的 SQL 查询客户端，
界面方向参考 JetBrains DataGrip：左侧是「连接 + 数据库 schema 树」，右侧是「SQL 编辑器 + 结果表格」。

## 界面布局

```
┌────────────────┬────────────────────────────────────────────┐
├────────────────┼────────────────────────────────────────────┤
│ Connections    │ Run   Clear   New Connection   Export ▼       │
│   - local      │ Database: New / Run / Export CSV / JSON / INSERT / Copy as │
│   History      │                                            │
│   12:04:05     │ Editor                                     │
│   SELECT ...   │   SQL editor (line numbers, highlight,     │
│   Schema       │   Ctrl+Enter to run)                       │
│   > public     │ Results                                    │
│     > users    │   [filter bar]  #  id  name  ...           │
│                │   << First < Prev  Page 1 / 3  Next > Last │
└────────────────┴────────────────────────────────────────────┘
```

- **左侧**：保存的连接列表（新建 / 编辑 / 删除），下面是**查询历史**（最近执行的语句，
  点击重新载入到编辑器），最下面是当前连接的 schema 树
  （数据库 → schema → 表 → 列）。展开 schema 会异步加载表；展开表会异步加载列。
- **编辑器**：语法高亮、行号、`Ctrl+Enter` 执行、SQL 自动补全。点击树中的表会自动填入
  `SELECT * FROM <schema>.<table> LIMIT 100`。
- **结果**：`ui.Table` 网格，行号固定在最左；NULL 显示为灰色 `NULL`。
  在表上**双击**直接浏览数据。网格下方是分页器（见下节）。网格上方是**筛选栏**，
  网格内**右键**是复制菜单（见「网格排序」「网格行筛选」「复制为 SQL / CSV」）。
- **工具栏**：`Run` / `Clear` / `New Connection` / `Export`（右键选 CSV / JSON / INSERT，
  见「导出文件」），右侧显示状态。
  编辑表格时，中间出现 `Insert Row` / `Delete Row` / `Commit` / `Rollback`。
- **菜单栏**：`Database` 下除 `New Connection` / `Connect` / `Disconnect` / `Run Query` /
  `Run Selection` / `Run to Caret` / `Explain Query` / `Execute with Parameters…` /
  `Format SQL` / `Commit` / `Rollback` / `Export CSV / JSON / INSERT` / `Compare Schemas…` 外，还有
  `Copy as INSERT / UPDATE / DELETE / CSV`（作用于整张结果网格）和 `Clear History`。
  `View` 下有 `Clear Messages`（清空消息面板）和 `Reset Split`（重置左右分栏宽度）。

## 快捷键

| 快捷键 | 作用 |
| --- | --- |
| `Ctrl+N`（macOS `Cmd+N`） | 新建连接 |
| `Ctrl+Enter` | 执行编辑器里的 SQL；编辑工作副本时等价于提交 |
| `Ctrl+Shift+Enter` | 执行选中的 SQL（无选区时执行到光标处） |
| `Ctrl+Shift+R` | 执行到光标处 |
| `Ctrl+Shift+E` | 解释（EXPLAIN）当前语句 / 选区 |
| `Ctrl+Shift+P` | 带参数执行当前语句 / 选区（参数面板） |
| `Ctrl+Shift+F` | 格式化编辑器 SQL（选区优先） |
| `Ctrl+Shift+M` | 清空消息面板 |
| `F2`（macOS `Enter`） | 开始编辑当前单元格 |
| `F12` / `Cmd`(`Ctrl`)+单击 | 转到定义（打开该表/视图的 DDL） |
| `Alt+←` / `Alt+→` | 上一页 / 下一页（焦点在窗口任意位置都生效） |

## 结果表格的列宽

每列有一个**按内容估算的起始宽度**：列名与该列前 8 个单元格中较宽的那个，按字符数换算
（约 7.4px/字符 + 28px 内边距），下限 88、上限 560。

- 这样就不会出现"列被平均压成一条缝"：宽结果**横向滚动**，而不是把每个字段挤到看不清。
- 起始宽度只是起点：拖列头右缘可调（受 88–560 限制），**双击右缘**按单元格内容自适应。
- 行号列固定 44px，不可移动、不可缩放。

## 界面布局

方向参考 **DBeaver**，区域内的能力参考 **DataGrip**：

```
┌ 工具栏 ─────────────────────────────────────────────────────────────┐
│ Run · Run Selection · Run to Caret · Stop │ Clear │ Autocommit · 隔离 │
│ Refresh · Export · History · New Connection │ [工作副本: 表名]          │
├──────────────┬──────────────────────────────────────────────────────┤
│ Database     │ [Console 1] [＋] [×]                         1 / 8   │
│ Navigator    │ Editor                               连接名          │
│ ＋ ✎ ⟳       │ ┌──────────────────────────────────────────────────┐ │
│ 连接列表     │ └──────────────────────────────────────────────────┘ │
│──────────────│ [Result 1] [Result 2] …                              │
│ Schema [▾]   │ Results  Refresh            3 of 128 row(s)          │
│ Search…      │ [Data] [Messages (4)]                                │
│ ┌ 树 ──────┐ │ ┌ 网格 / 消息 ───────────────────────────────────┐   │
│ └──────────┘ │ └────────────────────────────────────────────────┘   │
│              │ « ‹ Page 1/2 › »                          Rows/Page   │
├──────────────┴──────────────────────────────────────────────────────┤
│ ● 连接名 │ schema: gs_prod │ MySQL │ manual commit │ 128 row(s)  消息  │
└─────────────────────────────────────────────────────────────────────┘
```

两条规则贯穿整个界面：

- **工具栏只改状态，状态栏只报状态。** 以前状态文字夹在按钮中间，读起来像两个不同的事实；
  现在"我在哪、花了多少"全部归状态栏（连接、工作 schema、引擎、事务模式、行数、消息）。
- **每个区域都有标题和空状态**（`Database Navigator` / `Editor` / `Results` + `Data`/`Messages`），
  空区域永远给出原因（"Not connected — pick a connection above"、"No results yet — run a
  statement (Ctrl+Enter)"、"Nothing reported — …"），而不是一片空白。

具体变化：

- **导航器**（左）：连接列表 + 连接动作（＋/✎/⟳/Delete）+ 工作 schema 下拉 + 对象搜索 + 对象树。
  查询历史从"常驻 200px 侧条"改为**工具栏 `History` 打开的工具窗口**——那是它该在的地方，
  而且树拿回了这段高度（三十个 schema 的服务器上，这段高度就是全部意义）。
  选定 schema 后**树只显示该 schema 并自动展开**（选择器因此有可见效果）。
- **结果区**（右下）：语句页签 + `Results` 标题栏 + **`Data` / `Messages` 两个页签**。
  消息不再长期占用纵向空间，只在切到该页签时显示；`Messages` 上的计数是当前语句的报告行数。
- **窗口本身带颜色**：有颜色的连接在窗口顶部画一条 3px 色带，窗口标题变成 `DB Studio — ■ prod 🔒`（任务栏上也能认出是哪台服务器）。
- **状态栏**（底部）：健康圆点 + 连接名（完整地址在 tooltip）+ 工作 schema + 引擎 + 事务模式 +
  本次结果行数 + 最后一条消息。
- **每个 console 有自己的编辑器状态**：撤销栈、悬停缓存、补全上下文都在 `editor.State` 里，
  共用一份会让它们串到别的页签。

### 布局的一条硬规则：`Grow(1)` 而不是 `Fill()`

mygo 的 `Fill()` 意思是"**与父容器一样大**"，`Grow(1)` 才是"取剩余的弹性空间（并允许收缩）"。
一个容器里若还有兄弟节点，用 `Fill()` 会抢走整块高度、把兄弟挤出可视区——之前树、网格、
分页条、状态栏被裁掉正是这个原因。所以：**唯一子节点可以用 `Fill()`，有兄弟一律 `Grow(1)`**。

## 多结果集（一次执行，多个结果）

一条语句可以回答多个结果集：SQL Server 的批次（`SELECT…; SELECT…`）、MySQL 的 `CALL`
存储过程（每个 SELECT 一个集）。这类回答现在**每个集一个页签**，按引擎给出的顺序排开：

- 第一个集进该语句自己的页签，后续集紧随其后，标题形如 `SELECT … · 2/3`。
- 只有**第一个集**可能成为可编辑的工作副本（只有它才可能是那张表的网格）。
- 一个批次里的 DML 会回答一个**没有列**的空集——那不是结果，不占页签。
- 每个集各自套用 `DriverMaxRows` 上限（不是总和），一个多集批次不会把后面的集悄悄截断。
- 引擎/驱动不支持多集时退回单集路径，行为与以前完全一致；事务打开时也走单集路径
  （`TxManager` 的 `ExecuteInTx` 只取一条语句的一个集）。
- MySQL 为此打开了 DSN 的 `multiStatements`（否则存储过程/批次永远只有一个集可拿）。

内核侧：`dbcore.MultiResultReader` + `AsMultiResult`，`RunMulti` 是唯一的遍历实现
（`Rows.NextResultSet` 的走法、空集跳过、逐集上限都在这里）。

## 打磨细节（对照成熟客户端）

几处"没有就难受、有了不显眼"的东西：

- **破坏性语句先问一句**：`DROP` / `TRUNCATE` 一律问；`DELETE` / `UPDATE` **没有 WHERE** 才问
  （带 WHERE 的增删改是日常操作，问了只会被点掉）。弹窗显示**完整语句**和原因，确认按钮
  不是默认焦点（回车不应直接执行）。一次提问只问一次：确认后按原样跑同一条脚本。
- **Test Connection**：连接表单里的 `Test` 按钮用表单当前值真的连一次（走同一条 `OpenDB`，
  所以 SSH 隧道也会被测到），回报"Connected to MySQL in 12ms"或失败原因，**不保存任何东西**。
  失败信息写在测试自己那一行，不会混进表单的保存错误里。
- **会话恢复**：退出时把工作区写进 `session.json`（与 `connections.json` 同目录）——8 个
  console 的名称与 SQL、当前是哪个、工作 schema、左右/上下分栏位置、窗口尺寸。下次启动原样回来。
  页签增删/切换与每次执行后也会写一次，所以崩溃最多丢"上次执行以来的输入"。
- **数字列右对齐**：按列的 SQL 类型判定（走 `sqlcodec.KindOf`，与单元格编辑同一套分类），
  数值列靠右，文本/日期靠左——一列数字能对齐小数点才谈得上"一眼可比"。
- **快捷键**：`Ctrl+Enter` 执行、`Ctrl+T` 新 console、`Ctrl+Shift+W` 关当前（用 Shift 是因为
  `Ctrl+W` 在 Windows 上是关窗口）、`Ctrl+Tab` 下一个 console、`F5` 重读当前结果、
  `Alt+←/→` 翻页、`F12` 转到定义、`Ctrl+A`/`Ctrl+Space` 补全。

## 只读模式 与 连接颜色

这两件是配套的：**颜色让你看见是哪台服务器，锁让它不能改**。

### 只读模式

连接表单里的 `Read-only` 勾选（或工具栏的锁开关）把这条连接锁成只读。锁**在真正会写的
地方**检查，而不是在输入的地方提示：

| 路径 | 锁定后的行为 |
|---|---|
| 脚本执行 | `INSERT`/`UPDATE`/`DELETE`/`DROP`/`TRUNCATE`/`CREATE`/`ALTER`/`RENAME`/`GRANT`/`CALL`/`EXEC`/`COPY`/`VACUUM` 等**在执行前被拒**，并在错误里指出是哪一条 |
| `WITH … DELETE/UPDATE` | 同样拒绝（首个词是 `WITH`，效果仍是写） |
| 表格编辑 | **不会产生工作副本**：单元格不可改，`Commit` 根本不出现 |
| 提交工作副本 | 即使存在副本也被拒（锁在写入点再查一次） |
| 数据导入 | 拒绝并说明原因 |
| 读取 | **不受影响**——这正是只读连接存在的意义 |

- 分类**保守**：`CALL`/`EXEC` 可能写，所以在锁定的连接上会被拒；只读的存储过程要解锁才能跑。
  这是刻意的取舍——安全的默认值比"猜它是不是只读"重要。
- 锁**随连接保存**：工具栏的开关会把选择写回连接，下次启动仍然是锁着的（会重置的锁不算锁）。

### 连接颜色

表单里的 `Colour` 从五个可辨识的颜色里选（`red (production)` / `amber (staging)` /
`green (local)` / `blue` / `purple`），空 = 无颜色。

- 导航器的连接行与**状态栏**都会显示 `■` + 连接名，并用该颜色着色；锁定时追加 `🔒`。
- 用方块而不是圆点：状态栏的健康提醒是圆点，同一个窗口里两个圆点表示不同意思是谜题。
- 颜色键随连接保存（`red`/`amber`/…），配色表可以改而不影响已保存的连接；不认识的键不会
  凭空画出一个标记。

## 大表行数估算 与 列宽记忆

两件"大表/常用表"才暴露出来的事：

### 行数估算

分页器不知道总数时以前显示 `Page 1 / ?`。现在多说一句**目录里的估算**：`~1.2M rows`。

- 数据来源按代价分层：**表列表里已有的估算**（PostgreSQL / SQL Server / Oracle / DM 的
  `reltuples`、`sys.partitions`、`num_rows` 之类，取列表时顺带就有）优先；表列表没有的
  （MySQL——它的 `TABLE_ROWS` 会让服务器为整个 schema 收集统计，所以列表不查）走驱动的
  `RowEstimator`，**一次只问一张表**，代价限定在这张表上。
- 缺失就是缺失：视图、统计从未收集、引擎不支持时**不显示估算**，分页器保持 `?`——
  一个编造的数字比一个诚实的问号更糟。
- 结果按表缓存（含"没有"这一结果），所以不会每帧去问；`Refresh Schema` 后重新可得。
- 显示带 `~`，并说明"来自目录统计，不是计数"。
### 列宽记忆
拖出来的列宽会**写进会话**（`session.json` 的 `widths`，按列名），下次启动原样打开。

- 宽度由 mygo 的表格布局持有（`ListState.Columns.Widths`），每次绘制后对比一次，
  只有真的变了才写盘——不是每帧都写。
- 恢复时同时记为"当前状态"，所以重启后的第一帧不会误判成"用户拖过"而重复写。
- 按列名记忆：同名列在不同结果里共享宽度，这正是想要的（`id` 永远窄、`name` 永远宽）。

## 滚动到底自动加载（浏览表）

浏览一张表不再"一页 100 行、点 Next 换一页"：**读到接近已加载内容的末尾就自动取下一段并追加**，
表读起来像文档。

- 分页条对浏览改成**增量版**：`已加载 1,300 of ~1.2M rows` + `Load More`（手动兜底），
  以及 `Rows/Fetch`（每段取多少行）。**查询结果**仍用原来的页窗口（它是一次取回后客户端分页，
  和"表的下一段"是两回事）。
- 三条规则：
  1. 只有读到**接近末尾**（差约一屏）才取下一段，看开头几行只花一次请求；
  2. **有未提交编辑时停止自动加载**——追加会重读行，未提交的改动会跟着丢；
  3. **上限 5 万行**（`rowsLoadedMax`），到顶后状态栏说明并让你改用查询编辑器——"无限滚动"
     到一亿行只是带进度条的泄漏。
- 取了新段后工作副本会按已加载的行重建（干净时才做），所以网格仍可编辑。
- 某段失败会报告原因并停止继续自动加载，已加载的行保留。

## 表头的列信息

悬停列头会说明这一列是什么：`name · varchar(50) · not null · primary key · default ''`。

- 类型与可空来自结果集自身的元数据；**浏览表**时再从目录补上主键、默认值、注释——那里
  知道得更多。
- 为什么值得有：写 `WHERE`/字面量之前，需要知道这列是数值还是文本（`'12'` 要不要引号）、
  能不能为 NULL。这些信息以前只能去树里翻。

## 列的隐藏与重排

宽结果读不动：审计表有用的列不在前十二，join 里重复的 `id` 又碍事。两条路都可以：

- **表头右键**：`Hide This Column` / `Show All Columns` / `Move Left` / `Move Right` /
  `Columns…` / `Fit to Content`。
- **工具栏 `Columns`**：一个对话框列出全部列，勾选显隐、`↑`/`↓` 调序、`Show All`、
  `Reset Order`。被误点隐藏的列从这里回来。

规则：

- **按表记忆**（浏览表时）：`schema.table` 的排列写进会话，下次打开同一张表原样回来；任意查询结果没有稳定的表名，**按页签记忆**（生命周期内有效）。列名在不同表里含义不同，`id` 在一个表里藏起来不该影响另一个。
  同一页签内，翻页、排序、过滤、编辑都不会打乱你的排列。
- **最后一列不能隐藏**（否则是"空网格"），拒绝时状态栏说明。
- 隐藏的列**仍留在顺序里**：重新显示时回到原来的位置，而不是跑到末尾。
- 排好之后**拖列头**也有效——拖拽与对话框写的是同一个地方（mygo 的 `ListState.Columns.Order`），
  每帧把页签顺序交给布局、再用布局的结果更新页签，两者不会打架。
- `Fit to Content` 清掉这列的宽度记忆，让它重新按内容定宽。

### 冻结列（受工具包限制的诚实版本）

表头右键 `Freeze Columns to Here` 把**当前可见列到这一列**冻结，`Unfreeze Columns` 解除。
冻结的含义是：这些列**排在最前**、**不可拖动**、**不可缩放**，并按表记住（`session.json` 的
`frozen`）。

**必须说清的边界**：mygo 的表格是"一整条横向滚动的行"，**没有粘滞列**（`Fixed` 只锁定顺序
与宽度，不锁定滚动位置）。所以本项实现的是"锁顺序 + 锁宽度 + 置顶"，**不是**"横向滚动时钉在
左边"。真正的粘滞列需要改工具包（自绘覆层或给 mygo 加 sticky 支持），是另一件更大的事——
文档不声称它做到了。

## 导出与复制只含可见列

导出（CSV/JSON/Excel/INSERT）与"复制为 CSV/INSERT/UPDATE/DELETE"都**以网格所见为准**：
隐藏的列不出现，顺序按你排的顺序。

- 同一个投影（`visibleResult`）供导出与复制共用，所以文件与剪贴板里的东西一致。
- **隐藏了主键时拒绝 UPDATE/DELETE**并说明原因——没有键的 UPDATE 定位不到行，生成它是假的。
- INSERT/UPDATE/DELETE 的列名以投影后的结果为准（此前用的是工作副本的全列，隐藏列会被写进去，
  这是这次修掉的缺陷）。

## 结果分页

结果表格下方是 DataGrip 风格的分页器：`« First` / `‹ Prev` / `Page n / N` /
`Next ›` / `Last »`，右侧是 `Rows/Page` 选择器和 `Rows x–y of N` 行数。

- **翻页快捷键**：`Alt+←` 上一页，`Alt+→` 下一页，焦点在窗口任意位置都生效；
  到首页 / 末页时不会再翻。
- **点击页码跳转**：`Page n / N` 是一个按钮，点击弹出「Go to page」对话框，输入
  页码后 `Go` 跳转。页码超出范围会被拒绝并提示（不会静默夹到边界），输入非法值
  （小于 1、大于总页数）时对话框保留。
- **浏览表**走**服务端分页**：`BrowseRows` 每次只取一页，`Page` / `PageSize`
  传给驱动，页数与总数来自数据库的 `COUNT`。`Rows/Page` 与翻页都会重新向数据库请求，
  因此即使表很大也不会把整张表拉进内存。
- **任意 `SELECT`** 没有可移植的「给我第 k 页」，所以走**客户端分页**：翻页只是在
  已加载的行上切窗口（受驱动的材料化上限 `DriverMaxRows` 约束）。被截断时行数后面
  会显示 `(truncated)`。
- 页大小可选 50 / 100 / 200 / 500 / 1000（默认 100）。改动页大小会回到第 1 页。
- 服务端分页下，如果工作副本有未提交的改动，翻页会被锁定——翻页会重新读取该表，
  从而丢掉未保存的编辑。先 `Commit` 或 `Rollback` 再翻页。
- 服务端拒绝 `COUNT` 时（`TotalFound` 为假）没有总页数：此时如果取回的一页是满的，
  分页器按「还有更多」处理，`Page n / ?` 且没有 `Last`；取回不满一页则视为末尾。
  这种「未知页数」的结果仍然可以点击页码跳转——跳到的页会重新向数据库请求。
- 翻页后行选择会清空；`Insert Row` 会把新行所在的页翻到前台并选中它。

## 表数据编辑

浏览一张表（双击树节点，或执行 `SELECT * FROM <表>`）后，结果网格就是这张表的
**工作副本**，可以直接改动：

- **编辑单元格**：单元格光标（粗边框）所在列可编辑，`F2`（macOS 上 `Enter`）开始编辑，
  或按住单击约 0.5 秒后直接输入。`←` `→` 移动单元格光标，行首的 `#` 列会整体选中该行。
- **新增行**：工具栏 `Insert Row`，行首显示 `+`。
- **删除行**：工具栏 `Delete Row`，行首显示 `–`，文字带删除线；提交时才真正删除。
- **提交 / 回滚**：`Commit` 把全部改动作为一个事务批量写回（修改、删除、新增一起）；
  `Rollback` 丢弃改动，恢复读取时的样子。`Ctrl+Enter` / `Run` 在有改动时等价于提交。
  这个提交走内核的批量写入器，与编辑器脚本的连接级事务是两套；连接级事务开着时
  `Commit` 会被拒绝（见「连接级事务」）。
- 单元格显示 `NULL`（灰色）表示该列没有值；编辑框里留空即写回 `NULL`。
  主键为空的行无法更新或删除，提交会给出原因。
- 提交后自动重新读取该表，提交结果、行数显示在状态栏。

**可编辑的范围**：只有「浏览一张表」和「`SELECT * FROM <表>`」这两类结果可以编辑。
任意 `SELECT`（投影、`JOIN`、聚合、多表）没有唯一的写入目标，因此保持只读——与
DataGrip 的行为一致。列的主键、自增标记来自元数据缓存，结果集自带的列信息没有这些标记。

## 网格排序

点结果网格的列标题按该列排序：点一次升序、再点一次降序、第三次取消。标题上显示
方向箭头；`Ctrl+点击`（macOS 上 `Cmd+点击`）把该列加进多列排序，作为次级键。多列排序时
箭头从左到右表示主键、次键的顺序。

- **浏览表**走**服务端排序**：`BrowseRows` 的 `Sort` 把列名与方向传给驱动，翻页时
  排序跨页保持。标题被拖动过的话，列顺序用列 ID 保持，切换标签页回来仍是上次的样子。
- **任意 `SELECT`** 的行在客户端重排（结果集本身没有可移植的服务端排序接口），
  数值与时间按类型比较，文本按数据库排序规则近似（大小写不敏感）。
- 工作副本有未提交改动时，点标题排序会先**回滚**改动再排序，避免把编辑过的行顺序搞乱。

## 网格行筛选

结果网格上方有一行筛选条件：选列、选操作符（`=` `!=` `>` `>=` `<` `<=` `LIKE`
`IS NULL` `IS NOT NULL`）、输入值，`×` 删除条件，`+` 添加条件。多个条件之间是 `AND`。

- 浏览表走**服务端筛选**（`Filter` 传给 `BrowseRows`，条件随翻页保持）；任意
  `SELECT` 在**客户端筛选**已加载的行。两种路径都是「条件 → `FilterExpr` → 行」的
  同一套匹配逻辑（`LIKE` 用经典通配符回溯，大小写不敏感）。
- `IS NULL` / `IS NOT NULL` 忽略值输入框的内容；`LIKE` 的 `%` `_` 按通配符解释。
- 值按单元格自身的类型比较（数值比较数值、文本按文本），和 DataGrip 的筛选一致。
- 只读结果（任意 `SELECT`）只能客户端筛选，且网格上的筛选栏在等待服务端页面时保持不动。

## 复制为 SQL / CSV

结果网格的**右键菜单**提供复制：单个单元格、单行语句、整张表 CSV。

- `Copy Cell` 复制单元格本身（`#` 列复制行号）。
- `Copy Row as INSERT` / `UPDATE` / `DELETE` 把所在行渲染成一条语句。
- `Copy Grid as CSV` 把整张结果复制为 CSV（含列名行，NULL 为空）。
- 菜单 `Database` 下还有 `Copy as INSERT` / `UPDATE` / `DELETE` / `CSV`，作用于整张网格。

生成的 SQL 用**连接自己的方言**渲染：标识符按引擎加引号（MySQL 反引号、SQL Server
方括号、其余双引号），字符串字面量按引擎转义；数值不加引号，`NULL` 写 `NULL`，
BLOB 写 `X'...'`。`INSERT` 列出全部列；`UPDATE` 的 `SET` 是所有非主键列、`WHERE` 是
主键；`DELETE` 只有主键 `WHERE`。没有主键的表无法生成 `UPDATE` / `DELETE`（和提交的
限制一致）。只有「浏览一张表」/ 工作副本能生成语句——任意 `SELECT` 没有唯一写入目标；
CSV 则不需要表名。语句按结果自身的列与行渲染，工作副本里未提交的改动不在其中。

## 导出文件（CSV / JSON / Excel / INSERT）

工具栏 `Export`（或 `Database` 菜单）把**当前结果**导出成文件：`CSV` /
`JSON` / `Excel` / `INSERT` 四选一。除 INSERT 外可导出任意结果；INSERT 需要结果是一张表的
行（「浏览一张表」 / `SELECT * FROM t`），否则给出原因——和「复制为 INSERT」的限制一致。

- 导出的是**工作副本**：表格正在编辑时，未提交的改动（插入的行含在内、删除的行排除）
  按所见导出，不会漏掉还没提交的编辑。
- **CSV**：列名行 + 每行一 record，NULL 留空（同「复制为 CSV」）。
- **JSON**：数组，每行一个对象；NULL 是 `null`、整数/浮点/布尔保持类型、BLOB 写
  十六进制 `X'...'`，文本原样——一个能看懂也能再导入的文件。
- **Excel（.xlsx）**：单表工作簿，首行加粗并冻结，列宽按内容估算。数值真的写成数值
  （Excel 里能直接求和），但**保留前导零的码值仍写成文本**（`007`、`0421` 不会变成 7 / 421
  ——那是电子表格最经典的数据损坏）；NULL 写成空白单元格而不是"NULL"；超过 15 位有效数字的
  数值也按文本写出，避免 Excel 静默四舍五入。实现见 `internal/xlsx`，零外部依赖
  （xlsx 就是一个 zip + 若干 XML 部件）。
- **INSERT**：每行一条 `INSERT ...;`，按连接方言引号与转义渲染（同「复制为 INSERT」）。
- 文件写到连接文件同目录，命名 `query-<时间戳>.<csv|json|xlsx|sql>`，状态栏报告文件名。

## 执行计划（EXPLAIN）

`Database → Explain Query`（`Ctrl+Shift+E`）对**光标所在语句**（有选区则对选区）生成并展示
数据库的执行计划，结果出现在结果页签里，和普通查询一样可复制、可翻页。

- 计划语句由内核 `sqltext.ExplainPlanStructured` 按连接方言生成：SQLite 用
  `EXPLAIN QUERY PLAN`（步进行），PostgreSQL / MySQL 用 `EXPLAIN`（PG 还会带
  JSON 计划，若想读可导出 JSON/CSV 再看）。语句有活动事务时该事务内执行，未提交的
  结构变化会如实反映进计划。
- SQL Server 和 Oracle 的 EXPLAIN 需要在**同一条会话**里先占位再查询，池化连接暂不
  支持，因此这两个引擎上会给出明确说明而非错误地执行。
- 只对需要解释的引擎有意义：Redis 连接没有计划一说，会直接提示。

## 参数化查询

`Database → Execute with Parameters…`（`Ctrl+Shift+P`）对光标所在语句（有选区则对选区）打开参数
面板：语句里的占位符被提取出来，每个一个输入框，`Run` 后按**绑定**执行——值作为
prepared statement 参数交给驱动，**从不转义拼接**，没有 SQL 注入面。

- 支持的占位符：`?`（位置，MySQL/SQLite 风格）、`$n`（自编号，PostgreSQL 风格）、
  `:name`（命名，Oracle 风格）。字符串与注释里的同样写法**不算**占位符；PostgreSQL 的
  `::type` 强转也不会误判。
- 面板按**连接的方言**改写标记：MySQL/SQLite 用 `?`，PostgreSQL 用 `$n`，
  SQL Server 用 `@pn`，Oracle/DM 用 `:n`。所以你在 MySQL 连接上写 `:low` 也能跑——
  面板把它改写成 `?` 后绑定。
- 命名参数复用**首次出现**的绑定位置（`WHERE a = :low AND b > :low` 只填一次 low）；
  `$n` 按 n 自编号（`$2` 永远绑第二个值）。
- 值的解读：空输入绑 `NULL`；纯数字绑数值；带前导零的文本（如 `007`）保持文本——
  那是代号不是算术值；其余绑文本。
- 语句有活动事务时该事务内执行（与 Run 一致）；结果走与普通查询完全相同的页签、
  复制、导出管线。

## SQL 格式化

`Database → Format SQL`（`Ctrl+Shift+F`）把编辑器里的 SQL 重排成可读的布局：关键字大写、
子句各占一行并按嵌套深度缩进、逗号后换行、运算符两侧留空格、限定名（`schema.table`、
`t.col`）不加空格。字符串字面量与注释**原样保留**（包括内部大小写、空格与转义），因此
格式化不改变语句的含义——跑得通的语句格式化后依然跑得通。

- 有**选区**时只原地格式化选中的片段，前后的文本（含注释）不动。
- 格式化是幂等的：再按一次结果不变。
- 格式化器是 `internal/sqlformat` 独立包（无依赖、无解析器），方言无关：它认识的子句
  关键字与引号规则是 6 种引擎共享的 SQL 标准部分，MySQL 反引号标识符、SQL Server 方括号
  标识符也会作为引号字符串整体保留。

## 结构比较（Compare Schemas）

`Database → Compare Schemas…` 比较**两个已保存连接**的结构差异，并生成能把它应用到目标库的
迁移脚本。

- 弹窗里选 `Source` 与 `Target`（默认当前连接为源、下一个连接为目标），`Diff` 开始比较。
  两个连接为比较单独打开、比较完即关，**不动**你正在浏览的那条连接。
- 比较的是每个连接的**第一个 schema**（SQLite 的 `main`、其余的首个库），由内核
  `schemadiff.Collect` 读元数据、`Diff` 求差异、`Script` 渲染成目标方言的语句。
- 条目列表逐条显示操作（`create_table` / `add_column` / `create_index` / `add_foreign_key` 等）、
  表、对象与原因；**破坏性操作**（删表/删列/收窄类型等）用红色标出。
- `Warnings` 原样展示内核报告为"无法表达"的变更——例如 SQLite 不支持事后加外键、
  SQL Server 的命名默认值约束、带表达式/谓词的部分索引。**空计划不等于没有差异**，
  所以告警必须读。
- `Apply to Target` 逐条执行脚本，**遇错即停**并报告 `Applied N of M, then: <原因>`。
  不包事务：MySQL/Oracle 的 DDL 会隐式提交，包了也只是假原子——诚实语义是"前滚到失败点"，
  与业界客户端的做法一致。
- 应用完成后再按一次 `Diff`，差异应为空（脚本确实把目标补齐了）。

## 编辑器多页签（Consoles）

编辑器上方是**控制台页签**：每个页签一个独立的 SQL 编辑器，`+` 开新页签（最多 8 个）、
`×` 关当前（最后一个不可关）、点页签切换。

- 切换页签只换编辑器底下的缓冲区：**Run、Run Selection、Explain、Format、参数面板、
  补全、悬停、转到定义都作用于当前页签**——它们不知道有多个页签存在。
- 页签按内容**自动命名**：跑过语句或点过表之后，页签名变成它持有的第一条语句的首行
  （截到 24 字符）；空页签保持 `Console N`。两个页签的名字永不相重。
- 点树中的表、从历史载入语句，都写进**当前**页签——想在另一个页签里浏览，先切过去。
- 页签计数 `n / 8` 显示在右侧。

## 悬停文档与转到定义

编辑器对 SQL 里的名字提供两项导航（都是纯客户端，读 schema 树已加载的元数据）：

- **悬停文档**：鼠标在 `employees`、`e.name`、`public.employees` 这类名字上停一下，弹出它是什么：
  - 表 / 视图：类型（table/view）、限定名、注释（数据库给了才有）、列数。
  - 列：`schema.table.column`、类型、`NULL`/`NOT NULL`、`PRIMARY KEY`、`AUTO INCREMENT`、
    `DEFAULT ...`、注释。
- **转到定义**：光标在该名字上按 `F12`（或 `Cmd`/`Ctrl` + 单击），打开这张表 / 视图的
  **DDL 查看器**；名字是列时打开它所属的表。

## 查找用法（Find Usages）

schema 树中右键一张表（或一列），`Find Usages` 找出**谁引用了它**：哪些表的外键指向这张表
（或这一列），结果出现在结果页签里，可以像任何查询结果一样排序、筛选、导出。

- 走内核 `ListForeignKeys` 逐表读取（**只看树已加载的 schema**——没人展开过的 schema 没有
  外键缓存，结果状态里报告检查了多少张表，不假装扫过整个目录）。
- 表的用法行给出：引用表、外键名、被引用的列、引用表自己的列；列的用法只留指向该列的键。
- 目标表自己不算自己的用法；"没有任何引用"也是一条结果（`0 usage(s) across N table(s)`
  的页签），不是沉默。
- 某张表的外键读不出来时，错误收进该页签的详情，不中断其余表的检查。

取词规则：字符串 `'...'` 与注释 `--` / `/* */` 里的名字**不解析**（那是值，不是对象）；
`"…"`、`` `…` ``、`[…]` 三种引号标识符都认（解开引号后按库里存的名字匹配）；
`.号`限定名整段解析，别名的解析复用补全引擎（`FROM employees e` 之后 `e.name` 指向 `employees`），
所以悬停与补全给出的答案一致。裸列名只在「当前语句的 FROM/JOIN 表里唯一」时才解析——
`JOIN` 两侧都有 `id` 时不猜。

## 对象搜索（schema 树搜索框）

schema 树上方有搜索框：输入片段（≥1 字符）即向**数据库目录**查询名字包含该片段的表/视图，
结果列在树的位置上，点一行直接浏览该表的数据。

- 查询走内核 `sqltext.SearchObjectsQuery`，6 方言各自正确（绑定参数 vs 内联字面量、
  PostgreSQL 的 `ILIKE`、SQLite 的 `ESCAPE '\'`），**上限 50 行**。
- 片段按**字面文本**处理：`a_b` 里的 `_` 不是通配符（内核的 `LikePattern` 转义了 `% _ \`）。
- 它问的是目录而不是树，所以**能找到树里从没展开过的对象**——这是与"过滤已加载节点"的区别。
- 框清空即恢复树；`×` 也可清空。每次搜索带一个代号，慢的旧结果不会覆盖新结果；
  换连接时搜索状态一并清空。

## DDL 查看器

在 schema 树中右键一张表，`View DDL` 打开 DDL 查看器：以只读编辑器显示创建该对象的
语句，可以直接复制，或 `Generate INSERTs` 把表的列渲染成一条 `INSERT` 追加到 DDL 之后。

- **表**走驱动的 `CreateTableSQL`，即数据库自己保存的建表语句。
- **视图**没有单独的建表语句，改向内核的 `ExtraMetadataReader.ViewDefinition`
  取定义（`SELECT` 语句体），再用 `sqltext.ComposeViewDDL` 包成 `CREATE OR REPLACE VIEW`。
  驱动不实现该接口时会给出说明。
- 查看器在等待读回时按钮不可用，出错时在编辑器上方显示红色原因。
- 打开查看器时再打开另一个表的 DDL，前一个读回会被丢弃。

## 对象树广度

schema 树不只是表与列：每个 schema 还能展开出它的**表外对象**，每张表还能展开出
它的**表内对象**。这些都是从内核的 `ExtraMetadataReader` 与 `SQLDriver` 的结构化读取
懒加载的，谁点开谁才去查。

- **schema 下**：`Sequences`（序列）、`Functions`（函数 / 存储过程）、`Enums`（用户定义
  枚举类型，仅 PostgreSQL）。
- **表下**：`Indexes`（索引）、`Foreign Keys`（外键）、`CHECK Constraints`（检查约束）、
  `Partitions`（分区）、`Triggers`（触发器）。主键不算二级索引，不会混进 `Indexes`。
- 每个分节只在展开时读一次并缓存；**驱动不支持该概念时整个分节自动隐藏**——例如
  SQLite 没有序列、例程、枚举、分区，也没有 CHECK 约束目录，所以这些节不会出现，
  也不会显示成空的树桩。
- 双击（或右键 `View Definition`）一个对象叶子，就用 DDL 查看器展示它的重建语句：
  索引渲染成 `CREATE INDEX`，外键渲染成 `ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY`，
  CHECK 渲染成 `ADD CONSTRAINT ... CHECK`；序列、枚举、分区、函数/过程、触发器直接展示
  引擎保存的定义。
- **触发器**走内核单独的 `TriggerLister` 能力（不是 `ExtraMetadataReader`）：几乎每个引擎
  都有触发器，但没有两个目录的形状相同（SQLite 的 `sqlite_master`、MySQL 的
  `information_schema.TRIGGERS`、PostgreSQL 的 `pg_trigger`、SQL Server 的
  `sys.triggers`、Oracle 的 `user_triggers`），配方在 `sqltext/triggers.go`，驱动只执行与映射。
  定义用引擎能给到的最完整形式：SQLite/SQL Server/PostgreSQL 是完整 `CREATE TRIGGER`，
  MySQL 是 `SHOW CREATE TRIGGER`，Oracle 是描述 + 触发器体。
  索引/外键的渲染复用内核 `dbcore/ddl` 的方言感知生成器。

## 单元格值编辑器

网格单元格只显示一行：长文本、多行文本、BLOB 都看不全。双击单元格（或右键
`Edit Value…`）打开值编辑器，把整个值放进只读编辑器里看完整内容。

- **工作副本**（浏览表 / `SELECT * FROM <表>`）：编辑器可编辑，`Apply` 把文本写回该行
  该列并把行标记为已修改，改动随 `Commit` 一起提交。
- **只读结果**（任意 `SELECT`）：编辑器为只读，`Apply` 禁用，只用来查看。
- **BLOB**：编辑器默认显示十六进制，`Hex` / `Text` 切换；写回十六进制时会校验
  文本是否为合法十六进制，否则拒绝并提示，避免把不该是字节的内容当成字节提交。
- `Copy` 把编辑器里的完整文本复制到剪贴板（网格里一行显示不全的值，复制出来是全文）。

## 刷新 / 粘贴 / 删除行

结果网格上方的 `Refresh` 重新读取当前结果集：浏览表重新 `BrowseRows`，语句重新执行。
工作副本有未提交改动时，`Refresh` 会先确认是否丢弃。

- **粘贴单元格**：右键单元格 → `Paste`，把剪贴板文本写进该格并标记该行。BLOB 单元格
  同样只接受十六进制。
- **删除行**：右键单元格 → `Delete Row`，行首显示 `–` 并带删除线，提交时才真正删除；
  新插入的行（`+`）删除时会直接从工作副本移除，因为它还不存在。
- 粘贴与删除都会让工作副本变脏，`Commit` / `Rollback` 与编辑单元格时一致。

## 取消执行 与 刷新 schema

- **取消执行（Stop）**：查询在跑时工具栏出现 `Stop`，点它取消正在执行的语句。取消走
  context：内核的 `QueryContext(ctx)` 把它交给驱动（`go-sql-driver`/`pgx`/`modernc` 都实现了
  中断），被取消的页签标 `Cancelled` 而不是失败——用户要的就是停。5 分钟超时仍是兜底保险丝。
- **每次都重新跑会覆盖前一次的结果吗？** 不会：每次执行有一个代号（generation），被取代的
  运行结果迟到时丢弃，所以连点 Run 或取消后立刻重跑，屏上永远是最后那次的结果。
- **刷新 schema**：`Database → Refresh Schema`（`Ctrl+F5`）丢弃全部目录缓存——表、列、对象分节
  （索引/外键/触发器/序列…）——并重新加载。在别的客户端改了表结构、或跑了迁移脚本之后用它；
  已在途的加载会因代号前进而丢弃，不会把旧答案写回新缓存。连接、编辑器页签、结果集和事务
  都不受影响。

## 连接级事务

工具栏的 **Autocommit** 开关控制编辑器里的语句是否自动提交。默认（开）与数据库一致，
每条语句各自提交；关掉后，编辑器脚本的语句在**一条事务**里执行，跑完事务仍然开着，
可以手动结束：

- **打开 Autocommit**：工具栏出现开关，编辑器脚本的语句走连接池，各自提交。
  没有 SQL 的连接（Redis）不显示开关。
- **关闭 Autocommit**：脚本的语句进入内核的 `TxManager` 事务。工具栏出现
  `Commit Tx` / `Rollback Tx`，状态栏显示 `In transaction`。
  - `Commit Tx` 结束事务并让改动对其他会话可见；`Rollback Tx` 丢弃事务里的改动。
  - 再打开 Autocommit 时，**未结事务会自动提交**——重新打开自动提交意味着用户已经
    决定这批工作完成了。
  - 状态栏同时显示当前事务与编辑工作副本的状态，例如 `In transaction · Editing employees (unsaved)`。
- 事务的边界是**连接**，不是标签页：同一条连接上，编辑器的脚本共用这条事务。
- **与网格工作副本的关系**：网格的 `Commit` 走内核的批量写入器，它自己开一条事务。
  因此当连接级事务还开着时，网格的 `Commit` 会被拒绝（状态栏给出原因），先结束连接级
  事务再提交网格——否则改动会一半进事务、一半进池，破坏原子性。
- 断开连接会回滚并释放该连接上未结的事务。
- **隔离级别**：Autocommit 关掉后，开关旁出现隔离级别下拉（`default` / `read_uncommitted` /
  `read_committed` / `repeatable_read` / `serializable`），决定**下一次**开启事务时向引擎要的
  级别（透传内核 `TxMode.IsolationLevel`）。引擎不支持某个级别的错误由引擎给出，客户端不猜。

## 连接健康

工具栏右侧的圆点显示连接状态：**灰**＝尚未探测、**绿**＝最近一次应答、**红**＝探测失败。
后台每 30 秒对驱动 `Ping` 一次（5 秒超时），点圆点可立即探测。

- 为什么需要它：空闲连接会被操作系统或对端静默丢弃（休眠、切换网络、服务器回收会话），
  没有探测时故障只在下一条语句以一条费解的错误暴露出来。
- 探测带代号：慢的旧探测不会覆盖新连接的结论；换连接或断开时状态与代号一并重置。
- Ping 从不占语句路径：它不阻塞主线程，也不影响正在执行的查询。

## SSH 隧道

生产库常常只在私网里，要经一台堡垒机才够得着。连接表单里的 **SSH tunnel** 一节就是这件事：

| 字段 | 说明 |
|---|---|
| SSH host / Port | 堡垒机地址，端口默认 22 |
| SSH user | 登录堡垒机的用户 |
| Password | 堡垒机密码（与数据库密码一样**静态加密**存储） |
| Key file | 私钥路径（支持 `~`），**给了私钥就优先用私钥**，等同于 ssh 的行为 |
| Key passphrase | 私钥口令（同样加密存储） |

- 隧道由**内核的 `Dialer`** 承载：MySQL 注册 driver 级 dialer、PostgreSQL 用 pgx 的 DialFunc，
  所以**池里每一条物理连接都走隧道**，不是只代理第一次连接。MSSQL/Oracle 的驱动没有拨号钩子，
  内核明确拒绝自定义 dialer 并说明原因，不会静默直连到错误的主机。
- **主机密钥要校验**，不是无条件信任：默认读 `~/.ssh/known_hosts`（没有就用应用自己的
  `ssh_known_hosts`）。未知主机会**拒绝连接并在错误里给出 SHA256 指纹**——因为"信任任何主机密钥"
  恰好放过了堡垒机本要防的攻击。确认指纹可信后，把该主机加进 known_hosts 即可；**已知主机的
  密钥变了则永远拒绝**，这一条不可绕过。
- 隧道随连接关闭（`DB.Close` 先关驱动再关隧道，否则池自己的关闭会在死通道上失败）。
- 错误的凭据、够不到的堡垒机都会让连接失败，并带上隧道自己的原因，而不是退化成本地直连。

## 数据导入（CSV / TSV → 表）

schema 树中右键一张表，`Import Data…` 把一个分隔文本文件（`.csv` / `.tsv` / `.tab` / `.txt`）
的行插入这张表。

- 文件用系统**文件对话框**选择；分隔符按扩展名判断（`.tsv`/`.tab` 是制表符，其余逗号）。
- 弹窗先给出**列映射**：文件的每一列一个下拉框，选它写入表的哪一列，或 `(skip)` 跳过。
  文件首行若是表头（弹窗按"首行没有纯数字"猜测，可改），**名字与表列相同的列自动映射**；
  否则全部未映射，由你逐列指定。
- 选项：**First row is the header**（首行是否表头，改动会重排映射标签）、
  **Clear the table first**（先清空表；清空在独立的批次里先行，失败则什么都不改）。
- 写入走内核 `BatchWriter.ApplyBatch`：每 **500 行一个批次**，一个批次一个事务。
  某批失败则停止，弹窗报告 `Batch N failed after X rows` —— 已写入的批次保留，
  不假装整个文件是原子的（一个百万行的文件不是一个小事务）。
- 单元格按**目标列的类型**解析（`"42"` 进 INTEGER 列是数字、进 TEXT 列是文本），
  空单元格写 NULL；短行的缺失单元格也是 NULL。超长单元格（>4096 字符）被截断，
  与网格自己的显示规则一致。
- 完成后状态栏报告行数；若网格正显示这张表，自动重读。

## 连接信息 与 不支持能力的解释

- **连接信息**：`Database → Connection Info…`（`Ctrl+I`），或右键工具栏的连接圆点。面板列出这条
  连接**实际具备**的能力——SQL/事务/批量写入/单行写回/扩展元数据（视图·CHECK·序列·例程·分区·枚举）/
  触发器目录/目录批量读取/对象健康/服务器指标/流式结果/KV 浏览，以及指向的文件或 `host:port/db`。
  每行都来自内核的能力断言（`AsSQLTx`/`AsBatchWriter`/`AsExtraMetadata`/…），**与工具栏和树用的
  是同一个判断**，所以面板与实际行为不会互相矛盾。缺一项不是故障：界面只是不显示需要它的功能。
- **不支持能力的解释**：对象分节在引擎没有该概念时会消失（SQLite 没有序列/例程/枚举/分区/CHECK 目录）。
  消失时状态栏会说明原因，例如 `SQLite has no standalone sequences, so the section is hidden`——
  免得用户把"没有"当成"坏了"。措辞只陈述概念，不承诺版本；引擎哪天支持了，那次读取就会成功，
  分节自然出现。

## 目录加载：单飞、超时与重试

树的每个分支按需加载（打开才问），有三条规则值得知道：

- **单飞**：一个分支的加载在途时，后续请求直接返回。树的子节点每帧都会问一次，而缓存
  只有在答案到达后才变——没有这条守卫，一个慢查询会被**每帧重发**，把服务器和连接池淹掉，
  最终所有请求一起超时（这正是「展开后报 `context deadline exceeded`」的放大器）。
- **超时 30 秒**：`metadataTimeout`。目录读没有中间结果可显示，等不到就只能报错。
- **错误归树**：加载失败显示在**侧栏树下方**（红色一行 + `Retry`），不进结果区——一次
  目录读超时不是一次查询失败，混在结果里会误导。错误信息带上对象名（`list tables in "gs_prod"`），
  `Retry` 会用同一次加载再问一遍；对象分节的重试还会清掉失败时写下的缓存标记，
  确保真的重新询问服务器而不是读旧的"空答案"。

> MySQL 的 `information_schema.TABLES` 里 `TABLE_ROWS` 会让服务器收集 InnoDB 统计
> （逐表打开），大 schema 上能慢到超时；客户端也用不到这个估计值，所以内核的
> `CatalogTables` 不再查它（想按需取行数用 `TableRows`，代价限定在单表）。

## 选择 schema（工作 schema）

侧栏 `Schema` 标题下是**工作 schema 下拉**：`(all schemas)` 或某个 schema 名。它决定：

- **对象搜索的范围**：选 `(all schemas)` 时搜索**所有** schema（MySQL 下即
  `information_schema.TABLES` 不带 `TABLE_SCHEMA` 过滤）；选具体 schema 时只搜它。
  此前搜索被钉在“第一个 schema”上，其余库里的表根本搜不到。
- **裸表名的归属**：`SELECT * FROM t` 的编辑判定、生成 INSERT/UPDATE 用的 schema、
  DDL 默认值都取工作 schema（未选时取第一个 namespace）。
- **连接的默认库/`search_path`**：对 MySQL（DSN 的 `dbname`）、PostgreSQL（DSN 的
  `search_path`）、SQL Server（`database`）而言，默认 schema 是**连接级**的——池里每个
  连接都要带上它，没有别的办法让它在一条语句里生效。所以切换 schema 会**重连**（状态栏
  显示 `Working schema: …`），未结事务随之结束——和 DataGrip 切换会话 schema 的语义一致。
  SQLite（文件即库）与 Oracle（schema 即用户）不重连，只作用于搜索与默认值。

## 对象搜索

侧栏 `Search objects…` 按名字搜表/视图（`sqltext.SearchObjectsQuery`），回车触发：

- 范围由上方的**工作 schema** 决定（见上一节）：不选=全库，选了=该库。
- 结果可点开（跳到树里对应表）或右键浏览数据；`搜索为空` 与 `搜索出错` 分别显示。
- 输入清空即恢复树。

## 查询历史

执行过的每条语句都会记下来（无论成功与否），按**最近在前**显示在左侧的 `History`
区，并保存在连接文件旁边的 `*.history.json` 中，下次启动仍在。

- 左侧点击某条历史，把这条语句的 SQL 重新载入编辑器。
- 失败的语句标有红色 `!`，点开即可看到完整报错。
- 每条显示执行时间（`HH:MM:SS`）和语句首行，超过宽度截断为 `…`。
- 列表最多保留 200 条；空语句不记录。
- 菜单 `Database` → `Clear History`，或 `History` 标题下的 `Clear`，清空全部历史。

## 编辑器自动补全

编辑器按单词和上下文补全 SQL。输入字母、数字、`_`、`.`、`:`、`>` 时自动弹出，
`Ctrl+Space` 强制弹出；方向键选择，`Enter` / `Tab` 插入。

补全内容来自三个地方：

- **SQL 关键字 / 函数 / 类型**：`complete.go` 中的词表（五种引擎通用的子集）。
- **连接元数据**：schema 树里出现过的 schema、表、列名。点开 schema 加载表、点开表加载列之后，
  这些名字才进入补全候选——和 DataGrip 一样，先看到、才补全，不会每次按键都查数据库。
- **当前浏览的表**：如果语句里没有 `FROM`（例如只想给某张表的列补全），就用编辑器正在浏览
  的那张表的列。

上下文会收窄候选：

- 读光标前最近的子句关键字。`FROM` / `JOIN` / `UPDATE` / `DELETE` 之后给出**表**（以及
  `INNER`、`LEFT`、`ON` 等连接关键字）；`WHERE` / `SELECT` / `GROUP BY` / `ON` 之后给出
  **列**，并混入关键字与函数。
- `表名.` / `别名.` 之后只给出该表的**列**；`schema.` 之后给出该 schema 的**表**。
  `FROM` / `JOIN` 里出现的表名与别名都会被记住，`a.` 能补 `FROM t a` 中 `a` 的列。
- 字符串、引号标识符、`--` 与 `/* */` 注释里的文字先被清空再判断，避免把
  `WHERE` 这样的词当成子句关键字。

列表项带类型图标（关键字 `k`、函数 `ƒ`、表 `T`、列 `·`、schema `{}`）、标签、
细节（列类型、schema 名、`()`）和文档行（所属表）。列候选最多 200 个，避免宽 schema 下
每次按键都拼出上千项。

## 支持的数据库

通过本地的 [`dbcore`](../dbcore) 引擎层，全部使用纯 Go 驱动（`CGO_ENABLED=0`）：

| 类型 | 驱动 |
| --- | --- |
| SQLite | `modernc.org/sqlite` |
| MySQL | `github.com/go-sql-driver/mysql` |
| PostgreSQL | `github.com/jackc/pgx/v5` |
| SQL Server | `github.com/microsoft/go-mssqldb` |
| Oracle | `github.com/sijms/go-ora/v2` |
| Redis | `github.com/redis/go-redis/v9` |

## 密码加密存储

保存的连接密码**不以明文落盘**：`connections.json` 里的 `password` 字段是
`enc:v1:<base64>` 形式的密文（AES-256-GCM），密钥由旁边的 `keyring.bin` 管理
（AES-256-GCM 包裹的 DEK，包裹密钥经 scrypt 从文件内的随机种子派生，权限 0600）。

- 调用方无感：连接表单与 `OpenDB` 照旧拿**明文** `Conn.Password`，加解密封在 store
  的文件读写处——保存前加密、加载后解密。
- **旧文件无迁移步骤**：老版本存的明文密码照常读出，下一次保存该文件时自动变成密文。
- **篡改可检出**：密文带认证标签，被改过的 password 解密失败时**丢弃**（连接保留，
  密码置空重新输入），不会解出一个"看起来对"的假密码。
- **威胁模型的诚实边界**（写在 `internal/keyring` 的文档里）：单机工具没有用户口令，
  种子在文件内，所以防的是「JSON 单独被复制/分享/进日志」与「文件被篡改静默生效」；
  防不了同用户的攻击者——机器上没有任何东西能防这个。
- `keyring.bin` 丢失（只备份了 JSON）时，旧密文打不开，密码同样被丢弃，其余连接信息
  完好。

## 构建

```powershell
.\build.cmd build      # 开发版 -> build\dbstudio.exe（保留 inspector，同 mygo dev）
.\build.cmd run        # 编译后直接启动，窗口关掉才返回
.\build.cmd check      # 验收块：gofmt -l / go build / go vet / go test
.\build.cmd release    # 生产版 -> dist\dbstudio-<版本>-<os>-amd64[.exe] + .sha256
.\build.cmd smoke      # 编译后启动 8 秒，确认存活且 stderr 为空，再杀掉
.\build.cmd all        # check 然后 release
.\build.cmd clean      # 删掉 build\ 与 dist\
```

PS 脚本本身是 `build.ps1`（`build.cmd` 只是绕过执行策略的入口）：

```powershell
.\build.ps1 release -Target all -Version 0.2.0     # 三平台产物
.\build.ps1 smoke -SmokeSeconds 15
```

脚本做的事与注意事项：

- **环境固定到仓库自己的缓存**（`.gopath` / `.gocache` / `.gotmp`，与历次验收一致），
  不会另下载一套 toolchain，也不去填全局缓存；要改用环境里现成的 Go 环境，设
  `DBSTUDIO_GO_ENV=0`。
- `CGO_ENABLED=0` 固定：mygo 硬性禁 cgo。因此**不开 `-race`**（Windows 上竞态检测器需要 C 工具链）。
- 生产构建是 `-trimpath -tags mygo_noinspector -ldflags "-s -w -X github.com/egoist/mygo.production=1"`：
  产物不含本机源码路径，体积从开发版约 49 MB 降到约 37 MB，并且关掉 mygo 的开发模式（默认开 web inspector）。
- `release -Target all` 交叉编译 windows/linux/darwin；**非 Windows 产物只能打包到对应机器运行**，本机验证不了。
- 每个动作结束时打印分步耗时；任一步失败即以非零码退出（可直接接 CI）。
- `dist\*.exe.sha256` 是 `<sha256>  <文件名>` 的 GNU coreutils 格式，`sha256sum -c` 可直接校验。

### 手工构建（不经脚本）

脚本只是把下面的环境固定下来；等价的手工命令是：

```powershell
# 在项目目录下
$env:GOPATH='D:\tools\db\.gopath'
$env:GOMODCACHE='D:\tools\db\.gopath\pkg\mod'
$env:GOCACHE='D:\tools\db\.gocache'
$env:GOTMPDIR='D:\tools\db\.gotmp'
$env:CGO_ENABLED='0'

go run .                                          # 直接运行
go build -o dbstudio.exe .                        # 开发版
go build -trimpath -tags mygo_noinspector `
  -ldflags "-s -w -X github.com/egoist/mygo.production=1" -o dbstudio.exe .   # 生产版
```

## 首次使用

1. 生成示例库（可选，已附带 `demo.db`）：

   ```powershell
   go run ./cmd/demodb demo.db
   ```

   示例库含两张表（`departments`、`employees`，`employees` 有一列 `BLOB`）、
   一个视图 `engineering_payroll` 和一个索引，可以试 DDL 查看器与值编辑器。

2. 点击左侧 `New`（或菜单 Database → New Connection）。
3. 填名称、类型与连接信息。例如 SQLite：
   - Name：`local`，Kind：`sqlite`，File：`D:\tools\db\dbstudio\demo.db`
   （`:memory:` 也可，但每次重开为空库）。
4. 保存后自动连接，展开 `Schema` 树即可看到表；点表填入查询，点 Run 执行。
5. 结果表格中的 `Export`（右键选 CSV / JSON / INSERT）把当前结果导出到连接文件同目录。

连接信息保存在 `mygo` 用户数据目录下的 `connections.json`
（`mygo.App.Path(mygo.PathUserData)`），密码也在其中——请不要把该文件提交到版本库。

## 代码结构

| 文件 | 职责 |
| --- | --- |
| `app.go` | `App` 结构（窗口的全部状态）与各面板共用的 builder（模态编辑器、消息清理等） |
| `view.go` | 面板装配：`view` 组装工具栏/侧栏/编辑器/结果四区，网格渲染、行标记、消息面板 |
| `menu.go` | 菜单栏：`Database` 与 `View` 菜单到 App 方法的接线 |
| `main.go` | 入口：构建 `App`、开窗、接菜单、连接上次活动的连接 |
| `store.go` | 连接与查询历史的持久化（`connections.json`、`*.history.json`）与字段规范化 |
| `db.go` | 打开 `dbcore` 连接（含每连接 `TxManager`）、schema/表/列的元数据缓存 |
| `catalog.go` | 元数据屏障：schemas/tables/cols 的选择子、显式三态加载状态（NotLoaded/Empty/Loaded）、键编码内化、跨 schema 裸名查找、连接切换失效 |
| `tree.go` | schema 树的展开与异步加载，表/列节点 |
| `query.go` | 连接 / 断开、语句切分与执行、数据浏览 |
| `export.go` | 导出文件：CSV / JSON / INSERT 三格式，工作副本折叠、JSON 序列化、INSERT 脚本 |
| `explain.go` | 执行计划：光标/选区语句 → `ExplainPlanStructured` → 结果页签展示，会话式引擎说明 |
| `format.go` | SQL 格式化入口：整段 / 选区原地格式化编辑器内容 |
| `params.go` | 参数化查询面板：占位符提取（?/$n/:name，跳过字符串注释 cast）、方言改写、值绑定执行 |
| `compare.go` | 结构比较：双连接 `schemadiff.Collect/Diff/Script`、条目与告警展示、迁移脚本预览与逐条应用 |
| `nav.go` | 悬停文档与转到定义：指针下名字链取词（跳过引号字符串/注释）、表/视图/列/别名解析、悬停文本、开 DDL |
| `usages.go` | 查找用法：走已加载表的外键找引用者，结果进只读页签，覆盖数如实报告 |
| `consoles.go` | 编辑器多页签：每页签一个缓冲区，切换换入 `edBuf`/`edState`，按内容自动命名 |
| `refresh.go` | 刷新 schema：`treeGen` 前进 + 清目录缓存 + 重载首个 schema 的表 |
| `schema.go` | 工作 schema：选择器、作用域（搜索/默认值）、MySQL/PG 下重连带默认库 |
| `layout.go` | 界面骨架：工具栏（只改状态）、导航器、状态栏（只报状态）、面板标题与空状态 |
| `guard.go` | 破坏性语句守卫：DROP/TRUNCATE/无 WHERE 的 DELETE·UPDATE 先确认，一次提问只问一次 |
| `session.go` | 会话恢复：consoles 与 SQL、当前页签、工作 schema、分栏、窗口尺寸的读写与应用 |
| `formtest.go` | 连接表单的 Test Connection：真连一次、回报引擎与耗时、不保存 |
| `columns.go` | 列的隐藏与重排：每页签的 colOrder/colHidden、可见列映射、表头菜单、Columns 对话框、与框架 Columns.Order 双向同步 |
| `estimate.go` | 大表行数估算：列表估算优先、RowEstimator 按单表补、缺失不编造、分页器展示；列宽变化检测与落盘 |
| `readonly.go` | 只读锁：写入语句分类（含 WITH 修饰 CTE）、执行前拒绝、工具栏开关、随连接持久化 |
| `conncolor.go` | 连接颜色：五色调色板、连接标记（■ + 名 + 🔒）、导航器与状态栏着色 |
| `health.go` | 连接健康：每 30s `driver.Ping`、代号防迟到、工具栏圆点与提示 |
| `info.go` | 连接信息面板：能力矩阵（读内核 `As*` 断言）与地址，只读模态 |
| `explain_unsupported.go` | 不支持能力的分节在消失时给出一句解释（引擎名 + 概念名） |
| `build.ps1` / `build.cmd` | 构建脚本：build/run/release/check/test/smoke/clean/all，固定仓库内 Go 缓存，release 出三平台产物 + sha256 |
| `search.go` | 对象搜索：`sqltext.SearchObjectsQuery` 问目录、结果列表（`ui.Sidebar`）、代号丢弃旧答案 |
| `import.go` | 数据导入：CSV/TSV 解析、列映射、分批 `ApplyBatch` 写入、文件对话框与模态 |
| `trigger_test.go` | 触发器分节测试（叶子加载、空表空节、定义含 CREATE TRIGGER、树中出现、分节表级定位） |
| `internal/tunnel` | SSH 隧道：x/crypto/ssh 客户端、known_hosts 主机密钥校验（未知拒绝并报指纹、已变更拒绝）、按需信任并记录、内核 Dialer 适配 |
| `internal/xlsx` | 零依赖 xlsx 写入器：单表工作簿、加粗冻结表头、按内容列宽、数值/文本判定（前导零与超 15 位按文本）、XML 转义 |
| `internal/sqlformat` | 独立无依赖 SQL 美化器：token 化、关键字大写、子句缩进换行、引号与注释跳过 |
| `internal/sqlcodec` | 值↔文本编码单点：SQL 字面量、单元格文本、编辑文本回值、JSON 值、类型名猜测、blob 十六进制——同一数据在所有出口形态一致 |
| `internal/keyring` | 密码静态加密：AES-256-GCM + scrypt 的 `keyring.bin`，connections.json 里密码只以 `enc:v1:…` 密文落盘 |
| `form.go` | 连接编辑弹窗（`ui.Modal` + `ui.Form`） |
| `edit.go` | 可编辑结果网格的工作副本：改动追踪、批量提交、回滚、类型解析 |
| `pager.go` | 结果分页：页窗口、页数、翻页与页大小选择、`Alt+←/→` 快捷键、跳转页弹窗 |
| `tabs.go` | 结果面板的标签页：一个结果集一个标签，保存/恢复网格与分页状态 |
| `sort.go` | 网格列排序：客户端排序比较、列序保持 |
| `filter.go` | 网格行筛选：条件栏、`FilterExpr` 构造、`LIKE` 匹配、服务端/客户端两条路径 |
| `copy.go` | 复制网格为 SQL / CSV：语句渲染、方言标识符与字面量、剪贴板 |
| `ddl.go` | DDL 查看器：`CreateTableSQL` 与视图 `ViewDefinition`，树右键菜单、模态查看与 INSERT 生成 |
| `objects.go` | 对象树广度：schema/表下的对象分节（索引/外键/CHECK/分区/序列/函数/枚举）懒加载、`ExtraMetadataReader` 断言、各类对象定义渲染 |
| `celledit.go` | 单元格值编辑器：打开单元格、BLOB 十六进制/文本、写回工作副本、粘贴与刷新 |
| `tx.go` | 连接级事务：每连接 `TxManager`、Autocommit 开关、`Commit Tx`/`Rollback Tx`、脚本路由进事务 |
| `complete.go` | 编辑器 SQL 补全：关键字/函数/类型词表、schema/表/列候选、子句与点号上下文 |
| `history.go` | 查询历史面板：条目标签、载入到编辑器、清空历史 |
| `main_test.go` | 针对真实 SQLite 的冒烟测试（打开、列元数据、执行、浏览） |
| `edit_test.go` | 工作副本的逻辑测试（单元格解析、批量请求、提交往返、只读判定） |
| `pager_test.go` | 分页器的单元测试（页窗口、页数、翻页、页大小、服务端真实翻页） |
| `sort_test.go` | 排序的单元测试（比较、列序保持） |
| `filter_test.go` | 筛选的逻辑测试（条件构造、`LIKE` 匹配、客户端筛选、服务端 `BrowseRows` 筛选） |
| `copy_test.go` | 复制为 SQL/CSV 的单元测试与真实库语句往返（`INSERT`/`UPDATE`/`DELETE` 执行验证） |
| `complete_test.go` | 补全引擎的逻辑测试（词法剥离、子句上下文、点号限定、别名解析） |
| `messages_test.go` | 消息面板的条目构造与清空 |
| `history_test.go` | 查询历史的条目构造、排序、上限、落盘与载入 |
| `p1_test.go` | DDL 查看器与单元格值编辑器的真库/模态渲染测试（表/视图 DDL、编辑、粘贴、删除、刷新） |
| `tx_test.go` | 连接级事务的真库测试（注册表可用、Begin 幂等、提交可见、回滚丢弃、autocommit 开时绕行、未知事务报错、切回自动提交、事务打开时拒绝提交工作副本） |
| `export_test.go` | 导出格式测试（JSON 类型形状、空结果、INSERT 文本与需表守卫、工作副本折叠、类型行保留） |
| `explain_test.go` | 执行计划测试（sessionless 可解释、SQLite/PG 单语句、PG JSON、SQL Server 需会话、真库跑计划） |
| `format_test.go` | 格式化入口测试（整段、选区保留上下文、幂等） |
| `params_test.go` | 参数化测试（位置/自编号/命名提取、字符串注释 cast 跳过、方言改写、值解释、真库绑定执行） |
| `compare_test.go` | 结构比较测试（双库 diff 出行、脚本含 CREATE、应用后收敛为空） |
| `import_test.go` | 数据导入测试（分隔符判断、引号/短行解析、表头猜测、映射跳过、类型化插入与 NULL、真库写入读回、清空选项、超长截断、模态渲染） |
| `store_keyring_test.go` | 密码加密测试（落盘无明文有 enc:v1、API 仍拿明文、跨启动读回、旧明文自动迁移、篡改丢弃、无 keyring 丢密码、文件仍为合法 JSON） |
| `usages_test.go` | 查找用法测试（真库 FK 命中、无引用、列过滤、页签列名、空结果页签与状态） |
| `consoles_test.go` | 编辑器多页签测试（切换保文本、State 跟随、自动命名与截断、关闭与末页保护、标签唯一、上限、渲染） |
| `cancel_refresh_test.go` | 取消与刷新测试（代号丢弃迟到结果、取消状态非错误、句柄清理与空闲无操作、刷新清空表/列/对象、保留连接与页签、未连接提示、连刷两次） |
| `tree_error_test.go` | 树加载失败测试（错误归树不进结果区、保留可重试态、重试成功后清错并加载、断连清错、分节重试真的重新询问、单飞契约、60 帧不重发） |
| `schema_test.go` | 工作 schema 与列宽测试（选项/选择、搜索范围跟随、默认 schema 优先、连接级 schema 与重连、选择器渲染与过期回退、列宽按内容估算与夹取、表格列起始宽度） |
| `layout_test.go` | 界面布局测试（状态栏报告连接/schema/引擎与事务、未连接提示、导航器标题与空状态、结果区 Data/Messages 页签切换、空结果提示、工具栏只含动作且无健康点、导航器含 schema 与树根） |
| `multiset_test.go` | 多结果集测试（两集拆两页签且标题标 `2/2`、单集保持原形状、无多集能力时退回单集且不 panic、页签标题截断） |
| `loadmore_test.go` | 增量加载与窗口颜色测试（靠近末尾才取、追加续行、到末尾停止、脏副本停止、失败上报并停、上限停止并说明、标题与色带、会话同时保存布局与列宽） |
| `headerinfo_test.go` | 列信息与按表记忆测试（tooltip 内容与目录补齐、越界与 nil 不 panic、面板渲染、布局按表存取、查询结果不落盘、未变不重复写） |
| `columns_test.go` | 列隐藏/重排测试（默认全显、隐藏后的索引映射、重新显示归位、末列保护、移动与越界、每页签独立、Show All 保留顺序、未知列名的旧排列、对话框与表头菜单越界不 panic） |
| `estimate_test.go` | 行数与列宽测试（列表估算、驱动估算与缓存、未知不编造、分页器渲染 ~1.2M rows、列宽落盘/未变不写/恢复记为当前） |
| `readonly_test.go` | 只读与颜色测试（写入分类 13 种/只读 8 种、脚本拒绝且不触驱动、只读连接仍可读、网格不产生副本、提交拒绝、导入拒绝、开关持久化、连接时种入、调色板与标记、存取往返、状态栏渲染、表单选项） |
| `polish_test.go` | 打磨项测试（守卫分类与脚本级判定、WHERE 忽略字面量/注释、确认后只跑一次、安全语句不打扰、数值列对齐、会话往返/应用/损坏回退、Test 成功与失败、页签变动落盘、console 循环切换） |
| `tunnel_client_test.go` | 隧道客户端测试（SSH 密码/口令加密落盘与读回、旧文件无隧道、篡改丢弃、无隧道时 DB.Tunnel 为空、够不到的堡垒机报隧道原因、表单默认端口 22） |
| `export_excel_test.go` | Excel 导出测试（表头/数值/前导零文本/NULL 空白/转义、空结果仍有表头、格式标签与扩展名） |
| `health_test.go` | 隔离级别与健康探测测试（标签表、级别透传 `TxMode`、默认级别、无注册表无操作、探测落值、关闭连接不探测、迟到探测丢弃、圆点渲染与提示） |
| `info_test.go` | 连接信息与解释测试（引擎标签表、概念短语、解释句含引擎与概念、SQLite 隐藏序列时的状态栏、支持的分节不解释、SQLite 能力矩阵、无驱动守卫、未连接、面板渲染与重建） |
| `nav_test.go` | 悬停/转到定义测试（裸/限定/引号/字符串/注释取词、表/别名列/裸列解析、悬停文本、F12 开对 DDL） |
| `search_test.go` | 对象搜索测试（找表/视图、无匹配、片段字面性 `a_b` 不匹配 `axb`、清空、未连接、新搜索胜旧、点结果开浏览页签、配方契约） |
| `catalog_test.go` | 元数据屏障测试（三态加载状态、键不碰撞、TableType 回退、跨 schema 裸名查找按报序、SetSchemas/Reset 失效） |
| `cmd/demodb` | 生成用于试用的 SQLite 示例库 |

## 测试

```powershell
go test ./...
```

`TestOpenDBListAndQuery` 会真正打开一个 SQLite 文件，走一遍连接、加载 schema、
按序加载列、执行查询和 `BrowseRows`，确保 UI 之外的数据通路可用。
`TestCommitRoundTrip` 则建一张表、读出两行、改一行、删一行、插一行，作为一个批次提交，
再读回验证——即工具栏 `Commit` 走的那条路。

筛选与排序也各自有一条真实库路径：`TestBrowseRowsFilter` 用 `BrowseRows` 的 `Filter`
传条件给 SQLite 引擎验证；`TestSortLocally*` / `TestApplyGridClient*` 覆盖客户端排序与
筛选的重排。`TestCopyRoundTrip` 读出两行、渲染成 `INSERT` / `UPDATE` / `DELETE`
语句，逐条交给真实库执行并读回验证——即右键复制菜单走的那条路。

查询历史另有逻辑测试：`TestHistoryStore` 检查条目按最新在前、落到 `*.history.json`
并重新载入，以及清空后不复活；`TestHistoryStoreCaps` 验证最多 200 条、空语句不记录；
`TestHistoryLabel` 检查列表标签的时间、首行截断与失败标记；`TestHistoryEntry` 检查
执行一条语句（无论成败）都会留下记录。

DDL 与单元格值编辑器同样有真库用例：`TestReadDDLTable` / `TestReadDDLView` 打开 SQLite
文件，分别读出表的 `CreateTableSQL` 与视图的 `CREATE VIEW`；`TestOpenCellEditable` /
`TestApplyCellEditsWorkingCopy` 在工作副本上打开单元格、写入并校验行被标记修改；
`TestApplyCellBlobHex` / `TestApplyCellBlobRejectsBadHex` 覆盖 BLOB 的十六进制写回与
非法十六进制拒绝；`TestPasteCellText` / `TestDeleteRowAt` 覆盖粘贴与删除行；
`TestDDLModalRenders` / `TestCellModalRenders` 驱动框架 `Tester` 渲染模态，确认标题与按钮
在屏幕上。`TestOpenDDLWithoutConnection` 验证未连接时树菜单不会打开模态。

连接级事务用真实 SQLite 文件覆盖：`TestOpenDBBuildsTransactionRegistry` 确认 SQLite
是 `SQLTxDriver` 并建出注册表；`TestBeginTxIsIdempotent` 确认第二次 Begin 不开第二条事务；
`TestCommitTxMakesWorkVisible` 插入后先在事务内可见、事务外不可见，提交后事务外可见；
`TestRollbackTxDiscardsWork` 确认回滚后改动消失；`TestExecuteStatementAutocommitOnBypassesTx`
确认 Autocommit 开时语句绕过事务；`TestCommitTxUnknownIDReportsError` 确认未知事务报错；
`TestSetAutocommitCommitsOpenTx` 确认切回 Autocommit 会提交未结事务；
`TestCommitWorkingCopyRefusedWhileTxOpen` 确认事务开着时拒绝提交网格工作副本。

## 线程模型

视图函数 `view` 在主线程运行，绝不阻塞。所有数据库调用（连接、加载 schema、
执行查询、导出）都在后台 goroutine 中进行，结果通过 `win.Update(fn)` 回到主线程
再修改 UI 状态并 `Invalidate()`。因此任何面板都能安全读取这些状态字段。

查询历史也在执行语句的 goroutine 里写入（`Store` 自带互斥锁），不经过主线程，
所以不影响界面。

## 备注

- 补全的候选来自 schema 树已加载的元数据，点开表/列之后才会出现，不像 DataGrip
  那样按需向数据库查询。
- 结果分页：浏览表走服务端分页（每次一页 + 总数），任意 `SELECT` 走客户端分页；
  详见「结果分页」。
- 编辑仅限「浏览表」与 `SELECT * FROM <表>`；`JOIN`、聚合、投影等保持只读。
- 没有主键（或主键为 NULL）的行无法更新或删除，提交会提示原因。
- BLOB 在网格里显示为 `<blob N bytes>`；双击单元格打开值编辑器，可用十六进制或文本
  查看，写回时只接受十六进制（见「单元格值编辑器」）。
- Redis 通过 dbcore 的 KV 接口接入，`SQLDriver` 为 nil，因此 Redis 连接下
  查询面板不可用，工具栏也不显示 Autocommit 开关。
- 连接级事务由工具栏的 Autocommit 开关控制，`Commit Tx` / `Rollback Tx` 在事务打开时
  出现；详见「连接级事务」。
- 对象树广度：schema/表展开时的对象分节从内核 `ExtraMetadataReader`/`SQLDriver` 惰性
  读取，驱动不支持的概念分节自动隐藏；见「对象树广度」。
- 导出三格式：CSV/JSON 任意结果可导，INSERT 需结果是一张表的行；工作副本未提交的改动
  按所见导出；见「导出文件」。
- 执行计划：SQLite/PostgreSQL/MySQL 直接可解释；SQL Server/Oracle 需要同会话，本版
  给出说明而不误执行；见「执行计划」。
- 参数化：占位符按目标方言改写后由驱动**绑定**，不做转义拼接；空值绑 NULL、前导零文本
  保持文本；见「参数化查询」。
- 结构比较：比较两个**已保存连接**的首个 schema，应用脚本逐条前滚到失败点（DDL 隐式
  提交，不包事务）；`Warnings` 必读，空计划不等于无差异；见「结构比较」。
- 悬停与转到定义只解析 schema 树**已加载**的元数据（表/列展开过才有）；字符串与注释里的
  名字不解析，裸列不唯一时不猜；见「悬停文档与转到定义」。
- 排序与筛选：浏览表走服务端（`Sort` / `Filter` 传给驱动），任意 `SELECT` 在客户端
  重排；详见「网格排序」「网格行筛选」。
- 复制的 SQL 按连接方言渲染标识符与字面量；没有主键的表无法生成 `UPDATE` / `DELETE`。
- 查询历史保存在连接文件旁的 `*.history.json`，最多 200 条，点击可重新载入编辑器。
- 菜单、工具栏与 `Alt+←/→` 等快捷键的对照见上文；查询历史用 `Database` →
  `Clear History` 或左侧 `History` 的 `Clear` 清空。
