package main

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
	utils "github.com/llsw/goskynet/lib/utils"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	path := utils.GetConifgPath("v0.1.2")
	c, close := cluster.StartCluster(path)
	defer func() {
		close()
	}()
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)
	go utils.DelayFunc(3, callIkun)

	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)

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
