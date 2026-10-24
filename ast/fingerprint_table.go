package ast

import (
	"fmt"
	"sort"
)

// fingerprint_table.go — 拓扑指纹索引表
//
// 提供 O(1) 的结构同构查找、代码查重和死代码消除能力。
//
// 使用方式：
//
//	table := NewFingerprintTable()
//	table.Register(expr1, "file.hy:10")
//	table.Register(expr2, "file.hy:20")
//	dups := table.FindDuplicates()          // 找出所有结构相同的代码块
//	matches := table.Lookup(targetFingerprint) // O(1) 精确匹配

// FingerprintEntry 记录一个 AST 节点的指纹及其位置信息
type FingerprintEntry struct {
	Fingerprint uint64     // 拓扑指纹
	Node        Node       // 原始 AST 节点（用于二次验证）
	Location    string    // 源码位置描述（如 "file.hy:10"）
	NodeType    string     // 节点类型名（如 "InfixExpression"）
}

// FingerprintTable 拓扑指纹索引表
type FingerprintTable struct {
	// fingerprint → 所有匹配的条目列表
	index map[uint64][]FingerprintEntry
	// 总节点数
	count int
}

// NewFingerprintTable 创建空的指纹索引表
func NewFingerprintTable() *FingerprintTable {
	return &FingerprintTable{
		index: make(map[uint64][]FingerprintEntry),
	}
}

// Register 将一个 AST 节点注册到指纹表中。
// location 是可选的源码位置描述（如 "file.hy:10"）。
func (t *FingerprintTable) Register(node Node, location string) {
	if node == nil {
		return
	}

	fp := FingerprintNode(node)
	entry := FingerprintEntry{
		Fingerprint: fp,
		Node:        node,
		Location:    location,
		NodeType:    NodeTypeName(node),
	}
	t.index[fp] = append(t.index[fp], entry)
	t.count++
}

// RegisterExpression 注册一个表达式节点（使用 Fingerprint 而非 FingerprintNode）
func (t *FingerprintTable) RegisterExpression(expr Expression, location string) {
	if expr == nil {
		return
	}

	fp := Fingerprint(expr)
	entry := FingerprintEntry{
		Fingerprint: fp,
		Node:        expr,
		Location:    location,
		NodeType:    NodeTypeName(expr),
	}
	t.index[fp] = append(t.index[fp], entry)
	t.count++
}

// RegisterProgram 遍历整个程序，注册所有语句和子表达式
func (t *FingerprintTable) RegisterProgram(program *Program, fileHint string) {
	for i, stmt := range program.Statements {
		loc := fmt.Sprintf("%s:%d", fileHint, i+1)
		t.registerStatementDeep(stmt, loc)
	}
}

// registerStatementDeep 递归注册语句及其所有子表达式
func (t *FingerprintTable) registerStatementDeep(stmt Statement, location string) {
	if stmt == nil {
		return
	}

	// 先注册语句本身
	t.Register(stmt, location)

	switch s := stmt.(type) {
	case *ExpressionStatement:
		t.registerExpressionDeep(s.Expression, location)

	case *LetStatement:
		t.registerExpressionDeep(s.Value, location)

	case *ReturnStatement:
		t.registerExpressionDeep(s.ReturnValue, location)

	case *WhileStatement:
		t.registerExpressionDeep(s.Condition, location+":cond")
		if s.Body != nil {
			for i, bs := range s.Body.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:body:%d", location, i))
			}
		}

	case *ForStatement:
		t.registerExpressionDeep(s.Init, location+":init")
		t.registerExpressionDeep(s.Condition, location+":cond")
		t.registerExpressionDeep(s.Update, location+":update")
		if s.Body != nil {
			for i, bs := range s.Body.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:body:%d", location, i))
			}
		}

	case *ForInStatement:
		t.registerExpressionDeep(s.Iterable, location+":iter")
		t.registerExpressionDeep(s.EndExpr, location+":end")
		t.registerExpressionDeep(s.Guard, location+":guard")
		if s.Body != nil {
			for i, bs := range s.Body.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:body:%d", location, i))
			}
		}

	case *BlockStatement:
		for i, bs := range s.Statements {
			t.registerStatementDeep(bs, fmt.Sprintf("%s:%d", location, i))
		}

	case *TryCatchStatement:
		if s.Body != nil {
			for i, bs := range s.Body.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:try:%d", location, i))
			}
		}
		if s.Handler != nil {
			for i, bs := range s.Handler.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:catch:%d", location, i))
			}
		}
	}
}

