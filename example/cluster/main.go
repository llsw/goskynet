package main

import (
	"reflect"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	skynet "github.com/llsw/goskynet/network/skynet"
)

func main() {
	cluster1()
	c, err := cluster2()
	if err != nil {
		return
	}
	// 延迟一下
	delayFunc(3, func() {
		callTest(c)
	})
}

func delayFunc(sec int64, action func()) {
	timer := time.NewTimer(time.Duration(sec) * time.Second)
	select {
	case <-timer.C:
		action()
	}
}

func onDataIkun(addr interface{}, session uint32,
	args ...interface{}) (resps []interface{}, err error) {
	hlog.Debugf("onDataIkun addr:%v session:%d args:%v", addr, session, args)
	// TODO 根据addr call service

	resps = make([]interface{}, 10)
	array := []interface{}{1, 2, 3}
	table := make(map[string]interface{})
	table["who"] = "ikun"
	tt := make(map[string]interface{})
	tt["hello"] = "world"
	table["table"] = tt
	table["array"] = array
	resps[0] = nil
	resps[1] = 0
	resps[2] = -1
	resps[3] = 1
	resps[4] = 123.123
	resps[5] = true
	resps[6] = array
	resps[7] = table
	return
}

func onData(addr interface{}, session uint32,
	args ...interface{}) (resps []interface{}, err error) {
	return
}

func cluster1() {
	// call的cluster，可以改成lua skynet的cluster进行测试
	clusterName := "ikun"
	lisentAddr := "0.0.0.0:1989"
	_, err := skynet.NewCluster(
		clusterName, lisentAddr,
		onDataIkun)
	if err != nil {
		hlog.Errorf("%s err:%s\n", clusterName, err.Error())
	}
}

func cluster2() (c *skynet.Cluster, err error) {
	clusterName := "goskynet"
	lisentAddr := "0.0.0.0:1988"
	c, err = skynet.NewCluster(
		clusterName, lisentAddr,
		onData)
	if err != nil {
		hlog.Errorf("%s err:%s\n", clusterName, err.Error())
		return
	}
	return
}

func callTest(c *skynet.Cluster) {
	clusterAddr := "127.0.0.1:1989"
	serviceAddr := "ikun"
	cmd := "hello"
	resp, err := c.Call(clusterAddr, serviceAddr, cmd, "hello ikun")
	if err != nil {
		hlog.Errorf("call ikun cluster err:%s\n", err.Error())
		return
	}

	hlog.Debugf("call ikun cluster resp:%v\n", resp)

	for i, v := range resp {
		hlog.Debugf("resp: %d %v\n", i, reflect.TypeOf(v))
	}
}
