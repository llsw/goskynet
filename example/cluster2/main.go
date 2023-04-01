package main

import (
	"time"

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

	utils.DelayFunc(3, test)

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

func test() {
	st := time.Now().UnixMilli()
	for i := 0; i < 10000; i++ {
		go callIkun()
	}
	ed := time.Now().UnixMilli()
	hlog.Debugf("cost:%f", st-ed/1000)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
}
