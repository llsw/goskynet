package actor

import (
	"fmt"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	config "github.com/llsw/goskynet/lib/config"
	log "github.com/llsw/goskynet/lib/log"
	"github.com/llsw/goskynet/lib/utils"
	skynet "github.com/llsw/goskynet/network/skynet"
)

type (
	Cluster struct {
		name   string
		addr   string
		worker *skynet.Cluster
	}
)

// ===必须实现===
func (c *Cluster) Init(name string, pid *actor.PID) (err error) {
	worker, err := skynet.NewCluster(c.name, c.addr, c.onData)
	if err != nil {
		hlog.Errorf("start cluster:%s error:%s", name, err.Error())
		return
	}
	c.worker = worker
	return
}

func (c *Cluster) Start(name string, pid *actor.PID) {
}

func (c *Cluster) Stop(name string, pid *actor.PID) (err error) {
	return
}

func (c *Cluster) onData(addr interface{}, session uint32, args ...interface{}) (resp []interface{}, err error) {
	// hlog.Debugf("onData addr:%v session:%d, args:%v", addr, session, args)
	switch v := addr.(type) {
	case string:
		ins := GetInstance()
		var res interface{}
		res, err = ins.Call(v, args[0].(string), args[1:]...)
		if err == nil {
			switch v := res.(type) {
			case error:
				err = v
			}
			if err != nil {
				break
			}
			resp = res.([]interface{})
			// 本来是0才代表获取的是自己的，但go版skynet就不给服务编数字编号了，不直观
		}
	case uint32:
		resp = []interface{}{"cluster"}
	default:
		err = fmt.Errorf("call addr:%v not found", addr)
		resp = nil
	}

	if err != nil {
		hlog.Errorf("cluster handle call error addr:%v session:%d, error:%v", addr, session, err.Error())
	}
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (c *Cluster) Call(cluster string, addr string, req ...interface{}) (resp interface{}, err error) {
	node, err := utils.GetClusterAddrByName(cluster)
	if err != nil {
		return
	}
	return c.worker.Call(node, addr, req...)
}

func (c *Cluster) Send(cluster string, addr string, req ...interface{}) (err error) {
	node, err := utils.GetClusterAddrByName(cluster)
	if err != nil {
		return
	}
	return c.worker.Send(node, addr, req...)
}

func Call(req ...interface{}) (resp interface{}, err error) {
	return GetInstance().Call("cluster", "Call", req...)
}

func Send(req ...interface{}) (err error) {
	return GetInstance().Send("cluster", "Call", req...)
}

// ===自定义消息处理方法===

func Open(clusterConfigPath string) (c *skynet.Cluster, close func(), err error) {
	err = config.LoadClusterConfig(clusterConfigPath)
	if err != nil {
		hlog.Errorf("load cluster config error:%s", err.Error())
		return
	}
	cc := config.GetInstance().Config
	name := cc.Name
	workers := cc.Workers
	adrr := cc.Address
	lc := cc.Log
	l := log.InitLog(lc.Path, lc.Level, lc.Interval)
	close = func() {
		l.Close()
	}

	ins := GetInstance()
	for i := 0; i < workers; i++ {
		worker := Cluster{}
		worker.name = name
		worker.addr = "" // 空地址表示不监听
		_, err = ins.newService("cluster", &worker)
		if err != nil {
			hlog.Errorf("NewService cluster error:%s", err.Error())
			return
		}
	}

	master := Cluster{}
	master.name = name
	master.addr = adrr
	_, err = ins.newService("cluster", &master)
	if err != nil {
		hlog.Errorf("NewService cluster error:%s", err.Error())
		return
	}
	c = master.worker
	return
}
