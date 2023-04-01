package main

import (
	"sync"
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

func callIkun(wg *sync.WaitGroup, index int) {
	resp, err := cluster.Call("cluster1", "ikun", "Ikun", "hello", "ikun")
	if err != nil {
		hlog.Errorf("call cluster1 fail index:%d error:%s", index, err)
		wg.Done()
		return
	}
	hlog.Infof("call cluster1 index:%d resp:%v", index, resp)
	wg.Done()
}

func test() {
	var wg *sync.WaitGroup = new(sync.WaitGroup)
	st := time.Now().UnixMilli()
	num := 2000
	wg.Add(num)
	for i := 0; i < num; i++ {
		go callIkun(wg, i)
	}
	wg.Wait()
	ed := time.Now().UnixMilli()
	hlog.Debugf("qps:%d", ed-st)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
}
