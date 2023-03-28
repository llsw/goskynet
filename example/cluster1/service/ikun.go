package actor

import (
	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	skynet "github.com/llsw/goskynet/service"
)

type (
	Ikun struct {
	}
)

// ===必须实现===
func (i *Ikun) Init(name string, pid *actor.PID) (err error) {
	return
}

func (i *Ikun) Start(name string, pid *actor.PID) {
}

func (i *Ikun) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (i *Ikun) Ikun(args1 string, args2 string) (string, string, string) {
	hlog.Debugf("Ikun args1:%s args2:%s", args1, args2)
	return "唱跳", "rap", "篮球"
}

// ===自定义消息处理方法===

func NewIkunService() (*actor.PID, error) {
	ins := skynet.GetInstance()
	ikun := Ikun{}
	return ins.NewService("ikun", &ikun)
}
