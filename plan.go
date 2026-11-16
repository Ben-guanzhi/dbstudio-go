package main

// 执行计划的树。
//
// SQLite 的 EXPLAIN QUERY PLAN 返回四列：id / parent / notused / detail——
// **树已经在了**（dbcore 的 sqltext.ExplainPlanStructured 就这么问的）。但
// 结果网格把它当普通表格平铺，父子关系只能靠用户自己读 id 和 parent 两列
// 猜。DBeaver / DataGrip 都把计划显示成树，因为「哪一步属于哪一步」是计划
// 唯一要紧的信息。
//
// 这里把 id/parent 换成缩进：每行按它到根的距离画树形前缀（└─ / ├─ / │），
// detail 列因此自己说出层级。id / parent / notused 三列不再显示——它们的
// 作用已经由缩进承载了。
//
// 波 101 续五十一。

import (
	"strconv"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
)

// intValue 取一个值的整数：id/parent 在 SQLite 里是整数，但别的引擎可能给
// 文本，所以两种都认。
func intValue(v dbcore.Value) int {
	switch v.Kind {
	case dbcore.ValInt:
		return int(v.I)
	case dbcore.ValFloat:
		return int(v.F)
	case dbcore.ValText:
		n, err := strconv.Atoi(strings.TrimSpace(v.S))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// isPlanResult 判断一个结果是不是执行计划：有 id 和 parent 两列（SQLite 的
// EXPLAIN QUERY PLAN 的形状）。
func isPlanResult(res *dbcore.QueryResult) bool {
	if res == nil {
		return false
	}
	var hasID, hasParent bool
	for _, c := range res.Columns {
		switch strings.ToLower(c.Name) {
		case "id":
			hasID = true
		case "parent":
			hasParent = true
		}
	}
	return hasID && hasParent
}

// planColumns 返回计划结果里 id / parent / detail 三列的下标（-1 表示没有）。
func planColumns(res *dbcore.QueryResult) (id, parent, detail int) {
	id, parent, detail = -1, -1, -1
	for i, c := range res.Columns {
		switch strings.ToLower(c.Name) {
		case "id":
			id = i
		case "parent":
			parent = i
		case "detail":
			detail = i
		}
	}
	return
}

// planTree 是一个计划结果的结构：每行的深度与树形前缀。
type planTree struct {
	depth  []int    // 每行到根的距离
	prefix []string // 每行祖先贡献的竖直段
	last   []bool   // 每行是不是它父的最后一个孩子（└─ 还是 ├─）
}

// buildPlanTree 从 id/parent 两列算出每行的深度与树形前缀。
//
// parent 指向本结果里另一行的 id；指向不存在的 id（或自指）时当作根——
// 计划的输出总是连通的，但一行坏数据不该让整张表画不出来。
func buildPlanTree(res *dbcore.QueryResult) *planTree {
	idCol, parentCol, _ := planColumns(res)
	if idCol < 0 || parentCol < 0 {
		return nil
	}
	n := len(res.Rows)
	ids := make([]int, n)
	parents := make([]int, n)
	for i, row := range res.Rows {
		ids[i] = intValue(valueAt(row, idCol))
		parents[i] = intValue(valueAt(row, parentCol))
	}
	byID := make(map[int]int, n) // id → 行号
	for i, id := range ids {
		byID[id] = i
	}

	t := &planTree{
		depth:  make([]int, n),
		prefix: make([]string, n),
		last:   make([]bool, n),
	}
	// 同一父的最后一行是 └─，其余是 ├─。
	lastOfParent := map[int]int{}
	for i, p := range parents {
		lastOfParent[p] = i
	}
	for i, p := range parents {
		if j, ok := byID[p]; ok && j != i {
			t.last[i] = lastOfParent[p] == i
		}
	}

	// 沿 parent 向上走，带环守卫：深度是层数，前缀是路径上「除自己和根
	// 之外」的每个祖先的那一段——根不贡献竖线（它没有兄弟，竖线无处可接）。
	for i := 0; i < n; i++ {
		var segs []string
		visited := map[int]bool{ids[i]: true}
		path := []int{i}
		for len(path) <= n {
			cur := path[len(path)-1]
			p := parents[cur]
			j, ok := byID[p]
			if !ok || j == cur || visited[p] {
				break
			}
			visited[p] = true
			path = append(path, j)
		}
		// path = [自己, 父, 祖父, …, 根]。去掉自己和根，中间每个祖先一段。
		for k := 1; k < len(path)-1; k++ {
			if t.last[path[k]] {
				segs = append(segs, "   ")
			} else {
				segs = append(segs, "│  ")
			}
		}
		t.depth[i] = len(path) - 1
		// segs 是从当前往根收集的，倒过来才是从根往当前。
		for l, r := 0, len(segs)-1; l < r; l, r = l+1, r-1 {
			segs[l], segs[r] = segs[r], segs[l]
		}
		t.prefix[i] = strings.Join(segs, "")
	}
	return t
}

// indent 是行 i 的完整前缀（祖先段 + 末段）。
func (t *planTree) indent(i int) string {
	if t.depth[i] == 0 {
		return ""
	}
	if t.last[i] {
		return t.prefix[i] + "└─ "
	}
	return t.prefix[i] + "├─ "
}
