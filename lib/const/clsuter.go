package constv

type cluster struct {
	CENTER string
}

type service struct {
	CENTER string
	PPROF  string
}

var CLUSTER = &cluster{
	CENTER: "center",
}

var SERVICE = &service{
	CENTER: "center",
	PPROF:  "pprof",
}
