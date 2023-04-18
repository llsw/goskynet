package actor

import (
	"fmt"
	"reflect"
	"runtime"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	share "github.com/llsw/goskynet/lib/share"
	utils "github.com/llsw/goskynet/lib/utils"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
)

type (
	RedisConf struct {
		Host       string `yaml:"host"`
		Port       int    `yaml:"port"`
		Db         int    `yaml:"db"`
		Auth       string `yaml:"auth"`       // 空字符串默认没有密码
		ReplicaSet int    `yaml:"replicaSet"` // 副本数，一个db开多少个service
		Parallel   int    `yaml:"parallel"`   // 每个service同时执行多少个查询
		// 查询执行超时，单位毫秒，
		// 0 默认值3秒，-1 无超时，无限期的阻塞 -2 不进行超时设置，不调用 SetWriteDeadline 方法
		ReadTimeout int `yaml:"readTimeout"`
		// 设置执行超时，单位毫秒，
		//0 默认值3秒，-1 无超时，无限期的阻塞 -2 不进行超时设置，不调用 SetWriteDeadline 方法
		WriteTimeout int `yaml:"writeTimeout"`
		// PoolSize     int `yaml:"poolSize"` //  连接池大小， 0 默认10 * runtime.GOMAXPROCS 开太多未必好
		// PoolTimeout 代表如果连接池所有连接都在使用中，等待获取连接时间，超时将返回错误
		// 0 默认是 1秒+ReadTimeout
		PoolTimeout int `yaml:"poolTimeout"`

		// // 连接池保持的最小空闲连接数，它受到PoolSize的限制
		// // 默认为0，不保持
		// MinIdleConns int `yaml:"minIdleConns"`

		// // 连接池保持的最大空闲连接数，多余的空闲连接将被关闭
		// // 默认为0，不限制
		// MaxIdleConns int `yaml:"maxIdleConns"`

		// ConnMaxIdleTime 是最大空闲时间，超过这个时间将被关闭。
		// 如果 ConnMaxIdleTime <= 0，则连接不会因为空闲而被关闭。
		// 默认值是30分钟，0禁用
		// ConnMaxIdleTime int `yaml:"connMaxIdleTime"`
	}

	RedisCrud func(db *redis.Client, args ...interface{}) *share.Res

	Redis struct {
		db    *redis.Client
		req   share.ReqChan
		conf  *RedisConf
		cruds map[string]map[string]*share.Method
		gp    *share.GroutinePool
	}
)

// ===必须实现===
func (act *Redis) Init(name string, pid *actor.PID) (err error) {
	return
}

func (act *Redis) Start(name string, pid *actor.PID) {
	act.Open()
}

func (act *Redis) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (act *Redis) dispatch() {
	for a := range act.req {
		// 设置执行超时要换成goroutine,
		// 如果并发很高的话groutine太多
		// 可以使用goroutine对象池
		// 在池子里面获取到goroutine再开启goroutine
		// 这样能防止高并发下groutine暴涨
		act.gp.Job(0, func(args ...interface{}) {
			a := args[0].(*share.Req)
			a.Res <- a.Act()
		}, func(err error, args ...interface{}) {
			a := args[0].(*share.Req)
			a.Res <- &share.Res{
				Err: errors.Wrap(err, "crud error"),
			}
		}, a)
	}
}

func (act *Redis) Call(dao string, crud string,
	args ...interface{}) (resChan share.ResChan) {
	resChan = make(share.ResChan)
	var cb share.Act = func() (res *share.Res) {
		defer share.Recover(func(err error) {
			res = &share.Res{Err: err}
		})
		if d, ok := act.cruds[dao]; ok {
			if f, ok := d[crud]; ok {
				l := len(args)
				wrap := make([]interface{}, l+1)
				wrap[0] = act
				wrap[1] = act.db
				for i := 0; i < l; i++ {
					wrap[i+1] = args[i]
				}
				res = f.Call(wrap...)
			} else {
				res = &share.Res{
					Err: fmt.Errorf("crud:%s not found", crud),
				}
			}
		} else {
			res = &share.Res{
				Err: fmt.Errorf("dao:%s not found", dao),
			}
		}
		return
	}
	action := &share.Req{
		Act: cb,
		Res: resChan,
	}
	act.req <- action
	return
}

func (act *Redis) Open() {
	for i := 0; i < act.conf.Parallel; i++ {
		go act.dispatch()
	}
}

func openRedis(conf *RedisConf) (db *redis.Client) {
	var err error
	db = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", conf.Host, conf.Port),
		Password:     conf.Auth, // 没有密码，默认值
		DB:           conf.Db,   // 默认DB 0
		MinIdleConns: runtime.NumCPU(),
		ReadTimeout:  time.Duration(conf.ReadTimeout) * time.Millisecond,
		WriteTimeout: time.Duration(conf.WriteTimeout) * time.Millisecond,
	})

	if err != nil {
		hlog.Fatalf("redis init failed, err: ", err)
		return
	}
	hlog.Info("redis init ok")
	return
}

func NewRedisService(svcName string, conf *RedisConf, receiver ...interface{}) {
	for i := 0; i < conf.ReplicaSet; i++ {
		db := openRedis(conf)
		cruds := make(map[string]map[string]*share.Method)

		for _, v := range receiver {
			val := reflect.Indirect(reflect.ValueOf(v))
			dao := val.Type().Name()
			cruds[dao] = utils.Register(v)
		}

		svc := &Redis{
			db:    db,
			conf:  conf,
			req:   make(share.ReqChan, conf.Parallel),
			cruds: cruds,
			gp:    share.GreateGroutinePool(10),
		}
		skynet.NewService(svcName, svc)
	}
}
