package skynet

import (
	"context"
	"net"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	utils "github.com/llsw/goskynet/lib/utils"
)

type ClusterMsgHandler func(addr interface{}, session uint32, args ...interface{})

type ReqLargePkg struct {
	Addr interface{}
	Msgs []*MsgPart
}

type clusterData struct {
	addr    interface{}
	session uint32
	data    *MsgPart
	padding bool
}

type Cluster struct {
	name     string
	addr     string
	channels map[string]*Channel
	gate     *Gate
	handler  ClusterMsgHandler
	clock    sync.Mutex
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

func (c *Cluster) msg(cc *GateConn, ctx context.Context, conn network.Conn,
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

		err = c.gate.Response(cc, conn, msgs)
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

func (c *Cluster) grabLargePkg(gateConn *GateConn, conn network.Conn,
	session uint32, addr interface{}) *ReqLargePkg {
	if gateConn == nil {
		return nil
	}

	if gateConn.reqLargePkg == nil {
		return nil
	}

	if v, ok := gateConn.reqLargePkg.Load(session); ok {
		gateConn.lastSession = session
		return v.(*ReqLargePkg)
	} else {
		// TODO 可以用对象池
		pkg := &ReqLargePkg{
			Addr: addr,
			Msgs: make([]*MsgPart, 0, 1),
		}
		// TODO 这里是否线程安全，包的顺序怎么办
		gateConn.reqLargePkg.Store(session, pkg)
		gateConn.lastSession = session
		return pkg
	}
}

func (c *Cluster) OnConnect(fd int, conn network.Conn) {
}

func (c *Cluster) OnAccept(conn net.Conn) {
}

func (c *Cluster) OnClose(fd int, conn network.Conn) {
}

func (c *Cluster) OnMsg(req *Req) {
	data := req.data.(*clusterData)
	gateConn := req.gateConn
	// TODO 应该移到gate.socket去，防止大包顺序不一样
	reqLargePkg := c.grabLargePkg(gateConn, req.conn, data.session, data.addr)
	if reqLargePkg == nil {
		return
	}

	if v, ok := gateConn.reqLargePkg.Load(data.session); ok {
		pkg := v.(*ReqLargePkg)
		pkg.Msgs = append(reqLargePkg.Msgs, data.data)
		if data.padding {
			return
		}
		addr := pkg.Addr
		msgs := pkg.Msgs
		gateConn.reqLargePkg.Delete(data.session)
		c.msg(gateConn, req.ctx, req.conn, addr, data.session, msgs)
	}

}

func (c *Cluster) OnUnpack(msg []byte, sz int) (cd interface{}, err error) {
	addr, session, data, padding, err := UnpcakRequest(&msg, uint32(sz))
	if err != nil {
		return
	}
	cd = &clusterData{
		addr,
		session,
		data,
		padding,
	}
	return
}

func (c *Cluster) Open() (err error) {
	return c.gate.ListenAndServe()
}

func (c *Cluster) ListenAndServe() (err error) {
	return c.gate.ListenAndServe()
}

func (c *Cluster) getChannel(addr string) (channel *Channel, err error) {
	if ch, ok := c.channels[addr]; ok {
		channel = ch
		return
	} else {
		c.clock.Lock()
		defer c.clock.Unlock()
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
	gate := NewGate(addr)
	c = &Cluster{
		name:     name,
		addr:     addr,
		channels: make(map[string]*Channel),
		handler:  handler,
		gate:     gate,
	}
	// gate.SetOnAccept(c.OnAccept)
	// gate.SetOnConnect(c.OnConnect)
	// gate.SetOnClose(c.OnClose)
	gate.SetOnMsg(c.OnMsg)
	gate.SetOnUnpack(c.OnUnpack)
	if err != nil {
		hlog.Errorf("NewCluster fail err:%s\n", err.Error())
	}
	return
}
