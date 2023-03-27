// https://github.com/asynkron/protoactor-go
package actor

import (
	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type (
	Hello struct {
	}
)

// ===必须实现===
func (h *Hello) Init(name string, pid *actor.PID) (err error) {
	return
}

func (h *Hello) Start(name string, pid *actor.PID) {
}

func (h *Hello) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (h *Hello) Ikun(args1 string, args2 string) (string, string, string) {
	hlog.Debugf("Ikun args1:%s args2:%s", args1, args2)
	return "唱跳", "rap", "篮球"
}

// ===自定义消息处理方法===

func NewHelloService() (*actor.PID, error) {
	skynet := GetInstance()
	hello := Hello{}
	return skynet.NewService("hello", &hello)
}

func HelloTest() {
	NewHelloService()
	skynet := GetInstance()
	resp, err := skynet.Call("hello", "Ikun", "hello", "ikun")
	if err != nil {
		hlog.Errorf("call hello error:%s\n", err.Error())
		return
	}
	hlog.Debugf("call hello response:%v\n", resp)
}
