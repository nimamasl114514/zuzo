package object

import "strings"

// DictProxy 实例 __dict__ 的活视图（读写直达实例属性表）
type DictProxy struct {
	M map[string]Object
}

func (p *DictProxy) Type() *TypeObj { return dictType }
func (p *DictProxy) Repr() string {
	var sb strings.Builder
	sb.WriteString("{")
	i := 0
	for k, v := range p.M {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(ReprString(k))
		sb.WriteString(": ")
		sb.WriteString(v.Repr())
		i++
	}
	sb.WriteString("}")
	return sb.String()
}
func (p *DictProxy) Str() string { return p.Repr() }
