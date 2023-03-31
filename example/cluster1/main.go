package main

import (
	service "github.com/llsw/goskynet/example/cluster1/service"
	utils "github.com/llsw/goskynet/lib/utils"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	path := utils.GetConifgPath("v0.1.2")

	service.NewIkunService()

	c, close := cluster.StartCluster(path)
	defer func() {
		close()
	}()
	c.ListenAndServe()
}
