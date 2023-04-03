package utils

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	config "github.com/llsw/goskynet/lib/config"
	share "github.com/llsw/goskynet/lib/share"
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

func Now2Date() string {
	now := time.Now()
	year, mon, day := now.Date()
	h, m, s := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d", year, mon, day, h, m, s)
}

func Now2DateWithoutSpace() string {
	now := time.Now()
	year, mon, day := now.Date()
	h, m, s := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d_%02d:%02d:%02d", year, mon, day, h, m, s)
}

func Now2DateStart() string {
	now := time.Now()
	year, mon, day := now.Date()
	return fmt.Sprintf("%d-%02d-%02d 00:00:00", year, mon, day)
}

func Now2DateStartWithoutSpace() string {
	now := time.Now()
	year, mon, day := now.Date()
	return fmt.Sprintf("%d-%02d-%02d_00:00:00", year, mon, day)
}

func Now2Day() string {
	now := time.Now()
	year, mon, day := now.Date()
	return fmt.Sprintf("%d-%02d-%02d", year, mon, day)
}

func Now2Hour() string {
	now := time.Now()
	year, mon, day := now.Date()
	h, _, _ := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d %02d:00:00", year, mon, day, h)
}

func Now2HourWithoutSpace() string {
	now := time.Now()
	year, mon, day := now.Date()
	h, _, _ := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d_%02d:00:00", year, mon, day, h)
}

func Now2DateWithZone() string {
	now := time.Now()
	year, mon, day := now.Date()
	h, m, s := now.Clock()
	zone, _ := now.Zone()
	return fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d %sn", year, mon, day, h, m, s, zone)
}

func Sec2Date(sec int64) string {
	now := time.Unix(sec, 0)
	year, mon, day := now.Date()
	h, m, s := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d", year, mon, day, h, m, s)
}

func Sec2DateWithoutSpace(sec int64) string {
	now := time.Unix(sec, 0)
	year, mon, day := now.Date()
	h, m, s := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d_%02d:%02d:%02d", year, mon, day, h, m, s)
}

func Sec2Hour(sec int64) string {
	now := time.Unix(sec, 0)
	year, mon, day := now.Date()
	h, _, _ := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d %02d:00:00", year, mon, day, h)
}

func Sec2HourWithoutSpace(sec int64) string {
	now := time.Unix(sec, 0)
	year, mon, day := now.Date()
	h, _, _ := now.Clock()
	return fmt.Sprintf("%d-%02d-%02d_%02d:00:00", year, mon, day, h)
}

func Sec2DateWithZone(sec int64) string {
	now := time.Unix(sec, 0)
	year, mon, day := now.Date()
	h, m, s := now.Clock()
	zone, _ := now.Zone()
	// fmt.Printf("本地时间是 %d-%02d-%02d %02d:%02d:%02d %sn",
	// 	year, mon, day, h, m, s, zone) // 本地时间是 2016-7-14 15:06:46 CST
	return fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d %sn", year, mon, day, h, m, s, zone)
}

func DelayFunc(sec int64, action func()) {
	timer := time.NewTimer(time.Duration(sec) * time.Second)
	select {
	case <-timer.C:
		action()
	}
}

func GetNextHourSec() int64 {
	now := Now2Hour()
	loc, _ := time.LoadLocation("Local")
	nowTime, _ := time.ParseInLocation("2006-01-02 15:04:05", now, loc)
	nowSec := nowTime.Unix()
	nextSec := nowSec + 3600
	return nextSec - time.Now().Unix()
}

func GetNextDateSec() int64 {
	now := Now2DateStart()
	loc, _ := time.LoadLocation("Local")
	nowTime, _ := time.ParseInLocation("2006-01-02 15:04:05", now, loc)
	nowSec := nowTime.Unix()
	nextSec := nowSec + 3600*24
	return nextSec - time.Now().Unix()
}

type ClusterFlag struct {
	ConfigPath string
}

func usage(ver string) {
	exe := os.Args[0]
	fmt.Fprintf(os.Stderr, `%s %s
Usage: %s -c
Options:
`, exe, ver, exe)
	flag.PrintDefaults()
}

func PareClusterFlag(ver string) (cf *ClusterFlag, err error) {
	var config string
	// pwd, err := os.Getwd()
	if err != nil {
		return
	}
	// defaultPath := pwd + "/config.yaml"
	flag.StringVar(
		&config, "c",
		"",
		"配置文件路径",
	)
	flag.Parse()

	if config == "" {
		flag.Usage = func() {
			usage(ver)
		}
		flag.Usage()
		os.Exit(1)
	}

	cf = &ClusterFlag{ConfigPath: config}
	return
}

func GetConifgPath(ver string) (path string) {
	cf, err := PareClusterFlag(ver)
	if err != nil {
		hlog.Fatalf(err.Error())
		return
	}
	path = cf.ConfigPath
	return
}

func RunNumGoroutineMonitor() {
	for {
		hlog.Debugf("groutine ->%d\n", runtime.NumGoroutine())
		time.Sleep(60 * time.Second)
	}
}

func Register(receiver interface{}) (methods map[string]*share.Method) {
	typ := reflect.TypeOf(receiver)
	rcvr := reflect.ValueOf(receiver)
	methods = make(map[string]*share.Method)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		meth := &share.Method{
			Rcvr:   rcvr,
			Method: method,
		}
		methods[method.Name] = meth
	}
	return
}
