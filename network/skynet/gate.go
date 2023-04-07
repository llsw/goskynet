package skynet

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/netpoll"
	"github.com/cloudwego/hertz/pkg/network/standard"
	rawnet "github.com/cloudwego/netpoll"
	share "github.com/llsw/goskynet/lib/share"
	utils "github.com/llsw/goskynet/lib/utils"
)

type (
	Gate struct {
		addr        string
		transporter network.Transporter
		conns       *sync.Map
		connLock    sync.Mutex
		countLock   sync.Mutex
		onConnect   OnConnect
		onAccept    OnAccept
		onClose     OnClose
		onMsg       OnMsg
		onUnpack    OnUnpack
		reqChan     chan *Req
	}

	GateConn struct {
		lastSession  uint32
		reqLargePkg  *sync.Map
		reqLargeLock sync.Mutex
		wirteLock    sync.Mutex
		fd           int
		gp           *share.GroutinePool
	}

	Req struct {
		ctx      context.Context
		conn     network.Conn
		gateConn *GateConn
		data     interface{}
	}

	OnConnect func(fd int, conn network.Conn)
	OnAccept  func(conn net.Conn)
	OnClose   func(fd int, conn network.Conn)
	OnMsg     func(req *Req)
	OnUnpack  func(msg []byte, sz int) (data interface{}, err error)
)

var (
	cpuNum = runtime.NumCPU()
)

func (g *Gate) dispatchReqOnce(req *Req) {
	if g.onMsg != nil {
		req.gateConn.gp.Job(20, func(args ...interface{}) {
			g.onMsg(req)
		}, func(err error, args ...interface{}) {
		})
	}
}

// func (g *Gate) setRuning(gc *GateConn, running bool, id int) int {
// 	if !running && gc.runId != id {
// 		return gc.runId
// 	}
// 	gc.dispatchLock.Lock()
// 	defer gc.dispatchLock.Unlock()

// 	if !running && gc.runId != id {
// 		return gc.runId
// 	}
// 	if running && !gc.running {
// 		gc.reqChan = make(chan *Req, 100)
// 		gc.runId++
// 		go g.dispatchReq(gc, gc.runId)
// 		hlog.Debugf("open run id:%d", gc.runId)
// 	}
// 	if !running {
// 		close(gc.reqChan)
// 	}
// 	gc.running = running
// 	return gc.runId
// }

func (g *Gate) dispatchReq() {
	for r := range g.reqChan {
		g.dispatchReqOnce(r)
	}
}

func (g *Gate) socket(gateConn *GateConn,
	ctx context.Context, conn network.Conn) (err error) {
	defer utils.Recover(func(err error) {
		if gateConn != nil && gateConn.reqLargePkg != nil {
			gateConn.reqLargePkg.Delete(gateConn.lastSession)
		}
	})
	defer conn.Release()
	reqNum := 0
	for {
		var (
			szh  []byte
			buf  []byte
			skip int
		)
		skip = 2
		szh, err = conn.Peek(skip)
		if err != nil {
			return
		}
		sz := binary.BigEndian.Uint16(szh)
		isz := int(sz)
		skip = isz + 2
		rl := conn.Len()
		if rl < skip {
			reqNum++
			// if reqNum > 100 {
			// 	conn.Skip(rl)
			// 	err = fmt.Errorf(
			// 		"pack length error need:%d actual:%d", skip, rl,
			// 	)
			// 	return
			// }
			// hlog.Debugf("sz:%d rl:%d", isz, rl)
			continue
		}
		buf, err = conn.Peek(skip)
		if err != nil {
			return
		}
		err = conn.Skip(skip)
		if err != nil {
			return
		}
		d := buf[2:skip]
		var cd interface{}

		if g.onUnpack != nil {
			cd, err = g.onUnpack(d, isz)
			if err != nil {
				// conn.Skip(rl)
				return
			}
		}

		if gateConn != nil {
			g.reqChan <- &Req{
				gateConn: gateConn,
				ctx:      ctx,
				conn:     conn,
				data:     cd,
			}
		}
		// if rl <= isz {
		// 	return
		// }
	}
}

func (g *Gate) socketStream(ctx context.Context,
	conn network.StreamConn) (err error) {
	// TODO 如果需要steam的话
	return
}

/**
标准网络库需要自己循环调用onData，不然只有第一次连接的时候会调用一次onData
netpoll库有epoll事件循环，会自动调用onData
**/
func (g *Gate) onData(ctx context.Context, conn interface{}) (err error) {
	switch conn := conn.(type) {
	case network.Conn:
		switch v := conn.(type) {
		case *netpoll.Conn:
			rawConn, _ := getRawConn(conn)
			gc := g.getConn(rawConn.Fd())
			err = g.socket(gc, ctx, v)

			// case *standard.Conn:
			// 	// TODO 标准网络获取fd
			// 	rawConn, _ := getRawConn(conn)
			// 	cc := g.initConn(rawConn.Fd())
			// 	for {
			// 		err = g.socket(cc, ctx, v)
			// 		if err != nil {
			// 			hlog.Errorf("on data err:%s\n", err.Error())
			// 		}
			// 	}
		}
	case network.StreamConn:
		err = g.socketStream(ctx, conn)
	}
	if err != nil {
		hlog.Errorf("on data err:%s\n", err.Error())
	}
	return
}

