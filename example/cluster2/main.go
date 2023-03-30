package main

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
	utils "github.com/llsw/goskynet/lib/utils"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	cf, err := utils.PareClusterFlag("v0.1.2")
	if err != nil {
		hlog.Fatalf(err.Error())
		return
	}
	c, close, err := cluster.Open(cf.ConfigPath)
	defer func() {
		close()
	}()
	if err != nil {
		hlog.Errorf(
			"start cluster fail, config:%s error:%s",
			cf.ConfigPath, err.Error(),
		)
	}
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	hlog.Fatal(c.ListenAndServe())
}

func callIkun() {
	resp, err := cluster.Call("cluster1", "ikun", "Ikun", "hello", "ikun")
	if err != nil {
		hlog.Errorf("call cluster1 fail error:%s", err)
		return
	}
	hlog.Infof("call cluster1 resp:%v", resp)
}
