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
	utils "github.com/llsw/goskynet/lib/utils"
)

type ClusterMsgHandler func(addr interface{}, session uint32, args ...interface{})

type ReqLargePkg struct {
	Addr interface{}
	Msgs []*MsgPart
}

type req struct {
	ctx     context.Context
	conn    network.Conn
	cc      *ClusterConn
	addr    interface{}
	session uint32
	data    *MsgPart
	padding bool
}
type ClusterConn struct {
	lastSession uint32
	reqLargePkg map[uint32]*ReqLargePkg
	reqChan     chan *req
	wirteLock   sync.Mutex
}

type Cluster struct {
	name        string
	addr        string
	channels    map[string]*Channel
	transporter network.Transporter
	conns       map[int]*ClusterConn
	handler     ClusterMsgHandler
	connLock    sync.Mutex
	countLock   sync.Mutex
}

func genOpts(opts ...config.Option) *config.Options {
	return config.NewOptions(opts)
}

func (c *Cluster) grabLargePkg(clusterConn *ClusterConn, conn network.Conn,
	session uint32, addr interface{}) *ReqLargePkg {
	if clusterConn == nil {
		// hlog.Debugf("clusterConn nil")
		return nil
	}

	if clusterConn.reqLargePkg == nil {
		// hlog.Debugf("reqLargePkg nil")
		return nil
	}

	// hlog.Debugf("dispatchReqOnce reqLargePkg session%d addr:%s", session, conn.RemoteAddr())

	if msgs, ok := clusterConn.reqLargePkg[session]; ok {
		clusterConn.lastSession = session
		return msgs
	} else {
		// clusterConn.once.Do(func() {
		// TODO 可以用对象池
		clusterConn.reqLargePkg[session] = &ReqLargePkg{
			Addr: addr,
			Msgs: make([]*MsgPart, 0, 1),
		}
		// })
		clusterConn.lastSession = session
		return clusterConn.reqLargePkg[session]
	}
}

func defalutHandler(addr interface{}, session uint32, args ...interface{}) {
	// 创建一个lua数据，包含nil，bool，int64，float64，string，切片和映射
	// L := map[interface{}]interface{}{
	// 	"nil":    nil,
	// 	"bool":   true,
	// 	"int64":  int64(123),
	// 	"float64": float64(3.14),
	// 	"string": "hello",
	// 	"slice":  []interface{}{int64(1), int64(2), int64(3)},
	// 	"map":    map[interface{}]interface{}{"a": int64(1), "b": int64(2)}, // 这是你需要补充的部分
	// }
	resps := make([]interface{}, 10)
	array := []interface{}{1, 2, 3}
	table := make(map[string]interface{})
	table["who"] = "ikun"
	tt := make(map[string]interface{})
	tt["hello"] = "world"
	table["table"] = tt
	table["array"] = array
	resps[0] = nil
	resps[1] = 0
	resps[2] = -1
	resps[3] = 1
	resps[4] = 123.123
	resps[5] = true
	resps[6] = array
	resps[7] = table
	cb := args[len(args)-1].(func([]interface{}, error))
	cb(resps, nil)
}

