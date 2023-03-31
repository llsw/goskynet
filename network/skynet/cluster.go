package skynet

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/netpoll"
	"github.com/cloudwego/hertz/pkg/network/standard"
	rawnet "github.com/cloudwego/netpoll"
)

type ClusterMsgHandler func(addr interface{}, session uint32, args ...interface{}) (resps []interface{}, err error)

type ReqLargePkg struct {
	Addr interface{}
	Msgs []*MsgPart
}

type ClusterConn struct {
	lastSession uint32
	reqLargePkg map[uint32]*ReqLargePkg
}

type Cluster struct {
	name        string
	addr        string
	channels    map[string]*Channel
	transporter network.Transporter
	conns       map[network.Conn]*ClusterConn
	handler     ClusterMsgHandler
}

func genOpts(opts ...config.Option) *config.Options {
	return config.NewOptions(opts)
}

func (c *Cluster) grabLargePkg(conn network.Conn, session uint32, addr interface{}) (*ReqLargePkg, *ClusterConn) {
	if ncl, ok := c.conns[conn]; ok {
		if ncl.reqLargePkg == nil {
			ncl.reqLargePkg = make(map[uint32]*ReqLargePkg)
		}
		if msgs, ok := ncl.reqLargePkg[session]; ok {
			c.conns[conn].lastSession = session
			return msgs, ncl
		} else {
			c.conns[conn].reqLargePkg[session] = &ReqLargePkg{
				Addr: addr,
				Msgs: make([]*MsgPart, 0, 1),
			}
			c.conns[conn].lastSession = session
			return c.conns[conn].reqLargePkg[session], c.conns[conn]
		}
	} else {
		c.conns[conn] = &ClusterConn{
			lastSession: 0,
			reqLargePkg: make(map[uint32]*ReqLargePkg),
		}
		c.conns[conn].reqLargePkg[session] = &ReqLargePkg{
			Addr: addr,
			Msgs: make([]*MsgPart, 0, 1),
		}
		c.conns[conn].lastSession = session
		return c.conns[conn].reqLargePkg[session], c.conns[conn]
	}
}

func defalutHandler(addr interface{}, session uint32, args ...interface{}) (resps []interface{}, err error) {
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

	resps = make([]interface{}, 10)
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
	return
}

func (c *Cluster) msg(ctx context.Context, conn network.Conn,
	addr interface{}, session uint32, msgs []*MsgPart) (err error) {
	msg, sz, err := Concat(msgs)
	ok := true
	var msgsz int
	var resps []interface{}
	if err == nil {
		var args []interface{}
		args, err = Unpack(msg, sz)
		if err == nil {
			resps, err = c.handler(addr, session, args...)
		}
	}

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
		hlog.Errorf("pack response msg error:%s resps:%v", err.Error(), resps)
		return
	}
	for _, v := range msgs {
		_, err = conn.WriteBinary(*v.Msg)
		if err != nil {
			return
		}
	}
	// 要flush才会把包发出去
	err = conn.Flush()
	if err != nil {
		return
	}
	return
}

func (c *Cluster) socket(ctx context.Context, conn network.Conn) (err error) {
	hlog.Debug("socket start")
	var (
		szh     []byte
		buf     []byte
		addr    interface{}
		session uint32
		data    *MsgPart
		padding bool
		skip    int
		reqNum  int
	)

	defer func() {
		if err != nil {
			cconn := c.conns[conn]
			if cconn != nil && cconn.reqLargePkg != nil {
				delete(cconn.reqLargePkg, c.conns[conn].lastSession)
			}
		}
		conn.Release()
	}()

	reqNum = 0

	for {
		skip = 2
		conn.SetReadTimeout(5 * time.Second)
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
			if reqNum > 10 {
				conn.Skip(rl)
				err = fmt.Errorf(
					"pack length error need:%d actual:%d", skip, rl,
				)
				return
			}
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
			conn.Skip(rl)
			return
		}

		reqLargePkg, cconn := c.grabLargePkg(conn, session, addr)
		cconn.reqLargePkg[session].Msgs = append(reqLargePkg.Msgs, data)

		if padding {
			if rl <= isz {
				return
			}
			continue
		}
		pkg := cconn.reqLargePkg[session]
		addr = pkg.Addr
		msgs := pkg.Msgs
		delete(cconn.reqLargePkg, session)
		go c.msg(ctx, conn, addr, session, msgs)

		if rl <= isz {
			return
		}
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
			err = c.socket(ctx, v)
		case *standard.Conn:
			for {
				err = c.socket(ctx, v)
				if err != nil {
					hlog.Errorf("on data err:%s\n", err.Error())
				}
			}
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

func (c *Cluster) newOpts() *config.Options {
	// server.WithOnConnect 可以注册Connect回调
	// server.WithOnAccept 可以注册Accept回调
	return genOpts(
		server.WithHostPorts(c.addr),
		server.WithKeepAlive(true),
		server.WithOnConnect(func(ctx context.Context, conn network.Conn) context.Context {
			rawConn, rwaConnection := getRawConn(conn)
			if rwaConnection != nil {
				rwaConnection.AddCloseCallback(func(connection rawnet.Connection) error {
					c.conns[conn] = nil
					rawConn, _ := getRawConn(conn)
					if rawConn != nil {
						hlog.Debugf("on close fd:%d", rawConn.Fd())
					}

					return nil
				})
			}
			if rawConn != nil {
				hlog.Debugf("on connect fd:%d", rawConn.Fd())
			}
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

func (c *Cluster) getChannel(node string) (channel *Channel, err error) {
	if ch, ok := c.channels[node]; ok {
		channel = ch
		return
	} else {
		var conn net.Conn
		conn, err = net.Dial("tcp", node)
		if err != nil {
			hlog.Errorf(
				"node getChannel fail, node:%s, error:%s\n",
				node, err.Error(),
			)
		}
		c.channels[node], err = NewChannel(conn)
		if err != nil {
			hlog.Errorf(
				"node getChannel fail, node:%s, error:%s\n",
				node, err.Error(),
			)
			return
		}
		channel = c.channels[node]
	}
	return
}

func (c *Cluster) Call(node string, addr interface{}, req ...interface{}) (resp []interface{}, err error) {
	channel, err := c.getChannel(node)
	if err != nil {
		return
	}
	hlog.Debugf("addr:%v req:%v", addr, req)
	return channel.Call(addr, req...)
}

// send没有回复
func (c *Cluster) Send(node string, addr interface{}, req ...interface{}) (err error) {
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

func NewCluster(name string, addr string, handler ClusterMsgHandler) (c *Cluster, err error) {

	if handler == nil {
		handler = defalutHandler
	}
	c = &Cluster{
		name:     name,
		addr:     addr,
		channels: make(map[string]*Channel),
		conns:    make(map[network.Conn]*ClusterConn),
		handler:  handler,
	}
	if err != nil {
		hlog.Errorf("NewCluster fail err:%s\n", err.Error())
	}
	return
}
