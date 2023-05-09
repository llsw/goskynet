package skynet

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	share "github.com/llsw/goskynet/lib/share"
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
	padding int
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
	addr interface{}, session uint32, msgs ...*MsgPart) {
	defer share.Recover(func(err error) {})
	msg, sz, err := Concat(msgs)
	ok := true
	var msgsz int
	var resps []interface{}

	cb := func(resps []interface{}, err error) {
		defer share.Recover(func(err error) {})
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

func (c *Cluster) grabLargePkg(gc *GateConn, conn network.Conn,
	session uint32, addr interface{}) *ReqLargePkg {
	if gc == nil {
		return nil
	}

	if gc.ReqLargePkg == nil {
		return nil
	}

	if v, ok := gc.ReqLargePkg.Load(session); ok {
		gc.LastSession = session
		return v.(*ReqLargePkg)
	} else {
		// TODO 这里的锁还可以根据session进行更细粒度的划分
		// 但是大请求的情况比较少见，不要过度优化，如果性能瓶颈在这个再优化
		gc.ReqLargeLock.Lock()
		defer gc.ReqLargeLock.Unlock()
		if v, ok := gc.ReqLargePkg.Load(session); ok {
			gc.LastSession = session
			return v.(*ReqLargePkg)
		} else {
			// TODO 可以用对象池
			// 同样，如果是大请求比较多，这里可以优化成对象池，避免大量的申请对象
			pkg := &ReqLargePkg{
				Addr: addr,
				Msgs: make([]*MsgPart, 0, 1),
			}
			gc.ReqLargePkg.Store(session, pkg)
			gc.LastSession = session
			return pkg
		}
	}
}

func (c *Cluster) OnConnect(gc *GateConn) {
	gc.ReqLargePkg = new(sync.Map)
}

func (c *Cluster) OnAccept(conn net.Conn) {
}

func (c *Cluster) OnClose(gc *GateConn) {
}

func (c *Cluster) OnMsg(gateConn *GateConn, req *Req) {
	data := req.data.(*clusterData)
	if data.padding == 0 {
		c.msg(gateConn, req.ctx, req.conn, data.addr, data.session, data.data)
	} else {
		reqLargePkg := c.grabLargePkg(
			gateConn, req.conn, data.session, data.addr)
		if reqLargePkg == nil {
			return
		}
		if v, ok := gateConn.ReqLargePkg.Load(data.session); ok {
			pkg := v.(*ReqLargePkg)
			pkg.Msgs = append(reqLargePkg.Msgs, data.data)
			if data.padding == 1 {
				return
			}
			addr := pkg.Addr
			msgs := pkg.Msgs
			gateConn.ReqLargePkg.Delete(data.session)
			c.msg(gateConn, req.ctx, req.conn, addr, data.session, msgs...)
		}
	}

}

func (c *Cluster) OnUnpack(gc *GateConn, msg []byte, sz int) (cd interface{}, err error) {
	addr, session, data, padding, err := UnpcakRequest(&msg, uint32(sz))
	if err != nil {
		return
	}

	if padding == 0 {
		data.Id = 0
	} else {
		data.Id++
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
	if c.gate == nil {
		return fmt.Errorf("cluster:%s addr:%s gate is nil", c.name, c.addr)
	}
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
			ctx := context.Background()
			c.channels[addr], err = NewChannel(addr, ctx, func() {
				if _, ok := c.channels[addr]; !ok {
					return
				}
				c.clock.Lock()
				defer c.clock.Unlock()
				if _, ok := c.channels[addr]; !ok {
					return
				} else {
					hlog.Errorf(
						"node channel finish node:%s",
						addr,
					)
					delete(c.channels, addr)
				}
			})
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
	var gate *Gate

	c = &Cluster{
		name:     name,
		addr:     addr,
		channels: make(map[string]*Channel),
		handler:  handler,
	}

	if addr != "" {
		gate = NewGate(addr)

		gate.SetOnConnect(func(conn *GateConn) {
			c.OnConnect(conn)
		})
		gate.SetOnAccept(func(conn net.Conn) {
			c.OnAccept(conn)
		})
		gate.SetOnClose(func(conn *GateConn) {
			c.OnClose(conn)
		})
		gate.SetOnUnpack(
			func(conn *GateConn,
				msg []byte, sz int) (data interface{}, err error) {
				return c.OnUnpack(conn, msg, sz)
			},
		)
		gate.SetOnMsg(func(conn *GateConn, req *Req) {
			c.OnMsg(conn, req)
		})
		c.gate = gate
	}

	return
}
