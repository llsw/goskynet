package actor

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

var lock = &sync.Mutex{}

type Method struct {
	rcvr   reflect.Value
	method reflect.Method
}

func (m *Method) call(req ...interface{}) (resp []interface{}, err error) {
	num := m.method.Type.NumIn()
	in := make([]reflect.Value, num)
	in[0] = m.rcvr
	if len(req) != num-1 {
		err = fmt.Errorf("call method:%s error: args number need:%d actually:%d", m.method.Name, num, len(req))
		return
	}
	for i := 1; i < num; i++ {
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
func (h *Acotr) Receive(ctx actor.Context) {
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

type ActorGroup struct {
	Name    string
	Actors  []*actor.PID
	Balance int
}

type NewResult struct {
	Pid *actor.PID
	Err error
}

type Service struct {
	actors map[string]*ActorGroup
	names  map[*actor.PID]string
	system *actor.ActorSystem
}

var singleInstance *Service

func GetInstance() *Service {
	if singleInstance == nil {
		lock.Lock()
		defer lock.Unlock()
		if singleInstance == nil {
			singleInstance = &Service{
				actors: make(map[string]*ActorGroup),
				names:  make(map[*actor.PID]string),
				system: actor.NewActorSystem(),
			}
		}
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

func CallMethod(pid string, cmd string, args ...interface{}) (resp []interface{}, err error) {
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

func (s *Service) NewService(name string, service AcotrService) (*actor.PID, error) {
	newDone := make(chan *NewResult)
	go func() {
		var err error
		act := &Acotr{}
		done := &NewResult{}
		props := actor.PropsFromProducer(func() actor.Actor { return act })
		pid := s.system.Root.Spawn(props)
		err = service.Init(name, pid)
		if err != nil {
			hlog.Errorf(
				"service start error name:%d error:%s",
				name, err.Error(),
			)
			done.Err = err
			newDone <- done
			return
		}
		s.names[pid] = name
		if ag, ok := s.actors[name]; ok {
			s.actors[name].Actors = append(ag.Actors, pid)
		} else {
			s.actors[name] = &ActorGroup{
				Name:    name,
				Actors:  make([]*actor.PID, 0, 1),
				Balance: 0,
			}
			s.actors[name].Actors = append(s.actors[name].Actors, pid)
		}
		// 注册回调
		Register(pid.Id, service)
		go service.Start(name, pid)
		hlog.Infof(
			"service start name:%s pid:%s all:%d",
			name, pid.Id, len(s.actors[name].Actors),
		)
		// if err != nil {
		// 	hlog.Errorf(
		// 		"service run error name:%d error:%s",
		// 		name, err.Error(),
		// 	)
		// 	return
		// }

		done.Pid = pid
		newDone <- done
	}()
	done := <-newDone
	close(newDone)
	return done.Pid, done.Err

}

func (s *Service) Call(pidOrName interface{}, cmd string, args ...interface{}) (interface{}, error) {
	message := &Msg{
		Cmd:  cmd,
		Args: args,
	}
	ctx := s.system.Root
	switch v := pidOrName.(type) {
	case string:
		if ag, ok := s.actors[v]; ok {
			ag.Balance = (ag.Balance + 1) % len(s.actors)
			act := ag.Actors[ag.Balance]
			if act == nil {
				return nil, fmt.Errorf("call service:%s not found", pidOrName)
			}
			return ctx.RequestFuture(act, message, 30*time.Second).Result()
		} else {
			return nil, fmt.Errorf("call service:%s not found", pidOrName)
		}
	case *actor.PID:
		resp, err := ctx.RequestFuture(v, message, 30*time.Second).Result()
		if err != nil {
			return nil, err
		}

		switch v := resp.(type) {
		case error:
			return nil, v
		default:
			return resp, nil
		}
	}
	return nil, fmt.Errorf("call pidOrName type:%v invalid", pidOrName)
}

func (s *Service) Send(pidOrName interface{}, message interface{}) (err error) {
	ctx := s.system.Root
	switch v := pidOrName.(type) {
	case string:
		if ag, ok := s.actors[v]; ok {
			ag.Balance = (ag.Balance + 1) % len(s.actors)
			act := ag.Actors[ag.Balance]
			if act == nil {
				err = fmt.Errorf("send service:%s not found", pidOrName)
				return
			}
			ctx.Request(act, message)
		} else {
			err = fmt.Errorf("send service:%s not found", pidOrName)
		}
		return
	case *actor.PID:
		ctx.Request(v, message)
	default:
		err = fmt.Errorf("send pidOrName type:%v invalid", pidOrName)
	}
	return
}

func Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *Msg:
		resp, err := CallMethod(ctx.Self().Id, msg.Cmd, msg.Args...)
		if err != nil {
			hlog.Errorf(
				"pid:%s receive msg call method err:%s",
				ctx.Self().Id, err.Error(),
			)
			ctx.Respond(err)
			return
		}
		ctx.Respond(resp)
	}
}