package main

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
	service "github.com/llsw/goskynet/example/cluster1/service"
	utils "github.com/llsw/goskynet/lib/utils"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	cf, err := utils.PareClusterFlag("v0.1.2")
	if err != nil {
		hlog.Fatalf(err.Error())
		return
	}

	service.NewIkunService()

	c, close, err := cluster.Open(cf.ConfigPath)
	defer func() {
		close()
	}()

	if err != nil {
		hlog.Errorf(
			"start cluster fail, config:%s error:%s",
			cf.ConfigPath, err.Error(),
		)
		return
	}
	c.ListenAndServe()
}
