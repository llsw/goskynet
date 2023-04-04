package share

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type (
	Method struct {
		Rcvr   reflect.Value
		Method reflect.Method
	}
	Res struct {
		Data interface{}
		Err  error
	}

	ResChan chan *Res

	Act func() *Res

	Req struct {
		Act Act
		Res ResChan
	}
	ReqChan chan *Req

	GroutinePool struct {
		Parallel int
		jobChan  chan bool
	}
)

func (m *Method) Call(args ...interface{}) (res *Res) {
	res = &Res{}
	var err error
	var data interface{}
	defer func() {
		if e := recover(); e != nil {
			switch v := e.(type) {
			case error:
				res.Err = v
			case string:
				res.Err = fmt.Errorf("%s", v)

			}
			hlog.Debugf("call method args:%v err %s ", args, res.Err.Error())
		}
	}()
	actually := len(args)

	num := m.Method.Type.NumIn()
	in := make([]reflect.Value, actually+1)
	in[0] = m.Rcvr

	if actually < num-1 {
		res.Err = fmt.Errorf(
			"call method:%s error: args number need:%d actually:%d %v",
			m.Method.Name, num-1, actually, args,
		)
		return
	}

	for i := 1; i < actually+1; i++ {
		in[i] = reflect.ValueOf(args[i-1])
	}
	temp := m.Method.Func.Call(in)
	data = temp[0].Interface()
	err = temp[1].Interface().(error)
	res.Data = data
	res.Err = err
	return
}

func Recover(f func(error)) {
	var err error
	if e := recover(); e != nil {
		switch v := e.(type) {
		case error:
			err = v
		case string:
			err = fmt.Errorf("%s", v)
		}
	}
	if err != nil {
		f(err)
	}
}

func (g *GroutinePool) take() {
	g.jobChan <- false
}

func (g *GroutinePool) recycle() {
	<-g.jobChan
}

func (g *GroutinePool) Job(
	timeout int,
	job func(args ...interface{}),
	jobError func(err error, args ...interface{}), args ...interface{}) {

	g.take()
	go func() {
		lock := new(sync.Mutex)
		done := false
		finish := func(err error) {
			if !done {
				lock.Lock()
				defer lock.Unlock()
				if !done {
					done = true
					g.recycle()
					if err != nil {
						jobError(err, args...)
					}
				}
			}
		}
		defer Recover(
			func(err error) {
				finish(err)
			},
		)
		if timeout > 0 {
			time.AfterFunc((time.Duration(timeout) * time.Second),
				func() {
					finish(fmt.Errorf("job timeout"))
				},
			)
		}

		job(args...)
		finish(nil)
	}()
}

func GreateGroutinePool(parallel int) *GroutinePool {
	g := &GroutinePool{
		Parallel: parallel,
		jobChan:  make(chan bool, parallel),
	}
	return g
}
