package actor

import (
	"fmt"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	config "github.com/llsw/goskynet/lib/config"
	utils "github.com/llsw/goskynet/lib/utils"
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
		return
	}
	c.worker = worker
	return
}

func (c *Cluster) onData(addr interface{}, session uint32, args ...interface{}) (resp []interface{}, err error) {
	switch v := addr.(type) {
	case string:
		ins := GetInstance()
		var res interface{}
		res, err = ins.Call(v, args[0].(string), args[1:]...)
		if err != nil {
			break
		}
		resp = res.([]interface{})
	// 本来是0才代表获取的是自己的，但go版skynet就不给服务编数字编号了，不直观
	case uint32:
		resp = []interface{}{"cluster"}
	default:
		err = fmt.Errorf("call addr:%v not found", addr)
		resp = nil
	}
	if err != nil {
		hlog.Errorf("call addr:%v error:%s", err.Error())
	}
	return
}

func (c *Cluster) Start(name string, pid *actor.PID) {
}

func (c *Cluster) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (c *Cluster) Call(cluster string, addr string, req ...interface{}) (resp interface{}, err error) {
	return c.worker.Call(cluster, addr, req...)

}

func (c *Cluster) Send(cluster string, addr string, req ...interface{}) (err error) {
	return c.worker.Send(cluster, addr, req...)
}

func Call(cluster string, addr string, req ...interface{}) (resp interface{}, err error) {
	return GetInstance().Call("cluster", "Call", addr, req)
}

func Send(cluster string, addr string, req ...interface{}) (err error) {
	return GetInstance().Send("cluster", "Call", addr, req)
}

// ===自定义消息处理方法===

func Start(clusterConfigPath string) (err error) {
	err = config.LoadClusterConfig(clusterConfigPath)
	if err != nil {
		hlog.Errorf("load cluster config error:%s", err.Error())
		return
	}
	cc := config.GetInstance().Config
	name := cc.Name
	workers := cc.Workers
	adrr, err := utils.GetClusterAddrByName(name)
	if err != nil {
		hlog.Errorf("cluster addrr not found by name:%s", name)
		return
	}

	ins := GetInstance()
	cluster := Cluster{}
	cluster.name = name
	cluster.addr = adrr
	_, err = ins.newService("cluster", &cluster)
	if err != nil {
		hlog.Errorf("NewService cluster error:%s", err.Error())
		return
	}
	for i := 0; i < workers; i++ {
		cluster := Cluster{}
		cluster.name = name
		cluster.addr = "" // 空地址表示不监听
		_, err = ins.newService("cluster", &cluster)
		if err != nil {
			hlog.Errorf("NewService cluster error:%s", err.Error())
			return
		}
	}
	return
}