// registerExpressionDeep 递归注册表达式及其所有子表达式
func (t *FingerprintTable) registerExpressionDeep(expr Expression, location string) {
	if expr == nil {
		return
	}

	// 先注册表达式本身
	t.RegisterExpression(expr, location)

	switch n := expr.(type) {
	case *PrefixExpression:
		t.registerExpressionDeep(n.Right, location+":right")

	case *InfixExpression:
		t.registerExpressionDeep(n.Left, location+":left")
		t.registerExpressionDeep(n.Right, location+":right")

	case *AssignExpression:
		t.registerExpressionDeep(n.Value, location+":val")
		if n.IndexTarget != nil {
			t.registerExpressionDeep(n.IndexTarget, location+":idx")
		}

	case *IfExpression:
		t.registerExpressionDeep(n.Condition, location+":cond")
		if n.Consequence != nil {
			for i, bs := range n.Consequence.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:then:%d", location, i))
			}
		}
		if n.Alternative != nil {
			for i, bs := range n.Alternative.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:else:%d", location, i))
			}
		}

	case *FunctionLiteral:
		if n.Body != nil {
			for i, bs := range n.Body.Statements {
				t.registerStatementDeep(bs, fmt.Sprintf("%s:body:%d", location, i))
			}
		}

	case *CallExpression:
		t.registerExpressionDeep(n.Function, location+":fn")
		for i, arg := range n.Arguments {
			t.registerExpressionDeep(arg, fmt.Sprintf("%s:arg:%d", location, i))
		}
		for i, na := range n.NamedArgs {
			t.registerExpressionDeep(na.Value, fmt.Sprintf("%s:named:%d", location, i))
		}

	case *ArrayLiteral:
		for i, e := range n.Elements {
			t.registerExpressionDeep(e, fmt.Sprintf("%s:elem:%d", location, i))
		}

	case *HashLiteral:
		for k, v := range n.Pairs {
			t.registerExpressionDeep(k, location+":key")
			t.registerExpressionDeep(v, location+":val")
		}

	case *IndexExpression:
		t.registerExpressionDeep(n.Left, location+":left")
		t.registerExpressionDeep(n.Index, location+":index")

	case *FStringLiteral:
		for i, p := range n.Parts {
			t.registerExpressionDeep(p, fmt.Sprintf("%s:part:%d", location, i))
		}
	}
}

// Lookup 精确查找指纹，O(1) 时间复杂度
func (t *FingerprintTable) Lookup(fp uint64) []FingerprintEntry {
	return t.index[fp]
}

// LookupExpression 根据表达式查找所有结构同构的节点
func (t *FingerprintTable) LookupExpression(expr Expression) []FingerprintEntry {
	return t.index[Fingerprint(expr)]
}

// LookupNode 根据 AST 节点查找所有结构同构的节点
func (t *FingerprintTable) LookupNode(node Node) []FingerprintEntry {
	return t.index[FingerprintNode(node)]
}

// FindDuplicates 返回所有出现 ≥ 2 次的指纹组（代码查重）
// 结果按重复次数从高到低排序
func (t *FingerprintTable) FindDuplicates() [][]FingerprintEntry {
	var groups [][]FingerprintEntry
	for _, entries := range t.index {
		if len(entries) >= 2 {
			// 深拷贝避免外部修改
			cp := make([]FingerprintEntry, len(entries))
			copy(cp, entries)
			groups = append(groups, cp)
		}
	}
	// 按重复次数降序排序
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i]) > len(groups[j])
	})
	return groups
}

