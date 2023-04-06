package constv

type cluster struct {
	CLUSTER string
}

type service struct {
	CLUSTER string
	PPROF   string
	ENTITY  string
}

var CLUSTER = &cluster{
	CLUSTER: "cluster",
}

var SERVICE = &service{
	CLUSTER: "cluster",
	PPROF:   "pprof",
	ENTITY:  "entity", // 只有数据，没有逻辑
}
