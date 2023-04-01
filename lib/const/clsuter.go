package constv

type cluster struct {
	CLUSTER string
}

type service struct {
	CLUSTER string
	PPROF   string
}

var CLUSTER = &cluster{
	CLUSTER: "cluster",
}

var SERVICE = &service{
	CLUSTER: "cluster",
	PPROF:   "pprof",
}
