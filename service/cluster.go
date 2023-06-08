package actor

import (
	"fmt"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	config "github.com/llsw/goskynet/lib/config"
	cv "github.com/llsw/goskynet/lib/const"
	log "github.com/llsw/goskynet/lib/log"
	"github.com/llsw/goskynet/lib/utils"
	skynetCore "github.com/llsw/goskynet/network/skynet"
)

var skynet = GetInstance()

type (
	Cluster struct {
		name       string
		addr       string
		worker     *skynetCore.Cluster
		jobTimeout int
	}
)

// ===必须实现===
func (c *Cluster) Init(name string, pid *actor.PID) (err error) {
	worker, err := skynetCore.NewCluster(c.name, c.addr, c.onData, c.jobTimeout)
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

func (c *Cluster) onData(conn *skynetCore.GateConn, addr interface{}, session uint32, args ...interface{}) {
	// hlog.Debugf("onData addr:%v session:%d, args:%v", addr, session, args)
	index := len(args) - 1
	f := args[index].(func([]interface{}, error))
	var resp []interface{}
	var err error
	switch v := addr.(type) {
	case string:
		ins := GetInstance()
		var cb CbFun = func(res interface{}, err error) {
			if err == nil {
				switch v := res.(type) {
				case error:
					err = v
				}

				if err == nil {
					resp = res.([]interface{})
				}
			}
			f(resp, err)
		}
		args[index] = cb
		ins.CallNoBlock(v, args[0].(string), args[1:]...)
		return
	// 本来是0才代表获取的是自己的，但go版skynet就不给服务编数字编号了，不直观
	case uint32:
		resp = []interface{}{"cluster"}
	default:
		err = fmt.Errorf("call addr:%v not found", addr)
		resp = nil
	}

	if err != nil {
		hlog.Errorf("cluster handle call error addr:%v session:%d, error:%v", addr, session, err.Error())
	}
	f(resp, err)
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

func (c *Cluster) CallByAddr(cluster string, addr string, req ...interface{}) (resp interface{}, err error) {
	return c.worker.Call(cluster, addr, req...)
}

func (c *Cluster) CallNoBlock(cluster string, addr string, req ...interface{}) {
	node, err := utils.GetClusterAddrByName(cluster)
	if err != nil {
		cb := req[len(req)-1].(CbFun)
		cb(nil, err)
		return
	}
	// hlog.Debugf("CallNoBlock %v", req)
	c.worker.CallNoBlock(node, addr, req...)
}

func (c *Cluster) CallByAddrNoBlock(cluster string, addr string, req ...interface{}) {
	// hlog.Debugf("CallNoBlock %v", req)
	c.worker.CallNoBlock(cluster, addr, req...)
}

func (c *Cluster) Send(cluster string, addr string, req ...interface{}) (err error) {
	node, err := utils.GetClusterAddrByName(cluster)
	if err != nil {
		return
	}
	return c.worker.Send(node, addr, req...)
}

func Call(req ...interface{}) (resp interface{}, err error) {
	return GetInstance().Call(cv.SERVICE.CLUSTER, "Call", req...)
}

func CallByAddr(req ...interface{}) (resp interface{}, err error) {
	return GetInstance().Call(cv.SERVICE.CLUSTER, "CallByAddr", req...)
}

func CallNoBlock(req ...interface{}) {
	GetInstance().CallNoBlock(cv.SERVICE.CLUSTER, "CallNoBlock", req...)
}

func CallByAddrNoBlock(req ...interface{}) {
	GetInstance().CallNoBlock(cv.SERVICE.CLUSTER, "CallByAddrNoBlock", req...)
}

func Send(req ...interface{}) (err error) {
	return GetInstance().Send(cv.SERVICE.CLUSTER, "Call", req...)
}

func SendByAddr(req ...interface{}) (err error) {
	return GetInstance().Send(cv.SERVICE.CLUSTER, "CallByAddr", req...)
}

// ===自定义消息处理方法===

func Open(configPath string) (c *skynetCore.Cluster, close func(), err error) {
	err = config.LoadClusterConfig(configPath)
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
	jt := cc.JobTimeout
	if jt == 0 {
		jt = 20
	}
	ins := GetInstance()
	for i := 0; i < workers; i++ {
		worker := Cluster{}
		worker.name = fmt.Sprintf("%s_w%d", name, i)
		worker.addr = "" // 空地址表示不监听
		worker.jobTimeout = jt
		_, err = ins.newService(cv.SERVICE.CLUSTER, &worker)
		if err != nil {
			hlog.Errorf("NewService cluster error:%s", err.Error())
			return
		}
	}

	master := Cluster{}
	master.name = fmt.Sprintf("%s_m", name)
	master.addr = adrr
	master.jobTimeout = jt
	_, err = ins.newService(cv.SERVICE.CLUSTER, &master)
	if err != nil {
		hlog.Errorf("NewService cluster error:%s", err.Error())
		return
	}
	c = master.worker
	return
}

func startCluster(path string) (c *skynetCore.Cluster, close func()) {
	go utils.RunNumGoroutineMonitor()
	c, close, err := Open(path)
	if err != nil {
		if close != nil {
			close()
		}
		hlog.Fatalf(
			"start cluster fail, config:%s error:%s",
			path, err.Error(),
		)
		return
	}

	pc := config.GetInstance().Config.Pprof
	NewPprofService()
	_, err = GetInstance().Call(cv.SERVICE.PPROF, "Open", pc)
	if err != nil {
		if close != nil {
			close()
		}
		hlog.Fatalf(
			"start cluster fail, start %s error",
			cv.SERVICE.PPROF,
		)
		return
	}

	return
}

func StartCluster(version string, start func(), test func()) {
	path := utils.GetConifgPath(version)
	c, close := startCluster(path)
	defer close()
	go func() {
		start()
		test()
	}()
	hlog.Fatal(c.ListenAndServe())
}
