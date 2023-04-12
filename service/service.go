package actor

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	share "github.com/llsw/goskynet/lib/share"
	utils "github.com/llsw/goskynet/lib/utils"
	"github.com/pkg/errors"
)

var onceIns sync.Once

type CbFun = func(interface{}, error)

type Method struct {
	rcvr   reflect.Value
	method reflect.Method
}

func (m *Method) call(req ...interface{}) (resp []interface{}, err error) {
	defer share.Recover(func(err error) {
		hlog.Debugf("call req:%v err %s ", req, err.Error())
	})
	actually := len(req)
	in := make([]reflect.Value, actually+1)
	in[0] = m.rcvr

	for i := 1; i < actually+1; i++ {
		in[i] = reflect.ValueOf(req[i-1])
	}
	temp := m.method.Func.Call(in)
	l := len(temp)
	resp = make([]interface{}, l)
	for i, v := range temp {
		resp[i] = v.Interface()
	}
	return
}

var methods = make(map[string]map[string]*Method)

type Msg struct {
	Cmd  string
	Args []interface{}
}

type Acotr struct {
}

// 收到消息
func (a *Acotr) Receive(ctx actor.Context) {
	Receive(ctx)
}

type AcotrService interface {
	// 服务初始化
	Init(name string, pid *actor.PID) error
	// 服务启动
	Start(name string, pid *actor.PID)
	// TODO,还没有stop的使用
	// 服务停止
	Stop(name string, pid *actor.PID) error
}

type Lock struct {
	Id   int
	Lock chan bool
}

type ActorGroup struct {
	Name         string
	Actors       []*actor.PID
	Balance      int
	BalancelLock sync.Mutex
	Lock         *Lock
}

type Service struct {
	actors map[string]*ActorGroup
	names  map[*actor.PID]string
	system *actor.ActorSystem
}

var singleInstance *Service

func GetInstance() *Service {
	if singleInstance == nil {
		onceIns.Do(func() {
			if singleInstance == nil {
				singleInstance = &Service{
					actors: make(map[string]*ActorGroup),
					names:  make(map[*actor.PID]string),
					system: actor.NewActorSystem(),
				}
			}
		})
	}
	return singleInstance
}

func doRegister(pid string, rcvr reflect.Value, typ reflect.Type) error {
	if _, ok := methods[pid]; !ok {
		methods[pid] = make(map[string]*Method)
	}
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		meth := &Method{
			rcvr:   rcvr,
			method: method,
		}
		methods[pid][method.Name] = meth
	}
	return nil
}

func CallMethod(pid string, cmd string,
	args ...interface{}) (resp []interface{}, err error) {
	if _, ok := methods[pid]; !ok {
		err = fmt.Errorf("service:%s cmd:%s not found", pid, cmd)
		return
	}

	if _, ok := methods[pid][cmd]; !ok {
		err = fmt.Errorf("service:%s cmd:%s not found", pid, cmd)
		return
	}

	return methods[pid][cmd].call(args...)
}

func Register(pid string, receiver interface{}) error {
	typ := reflect.TypeOf(receiver)
	rcvr := reflect.ValueOf(receiver)

	if err := doRegister(pid, rcvr, typ); err != nil {
		return err
	}
	// if err := doRegister(pid, rcvr, reflect.PtrTo(typ)); err != nil {
	// 	return err
	// }
	return nil
}

func (s *Service) newService(name string,
	service AcotrService) (pid *actor.PID, err error) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		act := &Acotr{}
		props := actor.PropsFromProducer(func() actor.Actor { return act })
		pid = s.system.Root.Spawn(props)
		err = service.Init(name, pid)
		if err != nil {
			wg.Done()
			return
		}
		s.names[pid] = name
		if ag, ok := s.actors[name]; ok {
			s.actors[name].Actors = append(ag.Actors, pid)
		} else {
			lock := &Lock{
				Id:   0,
				Lock: make(chan bool, 1),
			}
			s.actors[name] = &ActorGroup{
				Name:    name,
				Actors:  make([]*actor.PID, 0, 1),
				Balance: 0,
				Lock:    lock,
			}
			s.actors[name].Actors = append(s.actors[name].Actors, pid)
		}
		// 注册回调
		err = Register(pid.Id, service)
		if err != nil {
			wg.Done()
			return
		}
		go service.Start(name, pid)
		wg.Done()
	}()
	wg.Wait()
	if err != nil {
		hlog.Errorf(
			"service start error name:%d error:%s",
			name, err.Error(),
		)
	} else {
		hlog.Infof(
			"service start ok name:%s pid:%s",
			name, pid.Id,
		)
	}
	return
}

