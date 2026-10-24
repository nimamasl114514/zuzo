package object

import "fmt"

// Channel 通道对象，封装 Go 的 chan object.Object
type Channel struct {
	Value chan Object
}

func (c *Channel) Type() ObjectType { return CHANNEL_OBJ }
func (c *Channel) Inspect() string {
	return fmt.Sprintf("通道(%p)", c.Value)
}

// CHANNEL_OBJ 通道类型常量
const CHANNEL_OBJ = "通道"