func (g *Gate) Response(cc *GateConn,
	conn network.Conn, msgs []*MsgPart) (err error) {
	cc.wirteLock.Lock()
	done := false
	finish := func() {
		if !done {
			done = true
			cc.wirteLock.Unlock()
		}
	}
	defer finish()
	defer utils.Recover(func(err error) {
		finish()
	})

	for _, v := range msgs {
		_, err = conn.WriteBinary(*v.Msg)
		if err != nil {
			return
		}
	}
	// TODO 是否做成定时flush
	// 要flush才会把包发出去
	err = conn.Flush()
	return
}

var connNum = 0

func (g *Gate) connCount(num int) {
	g.countLock.Lock()
	defer g.countLock.Unlock()
	connNum = connNum + num
}

func (g *Gate) getConn(fd int) (gc *GateConn) {
	if v, ok := g.conns.Load(fd); ok {
		gc = v.(*GateConn)
	} else {
		g.connLock.Lock()
		defer g.connLock.Unlock()
		gc = &GateConn{
			lastSession: 0,
			reqLargePkg: new(sync.Map),
			fd:          fd,
			// 每个连接最多同时能开启多少个goroutine处理理多少个请求
			gp: share.GreateGroutinePool(2 * cpuNum),
		}
		g.conns.Store(fd, gc)
		g.connCount(1)
	}
	return gc
}

func genOpts(opts ...config.Option) *config.Options {
	return config.NewOptions(opts)
}

func getRawConn(conn network.Conn) (rawnet.Conn, rawnet.Connection) {
	switch v := conn.(type) {
	case *standard.Conn:
		return nil, nil
	case *netpoll.Conn:
		conn1 := v
		conn2 := conn1.Conn.(rawnet.Conn)
		conn3 := conn1.Conn.(rawnet.Connection)
		return conn2, conn3
	}
	return nil, nil
}

func (g *Gate) newOpts() *config.Options {
	// server.WithOnConnect 可以注册Connect回调
	// server.WithOnAccept 可以注册Accept回调
	return genOpts(
		server.WithHostPorts(g.addr),
		server.WithKeepAlive(true),
		server.WithReadTimeout(20*time.Second),
		server.WithWriteTimeout(20*time.Second),
		server.WithOnConnect(
			func(ctx context.Context, conn network.Conn) context.Context {
				rawConn, rwaConnection := getRawConn(conn)
				fd := rawConn.Fd()
				// cc := g.getConn(fd)

				if rwaConnection != nil {
					rwaConnection.AddCloseCallback(
						func(connection rawnet.Connection) error {
							// close(cc.reqChan)
							g.conns.Delete(fd)
							// delete(g.conns, fd)
							g.connCount(-1)
							rawConn, _ := getRawConn(conn)
							if rawConn != nil {
								hlog.Debugf("on close fd:%d", fd)
							}
							if g.onClose != nil {
								g.onClose(fd, conn)
							}
							return nil
						})
				}
				if g.onConnect != nil {
					g.onConnect(fd, conn)
				}
				return ctx
			}),
		server.WithOnAccept(func(conn net.Conn) context.Context {
			if g.onAccept != nil {
				// TODO FD
				g.onAccept(conn)
			}
			return context.Background()
		}),
	)
}
func (g *Gate) SetOnConnect(f OnConnect) {
	g.onConnect = f
}

func (g *Gate) SetOnAccept(f OnAccept) {
	g.onAccept = f
}

func (g *Gate) SetOnClose(f OnClose) {
	g.onClose = f
}

func (g *Gate) SetOnUnpack(f OnUnpack) {
	g.onUnpack = f
}

func (g *Gate) SetOnMsg(f OnMsg) {
	g.onMsg = f
}

func (g *Gate) ListenAndServe() (err error) {
	// addr代表不监听请求，只处理发送, 可以多开几个cluster发送请求
	if g.addr == "" {
		return
	}
	opts := g.newOpts()
	switch runtime.GOOS {
	case "linux":
		g.transporter = netpoll.NewTransporter(opts)
	case "darwin":
		g.transporter = netpoll.NewTransporter(opts)
		// 标准网络库需要自己循环onData，不然只有第一次连接的时候会调用一次onData
		// c.transporter = standard.NewTransporter(opts)
	case "windows":
		g.transporter = standard.NewTransporter(opts)
	}
	err = g.transporter.ListenAndServe(g.onData)
	return
}

func NewGate(addr string) *Gate {
	g := &Gate{
		addr:    addr,
		conns:   new(sync.Map),
		reqChan: make(chan *Req, 2000),
	}
	// 派发网络包的协程数量，太多未必快
	num := cpuNum
	hlog.Debugf("gate dispatch groutine num:%d", num)
	for i := 0; i < num; i++ {
		go g.dispatchReq()
	}
	return g
}