func (s *Service) NewService(name string,
	service AcotrService) (pid *actor.PID, err error) {
	if name == "cluster" {
		err = fmt.Errorf("can not use cluster by service name")
		return
	}
	return s.newService(name, service)
}

func (s *Service) callByPid(ctx *actor.RootContext,
	pid *actor.PID, cmd string, message *Msg) (interface{}, error) {
	resp, err := ctx.RequestFuture(pid, message, 30*time.Second).Result()
	switch rt := resp.(type) {
	// 集群那边的报错
	case []interface{}:
		lrt := len(rt)
		if lrt > 0 {
			switch v := rt[lrt-1].(type) {
			case error:
				err = v
			}
		}
	case interface{}:
		switch v := rt.(type) {
		case error:
			err = v
		}
	}
	if err != nil {
		if cmd != "Call" {
			hlog.Errorf(
				"service call:%v cmd:%s error:%s",
				s.names[pid], cmd, err.Error())
		}
	}
	return resp, err
}

func (s *Service) callByPidNoBlock(ctx *actor.RootContext,
	pid *actor.PID, cmd string, message *Msg, callback CbFun) {
	index := len(message.Args) - 1
	message.Args[index] = func(resp interface{}, err error) {
		var res []interface{}
		switch rt := resp.(type) {
		// 集群那边的报错
		case []interface{}:
			lrt := len(rt)
			if lrt > 0 {
				switch v := rt[lrt-1].(type) {
				case error:
					err = v
				}
			}
			res = rt
		case interface{}:
			switch v := rt.(type) {
			case error:
				err = v
			}
		}

		if err != nil {
			if cmd != "Call" {
				hlog.Errorf(
					"service call:%v cmd:%s error:%s",
					s.names[pid], cmd, err.Error())
			}
		}
		callback(res, err)
	}
	ctx.RequestFuture(pid, message, 30*time.Second)
}

func (s *Service) getActor(ag *ActorGroup, name string) *actor.PID {
	ag.BalancelLock.Lock()
	defer ag.BalancelLock.Unlock()
	ag.Balance = (ag.Balance + 1) % len(ag.Actors)
	return ag.Actors[ag.Balance]
}

func (s *Service) callByName(ctx *actor.RootContext,
	name string, cmd string, message *Msg) (interface{}, error) {
	if ag, ok := s.actors[name]; ok {
		pid := s.getActor(ag, name)
		if pid == nil {
			return nil, fmt.Errorf(
				"call service:%s not found", name)
		}
		return s.callByPid(ctx, pid, cmd, message)
	} else {
		return nil, fmt.Errorf("call service:%s not found", name)
	}
}

func (s *Service) callByNameNoBlock(ctx *actor.RootContext,
	name string, cmd string, message *Msg, cb CbFun) {
	if ag, ok := s.actors[name]; ok {
		pid := s.getActor(ag, name)
		if pid == nil {
			cb(nil, fmt.Errorf("call service:%s not found", name))
			return
		}
		s.callByPidNoBlock(ctx, pid, cmd, message, cb)
	} else {
		cb(nil, fmt.Errorf("call service:%s not found", name))
	}
}

func (s *Service) Call(pidOrName interface{},
	cmd string, args ...interface{}) (interface{}, error) {
	message := &Msg{
		Cmd:  cmd,
		Args: args,
	}
	ctx := s.system.Root
	switch v := pidOrName.(type) {
	case string:
		return s.callByName(ctx, v, cmd, message)
	case *actor.PID:
		return s.callByPid(ctx, v, cmd, message)
	}
	return nil, fmt.Errorf("call pidOrName type:%v invalid", pidOrName)
}