// FindSideEffectFreeDuplicates 找出所有无副作用的重复表达式（CSE 候选）
// isPure 谓词返回 true 表示节点无副作用（如字面量、变量引用、纯算术运算）
func (t *FingerprintTable) FindSideEffectFreeDuplicates(
	isPure func(Node) bool,
) [][]FingerprintEntry {
	var groups [][]FingerprintEntry
	for _, entries := range t.index {
		if len(entries) < 2 {
			continue
		}
		// 仅保留无副作用的节点
		var pure []FingerprintEntry
		for _, e := range entries {
			if isPure(e.Node) {
				pure = append(pure, e)
			}
		}
		if len(pure) >= 2 {
			groups = append(groups, pure)
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i]) > len(groups[j])
	})
	return groups
}

// Count 返回已注册的节点总数
func (t *FingerprintTable) Count() int {
	return t.count
}

// UniqueCount 返回不同指纹的数量
func (t *FingerprintTable) UniqueCount() int {
	return len(t.index)
}

// Stats 返回指纹表的统计信息
type FingerprintStats struct {
	TotalNodes    int // 总节点数
	UniqueNodes   int // 唯一指纹数
	DuplicateNodes int // 有重复的节点数（总 - 唯一）
	DuplicateGroups int // 重复组数
	MaxGroupSize  int // 最大重复组大小
}

// Stats 返回指纹表的统计信息
func (t *FingerprintTable) Stats() FingerprintStats {
	stats := FingerprintStats{
		TotalNodes:  t.count,
		UniqueNodes: len(t.index),
	}
	for _, entries := range t.index {
		if len(entries) >= 2 {
			stats.DuplicateGroups++
			dupCount := len(entries) - 1
			stats.DuplicateNodes += dupCount
			if len(entries) > stats.MaxGroupSize {
				stats.MaxGroupSize = len(entries)
			}
		}
	}
	return stats
}

// nodeTypeName 返回 AST 节点的类型名（用于调试和报告）
func NodeTypeName(node Node) string {
	if node == nil {
		return "nil"
	}
	switch node.(type) {
	case *Program:
		return "Program"
	case *LetStatement:
		return "LetStatement"
	case *ReturnStatement:
		return "ReturnStatement"
	case *ExpressionStatement:
		return "ExpressionStatement"
	case *WhileStatement:
		return "WhileStatement"
	case *ForStatement:
		return "ForStatement"
	case *ForInStatement:
		return "ForInStatement"
	case *ImportStatement:
		return "ImportStatement"
	case *BlockStatement:
		return "BlockStatement"
	case *Identifier:
		return "Identifier"
	case *IntegerLiteral:
		return "IntegerLiteral"
	case *FloatLiteral:
		return "FloatLiteral"
	case *StringLiteral:
		return "StringLiteral"
	case *FStringLiteral:
		return "FStringLiteral"
	case *BooleanLiteral:
		return "BooleanLiteral"
	case *NullLiteral:
		return "NullLiteral"
	case *PrefixExpression:
		return "PrefixExpression"
	case *InfixExpression:
		return "InfixExpression"
	case *AssignExpression:
		return "AssignExpression"
	case *IfExpression:
		return "IfExpression"
	case *FunctionLiteral:
		return "FunctionLiteral"
	case *CallExpression:
		return "CallExpression"
	case *ArrayLiteral:
		return "ArrayLiteral"
	case *HashLiteral:
		return "HashLiteral"
	case *IndexExpression:
		return "IndexExpression"
	case *ErrorExpression:
		return "ErrorExpression"
	case *TryCatchStatement:
		return "TryCatchStatement"
	case *DefineStatement:
		return "DefineStatement"
	case *BreakStatement:
		return "BreakStatement"
	case *ContinueStatement:
		return "ContinueStatement"
	default:
		return "Unknown"
	}
}