func (c *Cluster) response(cc *ClusterConn,
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

func (c *Cluster) msg(cc *ClusterConn, ctx context.Context, conn network.Conn,
	addr interface{}, session uint32, msgs []*MsgPart) {
	defer utils.Recover(func(err error) {})
	msg, sz, err := Concat(msgs)
	ok := true
	var msgsz int
	var resps []interface{}

	cb := func(resps []interface{}, err error) {
		defer utils.Recover(func(err error) {})
		if err != nil {
			ok = false
			resps = []interface{}{err.Error()}
		}

		msg, msgsz, err = Pack(resps)
		if err != nil {
			hlog.Errorf("pack msg error:%s resps:%v", err.Error(), resps)
			return
		}

		msgs, err = PackResponse(session, ok, msg, uint32(msgsz))
		if err != nil {
			hlog.Errorf(
				"pack response msg error:%s resps:%v", err.Error(), resps)
			return
		}

		err = c.response(cc, conn, msgs)
		if err != nil {
			hlog.Errorf(
				"write response msg error:%s resps:%v", err.Error(), resps)
		}
	}

	if err == nil {
		var args []interface{}
		args, err = Unpack(msg, sz)
		if err == nil {
			args = append(args, cb)
			c.handler(addr, session, args...)
			return
		}
	}

	cb(resps, err)
}

func (c *Cluster) dispatchReqOnce(req *req) {
	reqLargePkg := c.grabLargePkg(req.cc, req.conn, req.session, req.addr)
	if reqLargePkg == nil {
		// hlog.Debugf("dispatchReqOnce  reqLargePkg nil session%d addr:%s", req.session, req.conn.RemoteAddr())
		return
	}
	req.cc.reqLargePkg[req.session].Msgs = append(reqLargePkg.Msgs, req.data)

	if req.padding {
		return
	}
	pkg := req.cc.reqLargePkg[req.session]
	addr := pkg.Addr
	msgs := pkg.Msgs
	delete(req.cc.reqLargePkg, req.session)
	// hlog.Debugf("delete session%d addr:%s", req.session, req.conn.RemoteAddr())
	c.msg(req.cc, req.ctx, req.conn, addr, req.session, msgs)
}

func (c *Cluster) dispatchReq(reqChan chan *req) {
	for r := range reqChan {
		c.dispatchReqOnce(r)
	}
	hlog.Debugf("dispatch req loop end")
}

func (c *Cluster) socket(clusterConn *ClusterConn,
	ctx context.Context, conn network.Conn) (err error) {
	defer utils.Recover(func(err error) {
		if clusterConn != nil && clusterConn.reqLargePkg != nil {
			delete(clusterConn.reqLargePkg, clusterConn.lastSession)
		}
	})
	defer conn.Release()
	reqNum := 0
	for {
		var (
			szh     []byte
			buf     []byte
			skip    int
			addr    interface{}
			session uint32
			data    *MsgPart
			padding bool
		)
		skip = 2
		conn.SetReadTimeout(20 * time.Second)
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
		d := buf[2:isz]
		addr, session, data, padding, err = UnpcakRequest(&d, uint32(sz))
		if err != nil {
			// conn.Skip(rl)
			return
		}
		if clusterConn != nil {
			clusterConn.reqChan <- &req{
				cc:      clusterConn,
				ctx:     ctx,
				conn:    conn,
				addr:    addr,
				session: session,
				data:    data,
				padding: padding,
			}
		}

		// if rl <= isz {
		// 	return
		// }
	}
}

func (c *Cluster) socketStream(ctx context.Context,
	conn network.StreamConn) (err error) {
	// TODO 如果需要steam的话
	return
}

/**
标准网络库需要自己循环调用onData，不然只有第一次连接的时候会调用一次onData
netpoll库有epoll事件循环，会自动调用onData
**/
func (c *Cluster) onData(ctx context.Context, conn interface{}) (err error) {
	switch conn := conn.(type) {
	case network.Conn:
		switch v := conn.(type) {
		case *netpoll.Conn:
			rawConn, _ := getRawConn(conn)
			cc := c.initConn(rawConn.Fd())
			err = c.socket(cc, ctx, v)
			// case *standard.Conn:
			// 	// TODO 标准网络获取fd
			// 	rawConn, _ := getRawConn(conn)
			// 	cc := c.initConn(rawConn.Fd())
			// 	for {
			// 		err = c.socket(cc, ctx, v)
			// 		if err != nil {
			// 			hlog.Errorf("on data err:%s\n", err.Error())
			// 		}
			// 	}
		}
	case network.StreamConn:
		err = c.socketStream(ctx, conn)
	}
	if err != nil {
		hlog.Errorf("on data err:%s\n", err.Error())
	}
	return
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

var connNum = 0

func (c *Cluster) connCount(num int) {
	c.countLock.Lock()
	defer c.countLock.Unlock()
	connNum = connNum + num
	// hlog.Debugf("conn num:%d", connNum)
}

func (c *Cluster) initConn(fd int) (cc *ClusterConn) {
	c.connLock.Lock()
	defer c.connLock.Unlock()
	if clusterConn, ok := c.conns[fd]; ok {
		cc = clusterConn
	} else {
		// TODO 可以用对象池
		cc = &ClusterConn{
			lastSession: 0,
			reqLargePkg: make(map[uint32]*ReqLargePkg),
			reqChan:     make(chan *req, 500),
		}
		c.conns[fd] = cc
		go c.dispatchReq(cc.reqChan)
		c.connCount(1)
	}
	return cc
}

func (c *Cluster) newOpts() *config.Options {
	// server.WithOnConnect 可以注册Connect回调
	// server.WithOnAccept 可以注册Accept回调
	return genOpts(
		server.WithHostPorts(c.addr),
		server.WithKeepAlive(true),
		server.WithOnConnect(
			func(ctx context.Context, conn network.Conn) context.Context {
				rawConn, rwaConnection := getRawConn(conn)
				fd := rawConn.Fd()
				cc := c.initConn(fd)

				if rwaConnection != nil {
					rwaConnection.AddCloseCallback(
						func(connection rawnet.Connection) error {
							close(cc.reqChan)
							delete(c.conns, fd)
							c.connCount(-1)
							rawConn, _ := getRawConn(conn)
							if rawConn != nil {
								hlog.Debugf("on close fd:%d", fd)
							}
							return nil
						})
				}
				// if rawConn != nil {
				// 	// hlog.Debugf("on connect fd:%d", rawConn.Fd())
				// }
				return ctx
			}),
		server.WithOnAccept(func(conn net.Conn) context.Context {
			// TODO WithOnAccept
			return context.Background()
		}),
	)
}

func (c *Cluster) ListenAndServe() (err error) {
	// addr代表不监听请求，只处理发送, 可以多开几个cluster发送请求
	if c.addr == "" {
		return
	}
	opts := c.newOpts()
	switch runtime.GOOS {
	case "linux":
		c.transporter = netpoll.NewTransporter(opts)
	case "darwin":
		c.transporter = netpoll.NewTransporter(opts)
		// 标准网络库需要自己循环onData，不然只有第一次连接的时候会调用一次onData
		// c.transporter = standard.NewTransporter(opts)
	case "windows":
		c.transporter = standard.NewTransporter(opts)
	}
	err = c.transporter.ListenAndServe(c.onData)
	return
}

func (c *Cluster) Open() (err error) {
	return c.ListenAndServe()
}

func (c *Cluster) getChannel(addr string) (channel *Channel, err error) {
	if ch, ok := c.channels[addr]; ok {
		channel = ch
		return
	} else {
		c.channels[addr], err = NewChannel(addr)
		if err != nil {
			hlog.Errorf(
				"node getChannel fail, node:%s, error:%s\n",
				addr, err.Error(),
			)
			return
		}
		channel = c.channels[addr]
	}
	return
}

func (c *Cluster) Call(node string, addr interface{},
	req ...interface{}) (resp []interface{}, err error) {
	channel, err := c.getChannel(node)
	if err != nil {
		return
	}
	// hlog.Debugf("addr:%v req:%v", addr, req)
	return channel.Call(addr, req...)
}

func (c *Cluster) CallNoBlock(node string, addr interface{},
	req ...interface{}) {
	channel, err := c.getChannel(node)
	if err != nil {
		f := req[len(req)-1].(CallbackFun)
		f(nil, err)
		return
	}
	channel.CallNoBlock(addr, req...)
}

// send没有回复
func (c *Cluster) Send(node string, addr interface{},
	req ...interface{}) (err error) {
	channel, err := c.getChannel(node)
	if err != nil {
		return
	}
	return channel.Invoke(addr, req)
}

func (c *Cluster) GetName() string {
	return c.name
}

func (c *Cluster) GetAddr() string {
	return c.addr
}

func NewCluster(name string, addr string,
	handler ClusterMsgHandler) (c *Cluster, err error) {

	if handler == nil {
		handler = defalutHandler
	}
	c = &Cluster{
		name:     name,
		addr:     addr,
		channels: make(map[string]*Channel),
		conns:    make(map[int]*ClusterConn),
		handler:  handler,
	}
	if err != nil {
		hlog.Errorf("NewCluster fail err:%s\n", err.Error())
	}
	return
}