func (s *Service) CallNoBlock(pidOrName interface{},
	cmd string, args ...interface{}) {
	ll := len(args)
	cb := args[ll-1].(CbFun)
	message := &Msg{
		Cmd:  cmd,
		Args: args,
	}
	ctx := s.system.Root
	switch v := pidOrName.(type) {
	case string:
		s.callByNameNoBlock(ctx, v, cmd, message, cb)
	case *actor.PID:
		s.callByPidNoBlock(ctx, v, cmd, message, cb)
	}
}

func (s *Service) sendByName(ctx *actor.RootContext,
	name string, cmd string, message *Msg) error {
	if ag, ok := s.actors[name]; ok {
		pid := s.getActor(ag, name)
		if pid == nil {
			return fmt.Errorf(
				"call service:%s not found", name)
		}
		ctx.Request(pid, message)
		return nil
	} else {
		return fmt.Errorf("call service:%s not found", name)
	}
}

func (s *Service) Send(pidOrName interface{},
	cmd string, args ...interface{}) (err error) {
	message := &Msg{
		Cmd:  cmd,
		Args: args,
	}
	ctx := s.system.Root
	switch v := pidOrName.(type) {
	case string:
		return s.sendByName(ctx, v, cmd, message)
	case *actor.PID:
		ctx.Request(v, message)
	default:
		err = fmt.Errorf("send pidOrName type:%v invalid", pidOrName)
	}
	return
}

func (s *Service) Lock(pid *actor.PID, timeout int) (lockId int, err error) {
	if name, ok := s.names[pid]; ok {
		if ag, ok := s.actors[name]; ok {
			// 请求锁
			ag.Lock.Lock <- true
			ag.Lock.Id++
			lockId = ag.Lock.Id
			time.AfterFunc(time.Duration(timeout), func() {
				s.Unlock(pid, lockId)
			})
		} else {
			err = fmt.Errorf("lock service:%s not found", name)
		}
	} else {
		err = fmt.Errorf("lock pid:%s service name not found", pid.Id)
	}
	return
}

func (s *Service) Unlock(pid *actor.PID, lockId int) (err error) {
	if name, ok := s.names[pid]; ok {
		if ag, ok := s.actors[name]; ok {
			// 避免超时解了别人的锁，不一定加了锁就不会超卖，经典问题
			// 这里还有问题，有可能刚解锁，加锁的线程还没来得及对锁id进行++，
			// 也会出现解了别人锁的情况，本来对锁id的访问应该也加锁
			// 但一般锁的超时时间定的都比业务执行时间长得多，
			// 所以基本上很少出现业务解锁和超时解锁同时进行的情况
			// 概率太低，对lockId的操作就不用锁了
			if ag.Lock.Id == lockId {
				// 使用select解锁，避免死锁
				select {
				case <-ag.Lock.Lock:
				default:
				}
			}
		} else {
			err = fmt.Errorf("unlock service:%s not found", name)
		}
	} else {
		err = fmt.Errorf("unlock pid:%s service name not found", pid.Id)
	}
	return
}

func Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *Msg:
		var f func(interface{}, error)
		args := msg.Args
		if msg.Cmd != "CallNoBlock" {
			ll := len(args) - 1
			if ll >= 0 {
				switch v := args[ll].(type) {
				case func(interface{}, error):
					f = v
					args = msg.Args[0:ll]
				}
			}
		}
		resp, err := CallMethod(ctx.Self().Id, msg.Cmd, args...)
		if err != nil {
			if f != nil {
				ctx.Respond(nil)
				f(nil, err)
			} else {
				ctx.Respond(err)
			}
		} else {
			if f != nil {
				ctx.Respond(nil)
				f(resp, err)
			} else {
				ctx.Respond(resp)
			}
		}
	}
}

