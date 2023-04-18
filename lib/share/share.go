package share

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/pkg/errors"
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
	defer Recover(func(err error) {
		res.Err = err
	})
	actually := len(args)
	// num := m.Method.Type.NumIn()
	in := make([]reflect.Value, actually+1)
	in[0] = m.Rcvr

	for i := 0; i < actually; i++ {
		in[i+1] = reflect.ValueOf(args[i])
	}
	temp := m.Method.Func.Call(in)
	res = temp[0].Interface().(*Res)
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
		err = errors.Wrap(err, "recover error")
		hlog.Errorf("%+v", err)
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
		var ti *time.Timer
		if timeout > 0 {
			ti = time.AfterFunc((time.Duration(timeout) * time.Second),
				func() {
					finish(fmt.Errorf("job timeout"))
				},
			)
		}

		job(args...)
		if ti != nil {
			if !ti.Stop() {
				<-ti.C
			}
		}
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
