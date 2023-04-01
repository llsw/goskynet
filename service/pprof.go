// https://github.com/asynkron/protoactor-go
package actor

import (
	"net/http"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/llsw/goskynet/lib/config"
	cv "github.com/llsw/goskynet/lib/const"
)

type (
	Pprof struct {
	}
)

// ===必须实现===
func (act *Pprof) Init(name string, pid *actor.PID) (err error) {
	return
}

func (act *Pprof) Start(name string, pid *actor.PID) {
}

func (act *Pprof) Stop(name string, pid *actor.PID) (err error) {
	return
}

func Ping(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func (h *Pprof) Open(conf *config.PprofConifg) {
	go func() {
		r := gin.Default()
		pprof.Register(r)
		r.GET("/ping", Ping)
		r.Run(conf.Address)
	}()
}

// ===必须实现===

// ===自定义消息处理方法===

// ===自定义消息处理方法===

func NewPprofService() (*actor.PID, error) {
	skynet := GetInstance()
	svc := Pprof{}
	return skynet.NewService(cv.SERVICE.PPROF, &svc)
}