func CallDb(db string, dao string, crud string,
	args ...interface{}) (res interface{}, err error) {
	defer share.Recover(func(e error) {
		err = e
	})
	args = utils.WrapInterface(dao, crud, args)
	data, err := skynet.Call(db, "Call", args...)
	if err != nil {
		return
	}
	ch := data.([]interface{})[0].(share.ResChan)
	r := <-ch
	res = r.Data
	err = r.Err
	return
}

type (
	SvcConf struct {
		ReplicaSet   int `yaml:"replicaSet"`   // 副本数，开多少个service
		Parallel     int `yaml:"parallel"`     // 每个service同时执行多少个call
		ActTimeout   int `yaml:"actTimeout"`   //执行超时，单位秒
		GroutinePool int `yaml:"groutinePool"` // 最多能开启多少个协程
	}

	Svc struct {
		Name    string
		Pid     *actor.PID
		conf    *SvcConf
		req     share.ReqChan
		methods map[string]map[string]*share.Method
		gp      *share.GroutinePool
	}
	ServiceMethod func(args ...interface{}) *share.Res
)

// ===必须实现===
func (act *Svc) Init(name string, pid *actor.PID) (err error) {
	act.Name = name
	act.Pid = pid
	return
}

func (act *Svc) Start(name string, pid *actor.PID) {
	act.Open()
}

func (act *Svc) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (act *Svc) dispatch() {
	for a := range act.req {
		// 设置执行超时要换成goroutine,
		// 如果并发很高的话groutine太多
		// 可以使用goroutine对象池
		// 在池子里面获取到goroutine再开启goroutine
		// 这样能防止高并发下groutine暴涨
		act.gp.Job(act.conf.ActTimeout, func(args ...interface{}) {
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

func (act *Svc) CallNoBlock(mod string, method string,
	args ...interface{}) (resChan share.ResChan) {
	resChan = make(share.ResChan)
	var cb share.Act = func() (res *share.Res) {
		defer share.Recover(func(err error) {
			res = &share.Res{Err: err}
		})
		if ms, ok := act.methods[mod]; ok {
			if f, ok := ms[method]; ok {
				l := len(args) + 2
				wrap := make([]interface{}, l)
				wrap[0] = act
				for i := 1; i < l; i++ {
					wrap[i] = args[i-1]
				}
				res = f.Call(wrap...)
			} else {
				res = &share.Res{
					Err: fmt.Errorf("method:%s not found", method),
				}
			}
		} else {
			res = &share.Res{
				Err: fmt.Errorf("svc:%s mod:%s not found", act.Name, mod),
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

func (act *Svc) Call(mod string, method string,
	args ...interface{}) (res *share.Res) {
	defer share.Recover(func(err error) {
		res = &share.Res{Err: err}
	})
	if ms, ok := act.methods[mod]; ok {
		if f, ok := ms[method]; ok {
			l := len(args) + 2
			wrap := make([]interface{}, l)
			wrap[0] = act
			for i := 1; i < l; i++ {
				wrap[i] = args[i-1]
			}
			res = f.Call(wrap...)
		} else {
			res = &share.Res{
				Err: fmt.Errorf("method:%s not found", method),
			}
		}
	} else {
		res = &share.Res{
			Err: fmt.Errorf("svc:%s mod:%s not found", act.Name, mod),
		}
	}
	return

}

func (act *Svc) Open() {
	for i := 0; i < act.conf.Parallel; i++ {
		go act.dispatch()
	}
}

func NewSvc(svcName string, conf *SvcConf, receiver ...interface{}) {
	for i := 0; i < conf.ReplicaSet; i++ {
		methods := make(map[string]map[string]*share.Method)
		for _, v := range receiver {
			val := reflect.Indirect(reflect.ValueOf(v))
			mod := val.Type().Name()
			methods[mod] = utils.Register(v)
		}

		svc := &Svc{
			conf:    conf,
			req:     make(share.ReqChan, conf.Parallel),
			methods: methods,
			gp:      share.GreateGroutinePool(conf.GroutinePool),
		}
		skynet.NewService(svcName, svc)
	}
}
