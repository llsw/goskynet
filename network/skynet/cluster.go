package skynet

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/netpoll"
	"github.com/cloudwego/hertz/pkg/network/standard"
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

func (c *Cluster) msg(ctx context.Context, conn network.Conn, addr interface{}, session uint32, msg *[]byte, sz uint32) (err error) {
	var args []interface{}
	args, err = Unpack(msg, sz)
	if err != nil {
		return
	}
	ok := true
	var msgsz int
	resps, err := c.handler(addr, session, args...)
	if err != nil {
		ok = false
		resps = []interface{}{err.Error()}
	}

	msg, msgsz, err = Pack(resps)
	if err != nil {
		hlog.Errorf("pack msg error:%s resps:%v", err.Error(), resps)
		return
	}

	msgs, err := PackResponse(session, ok, msg, uint32(msgsz))

	if err != nil {
		return
	}
	for _, v := range msgs {
		// hlog.Debugf("netpack.PackResponse %v ", *v.Msg)
		_, err = conn.WriteBinary(*v.Msg)

		if err != nil {
			// // hlog.Debugf("conn.WriteBinary error:%v ", err)
			return
		}
	}
	// 要flush才会把包发出去
	err = conn.Flush()
	if err != nil {
		// // hlog.Debugf("conn.Flush  error:%v ", err)
		return
	}
	return
}

func (c *Cluster) socket(ctx context.Context, conn network.Conn) (err error) {
	szh, err := conn.ReadBinary(2)
	if err != nil {
		return
	}
	sz := binary.BigEndian.Uint16(szh)
	buf, err := conn.ReadBinary(int(sz))
	if err != nil {
		return
	}
	addr, session, data, padding, err := UnpcakRequest(&buf, uint32(sz))
	cconn := c.conns[conn]
	if err != nil {
		if cconn != nil {
			delete(cconn.reqLargePkg, c.conns[conn].lastSession)
		}
		return
	}

	reqLargePkg, cconn := c.grabLargePkg(conn, session, addr)
	cconn.reqLargePkg[session].Msgs = append(reqLargePkg.Msgs, data)

	if padding {
		return
	}
	pkg := cconn.reqLargePkg[session]
	addr = pkg.Addr
	msgs := pkg.Msgs
	var dsz uint32
	// // hlog.Debugf("socket sz:%d", sz)
	msg, dsz, err := Concat(msgs)
	if err != nil {
		delete(cconn.reqLargePkg, session)
		return
	}
	delete(cconn.reqLargePkg, session)
	return c.msg(ctx, conn, addr, session, msg, dsz)
}

func (c *Cluster) socketStream(ctx context.Context, conn network.StreamConn) (err error) {
	// TODO 如果需要steam的话
	return
}

func (c *Cluster) onData(ctx context.Context, conn interface{}) (err error) {
	switch conn := conn.(type) {
	case network.Conn:
		err = c.socket(ctx, conn)
	case network.StreamConn:
		err = c.socketStream(ctx, conn)
	}

	if err != nil {
		hlog.Errorf("on data err:%s\n", err.Error())
	}
	return
}

func (c *Cluster) newOpts() *config.Options {
	// server.WithOnConnect 可以注册Connect回调
	// server.WithOnAccept 可以注册Accept回调
	return genOpts(server.WithHostPorts(c.addr))
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
		fallthrough
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
