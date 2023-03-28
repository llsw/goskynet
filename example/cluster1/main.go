package main

import (
	"flag"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	service "github.com/llsw/goskynet/example/cluster1/service"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	var config string
	flag.StringVar(&config, "c", "./config.yaml", "配置文件路径，默认./config.yaml")
	flag.Parse()

	service.NewIkunService()

	err := cluster.Open(config)
	if err != nil {
		hlog.Errorf(
			"start cluster fail, config:%s error:%s",
			config, err.Error(),
		)
		return
	}

}

func delayFunc(sec int64, action func()) {
	timer := time.NewTimer(time.Duration(sec) * time.Second)
	select {
	case <-timer.C:
		action()
	}
}
