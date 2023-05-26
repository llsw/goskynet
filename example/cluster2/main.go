package main

import (
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/llsw/goskynet/lib/utils"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	cluster.StartCluster("v0.1.2", func() {
		// time.AfterFunc(3*time.Second, test)
		// time.AfterFunc(30*time.Second, test)
		// for {
		utils.DelayFunc(1, func() {
			go test2()
		})
		// }
	}, func() {

	})
}

func callIkun(wg *sync.WaitGroup, index int) {
	var cb cluster.CbFun = func(resp interface{}, err error) {
		if err != nil {
			hlog.Errorf("call cluster1 fail index:%d error:%s", index, err)
			wg.Done()
			return
		}
		// hlog.Debugf("res %v", resp)
		wg.Done()
	}
	cluster.CallNoBlock("cluster1", "ikun", "Ikun", "hello", "ikun", cb)
}

func test1() {
	var cb cluster.CbFun = func(resp interface{}, err error) {
		if err != nil {
			hlog.Errorf("call cluster1 fail error:%s", err)

			return
		}
	}
	cluster.CallNoBlock("cluster1", "ikun", "Ikun", "hello", "ikun", cb)
}

func test2() {
	resp, err := cluster.Call("plaza1", "plaza", "commonCallback", "dfdfdsfads", "dfadsfdsaf", 100)
	ok := resp.([]interface{})[0].([]interface{})[0].(bool)
	hlog.Errorf("resp:%v ok:%v err:%v", resp, ok, err)
}

func test() {
	var wg *sync.WaitGroup = new(sync.WaitGroup)
	st := time.Now().UnixMilli()
	num := 10000
	wg.Add(num)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < num/10; j++ {
				callIkun(wg, j)
			}
		}()
	}

	wg.Wait()
	ed := time.Now().UnixMilli()
	hlog.Debugf("cost:%dms", ed-st)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
	// go utils.DelayFunc(180, callIkun)
}
