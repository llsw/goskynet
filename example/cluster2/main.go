package main

import (
	"flag"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		var config string
		flag.StringVar(&config, "c", "./config.yaml", "配置文件路径，默认./config.yaml")
		flag.Parse()
		err := cluster.Open(config)
		if err != nil {
			hlog.Errorf(
				"start cluster fail, config:%s error:%s",
				config, err.Error(),
			)
		}
		wg.Done()
	}()

	delayFunc(3, func() {
		hlog.Error("call cluster1")
		resp, err := cluster.Call("cluster1", "ikun", "Ikun", "hello", "ikun")
		if err != nil {
			hlog.Errorf("call cluster1 fail error:%s", err)
			return
		}
		hlog.Infof("call cluster1 resp:%v", resp)
	})

	wg.Wait()

}

func delayFunc(sec int64, action func()) {
	timer := time.NewTimer(time.Duration(sec) * time.Second)
	select {
	case <-timer.C:
		action()
	}
}
