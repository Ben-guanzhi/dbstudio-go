package main

// The ER diagram's layout, apart from its drawing.
//
// A diagram is a graph: tables are boxes, foreign keys are edges. Drawing one
// well is a layout problem first — boxes that overlap or edges that cross at
// random angles make the diagram harder to read than the raw schema. This
// file assigns each table a box; the drawing comes after.
//
// The algorithm is the same left-to-right layered layout TableProGo's
// erLayout.ts and fynequery's erd.go use (ported, not re-derived): a table's
// column is one past the deepest table it references, so the referenced tables
// sit left of the tables that reference them; within a column a few
// barycenter sweeps pull connected tables level with each other. Minimizing
// crossings is NP-hard even to verify, so the sweep count is fixed: the point
// is determinism, not optimality.
//
// 波 101 续四十五.

import "sort"

// ER 图的几何常量。盒宽固定（列名长短不改变盒宽，长名截断）；行高固定。
const (
	erBoxW   float32 = 180
	erTitleH float32 = 24
	erRowH   float32 = 16
	erGap    float32 = 48
	erMaxTb          = 60 // 一张图最多画的表数：把取数的规模框住
)

// erLayoutSweeps 是 barycenter 扫描的固定次数。交叉最小化是 NP-hard（连验证
// 都是），所以固定次数本身就是重点：布局确定（与输入顺序无关）、复杂度
// O(扫描次数 × 边数)。探测收敛会把确定性换成在图的规模上量不出来的东西。
const erLayoutSweeps = 4

// erBox 是一个表盒子的位置与大小。
type erBox struct {
	X, Y, W, H float32
}

// erCol 是 ER 图里的一张表的列。
type erCol struct {
	Name string
	PK   bool // 主键，画的时候标出来
}

// erTable 是一个表节点：列、键，和布局给出的盒子。
type erTable struct {
	Name string
	Cols []erCol
	Box  erBox
}

// erFK 是一条外键边：From 表的 FromCol 指向 To 表的 ToCol。
type erFK struct {
	From, FromCol, To, ToCol string
}

// erModel 是一张图的全部输入与输出。
type erModel struct {
	Tables []erTable
	FKs    []erFK
}

// erBoxSize 是一个表盒子的大小：标题 + 每列一行，至少两行（空表也占住
// 一个能看清的盒子）。
func erBoxSize(t erTable) (w, h float32) {
	rows := len(t.Cols)
	if rows < 2 {
		rows = 2
	}
	return erBoxW, erTitleH + float32(rows)*erRowH
}

// layoutER 给每个表分配盒子：左到右分层 + barycenter 扫描 + 列内自上而下。
//
// 返回的模型与传入的是同一个（就地改写），方便调用方直接拿去画。
func (m *erModel) layoutER() {
	if len(m.Tables) == 0 {
		return
	}

	// 先固定 FK 的顺序：扫描要对邻居的行号求和，和不能依赖目录的行序。
	sort.SliceStable(m.FKs, func(i, j int) bool {
		a, b := m.FKs[i], m.FKs[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.FromCol != b.FromCol {
			return a.FromCol < b.FromCol
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.ToCol < b.ToCol
	})

	idx := make(map[string]int, len(m.Tables))
	for i := range m.Tables {
		idx[m.Tables[i].Name] = i
	}

	// 分层：DAG 最长路，带环守卫（深度超限或递归中重入都落到第 0 层）。
	level := make(map[string]int, len(m.Tables))
	visiting := make(map[string]bool, len(m.Tables))
	var levelOf func(name string, depth int) int
	levelOf = func(name string, depth int) int {
		if depth > 1000 {
			return 0
		}
		if l, ok := level[name]; ok {
			return l
		}
		if visiting[name] {
			return 0 // 环：断开它
		}
		visiting[name] = true
		l := 0
		for _, fk := range m.FKs {
			if fk.From != name || fk.To == name {
				continue
			}
			if _, ok := idx[fk.To]; !ok {
				continue // 跨 schema 的引用不在这张图里
			}
			if l2 := levelOf(fk.To, depth+1) + 1; l2 > l {
				l = l2
			}
		}
		visiting[name] = false
		level[name] = l
		return l
	}
	for _, t := range m.Tables {
		levelOf(t.Name, 0)
	}

	maxLevel := 0
	for _, t := range m.Tables {
		if l := level[t.Name]; l > maxLevel {
			maxLevel = l
		}
	}
	columns := make([][]int, maxLevel+1)
	for i := range m.Tables {
		columns[level[m.Tables[i].Name]] = append(columns[level[m.Tables[i].Name]], i)
	}
	// 列内先按字母序：码点序，不是区域序——每台机器上都逐字节相同。
	for _, col := range columns {
		sort.SliceStable(col, func(a, b int) bool {
			return m.Tables[col[a]].Name < m.Tables[col[b]].Name
		})
	}

	// 无向邻接（自引用对分层没有贡献）。
	neighbors := make(map[string][]string, len(m.Tables))
	for _, fk := range m.FKs {
		if fk.From == fk.To {
			continue
		}
		if _, ok := idx[fk.From]; !ok {
			continue
		}
		if _, ok := idx[fk.To]; !ok {
			continue
		}
		neighbors[fk.From] = append(neighbors[fk.From], fk.To)
		neighbors[fk.To] = append(neighbors[fk.To], fk.From)
	}

	// barycenter 扫描：每一遍按方向走各列，把每列按它在相邻列里的邻居的
	// 平均行号重排；方向交替，两侧的拉力都能落定。
	order := make([][]string, len(columns))
	for ci, col := range columns {
		for _, ti := range col {
			order[ci] = append(order[ci], m.Tables[ti].Name)
		}
	}
	for sweep := 0; sweep < erLayoutSweeps; sweep++ {
		forward := sweep%2 == 0
		cols := make([]int, len(order))
		for i := range cols {
			cols[i] = i
		}
		if !forward {
			for i, j := 0, len(cols)-1; i < j; i, j = i+1, j-1 {
				cols[i], cols[j] = cols[j], cols[i]
			}
		}
		for _, c := range cols {
			adj := c - 1
			if !forward {
				adj = c + 1
			}
			if adj < 0 || adj >= len(order) || len(order[c]) < 2 {
				continue
			}
			// 每个表当前的行号。每列重建：扫描就地改 order，后面的列要看到更新。
			pos := make(map[string]int, len(m.Tables))
			for _, col := range order {
				for i, name := range col {
					pos[name] = i
				}
			}
			type entry struct {
				name string
				key  float64
			}
			entries := make([]entry, len(order[c]))
			for i, name := range order[c] {
				sum, count := 0, 0
				for _, nb := range neighbors[name] {
					if level[nb] == adj {
						sum += pos[nb]
						count++
					}
				}
				// 相邻列里没有邻居的表保持原行号当排序键，孤立的表不会漂移。
				key := float64(i)
				if count > 0 {
					key = float64(sum) / float64(count)
				}
				entries[i] = entry{name: name, key: key}
			}
			sort.SliceStable(entries, func(a, b int) bool {
				if entries[a].key != entries[b].key {
					return entries[a].key < entries[b].key
				}
				return entries[a].name < entries[b].name
			})
			for i := range entries {
				order[c][i] = entries[i].name
			}
		}
	}

	// 定位：逐列，列内 y 累加。
	for ci, col := range order {
		y := erGap
		for _, name := range col {
			t := &m.Tables[idx[name]]
			w, h := erBoxSize(*t)
			t.Box = erBox{X: erGap + float32(ci)*(erBoxW+erGap), Y: y, W: w, H: h}
			y += h + erGap
		}
	}
}
