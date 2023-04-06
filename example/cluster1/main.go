package main

import (
	service "github.com/llsw/goskynet/example/cluster1/service"
	cluster "github.com/llsw/goskynet/service"
)

func main() {
	cluster.StartCluster("v0.1.2", func() {
		for i := 0; i < 12; i++ {
			service.NewIkunService()
		}
	}, func() {
	})
}
