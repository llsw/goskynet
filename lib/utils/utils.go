package utils

import (
	"fmt"

	config "github.com/llsw/goskynet/lib/config"
)

func GetClusterAddrByName(name string) (addr string, err error) {
	config := config.GetInstance()
	if v, ok := (*config.Clustername)[name]; ok {
		addr = v.(string)
		return
	}
	err = fmt.Errorf("cluster:%s addr not found", name)
	return
}
