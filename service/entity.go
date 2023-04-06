package actor

import (
	"encoding/json"
	"fmt"
	"sync"

	cv "github.com/llsw/goskynet/lib/const"

	"github.com/asynkron/protoactor-go/actor"
)

type (
	Data struct {
		Key     string       `json:"key"`
		Value   interface{}  `json:"value"`
		Created int64        `json:"created"` // 创建时间
		Updated int64        `json:"updated"` // 更新时间
		TTL     int64        `json:"ttl"`     // 过期时间单位毫秒
		Lock    sync.RWMutex `son:"-"`
	}

	Timer struct {
		Key      string  `json:"key"`
		Value    string  `json:"value"`
		Created  int64   `json:"created"`  // 创建时间
		Updated  int64   `json:"updated"`  // 更新时间
		Svc      string  `json:"svc"`      // 发起的服务
		Args     []*Data `json:"args"`     // 回调函数参数
		Loop     int     `json:"loop"`     //	-1 无限循环 >= 0 循环次数
		Delay    int64   `json:"delay"`    // 延迟执行，单位毫秒
		Interval int64   `json:"interval"` // 每个循环间隔, 单位毫秒
		Times    int64   `json:"times"`    // 当前运行次数
	}

	entity struct {
		datas  map[string]*Data
		timers map[string]*Timer
	}
)

func (ins *entity) Init(name string, pid *actor.PID) (err error) {
	return
}

func (ins *entity) Start(name string, pid *actor.PID) {
	go ins.dispatchTimer()
	go ins.dispatchExipre()
}

func (ins *entity) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

// 定时器循环
func (ins *entity) dispatchTimer() {
	// TODO 使用时间轮触发事件
}

// 数据过期
func (ins *entity) dispatchExipre() {
	// TODO 使用时间轮过期数据
}

// 创建数据
func (ins *entity) CreateData(data *Data) {
	// TODO创建数据
}

//  获取数据
func (ins *entity) GetData(key string) {
	// TODO 获取数据
}

// 设置数据
func (ins *entity) SetData(data *Data) {
	// TODO设置数据
}

// 创建定时器
func (ins *entity) CreateTimer(timer *Timer) {
	// TODO 创建定时器
}

//取消定时器
func (ins *entity) CancelTimer(key string) {
	// TODO 取消定时器
}

// ===自定义消息处理方法===

func unmarshal(args ...string) (datas map[string]*Data,
	timers map[string]*Timer, err error) {
	datas = make(map[string]*Data)
	timers = make(map[string]*Timer)
	l := len(args)
	if l >= 1 {
		if args[0] != "" {
			err = json.Unmarshal([]byte(args[0]), &datas)
			if err != nil {
				return
			}
		}
	}

	if l == 2 {
		if args[1] != "" {
			err = json.Unmarshal([]byte(args[1]), &timers)
			if err != nil {
				return
			}
		}
	}
	return
}

func NewEntityService(name string, args ...string) (pid *actor.PID, err error) {
	datas, timers, err := unmarshal(args...)
	if err != nil {
		return
	}
	svc := &entity{
		datas,
		timers,
	}
	svcName := fmt.Sprintf("%s%s", name, cv.SERVICE.ENTITY)
	return skynet.NewService(svcName, svc)
}
