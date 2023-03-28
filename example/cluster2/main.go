package main

import (
	"flag"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	var config string
	flag.StringVar(&config, "c", "./config.yaml", "配置文件路径，默认./config.yaml")
	flag.Parse()
	err := cluster.Open(config)
	if err != nil {
		hlog.Errorf(
			"start cluster fail, config:%s error:%s",
			config, err.Error(),
		)
		return
	}

	delayFunc(3, func() {
		resp, err := cluster.Call("cluster1", "ikun", "Ikun", "hello", "ikun")
		if err != nil {
			hlog.Errorf("call cluster1 fail error:%s", err)
			return
		}
		hlog.Infof("call cluster1 resp:%v", resp)
	})

}

func delayFunc(sec int64, action func()) {
	timer := time.NewTimer(time.Duration(sec) * time.Second)
	select {
	case <-timer.C:
		action()
	}
}
