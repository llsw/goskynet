package skynet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/netpoll"
	share "github.com/llsw/goskynet/lib/share"
)

const (
	MSG_MAX_LEN = 0xffff
)

type Resp struct {
	Session uint32
	Data    interface{}
}

type OnUnknownPacket func(session uint32, data interface{}) error

func defaultOnUnknownPacket(session uint32, data interface{}) error {
	return fmt.Errorf("unknown packet, session:%d data:%v", session, data)
}

type Call struct {
	Resp             *Resp
	Err              error
	DoneChan         chan *Call
	ResponseCallback ResponseCallFun
}

type ResponseCallFun = func(c *Call)

type CallbackFun = func(interface{}, error)

func (call *Call) done() {
	if call.ResponseCallback != nil {
		// hlog.Debugf("dddddddddd")
		call.ResponseCallback(call)
	} else {
		// hlog.Debugf("ffffffffff")
		select {
		case call.DoneChan <- call:
		default:
			log.Panicf("method block")
		}
	}
}

type Channel struct {
	name         string
	addr         string
	rpc          *Rpc
	readMutex    sync.Mutex // gates read one at a time
	writeMutex   sync.Mutex // gates write one at a time
	rw           netpoll.Connection
	rdbuf        []byte // read buffer
	wrbuf        []byte // write buffer
	session      uint32
	sessionMutex sync.Mutex
	sessions     map[uint32]*Call
	largePkg     map[uint32][]*MsgPart
	onUnknown    OnUnknownPacket
	reqChan      chan []*MsgPart
}

func (c *Channel) NextSession() uint32 {
	return atomic.AddUint32(&(c.session), 1)
}

func (c *Channel) setSession(session uint32, call *Call) {
	c.sessionMutex.Lock()
	c.sessions[session] = call
	c.sessionMutex.Unlock()
}

func (c *Channel) grabSession(session uint32) *Call {
	c.sessionMutex.Lock()
	defer c.sessionMutex.Unlock()
	if call, ok := c.sessions[session]; ok {
		delete(c.sessions, session)
		return call
	}
	return nil
}

func (c *Channel) WritePacket(msg []byte, sz uint16) error {
	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()
	if sz > MSG_MAX_LEN {
		return fmt.Errorf(
			"message size(%d) should be less than %d",
			sz, MSG_MAX_LEN,
		)
	}

	writer := c.rw.Writer()
	_, err := writer.WriteBinary(msg)
	writer.Flush()
	return err
}

func (c *Channel) readPacket() (reader netpoll.Reader, sz int, err error) {
	c.readMutex.Lock()
	defer c.readMutex.Unlock()
	// stime := time.Now().UnixMilli()
	conn := c.rw.Reader()
	szh, err := conn.Peek(2)
	if err != nil {
		return
	}
	conn.Skip(2)
	sz = int(binary.BigEndian.Uint16(szh))
	reader, err = conn.Slice(sz)
	if err != nil {
		return
	}
	return
}

// func word2Int(bytes *[]byte, index int) int {
// 	return int(binary.BigEndian.Uint16((*bytes)[index : index+2]))
// }

// func getInt16Bytes(bytes *[]byte, index int, x int) *[]byte {
// 	binary.BigEndian.PutUint16((*bytes)[index:index+2], uint16(x))
// 	return bytes
// }

func (c *Channel) grabLargePkg(session uint32) []*MsgPart {
	if msgs, ok := c.largePkg[session]; ok {
		return msgs
	}
	return nil
}

// dispatch one packet
func (c *Channel) DispatchResOnce() (ok bool, err error) {
	reader, sz, err := c.readPacket()
	if err != nil {
		return
	}

	rpc := c.rpc
	session, ok, data, padding, err := rpc.Dispatch(reader, sz)
	if err != nil {
		if session != 0 {
			delete(c.largePkg, session)
		}
		return
	}

	msgs := c.grabLargePkg(session)
	if len(msgs) != 0 {
		c.largePkg[session] = append(msgs, data)
	} else {
		c.largePkg[session] = make([]*MsgPart, 0, 1)
		c.largePkg[session] = append(msgs, data)
	}

	if padding {
		return
	}

	msgs = c.largePkg[session]
	var dsz uint32
	var msg *[]byte
	msg, dsz, err = Concat(msgs)

	if err != nil {
		return
	}

	respData, err := Unpack(msg, dsz)
	if err != nil {
		hlog.Errorf(
			"DispatchOnce error session:%d %v\n", session, err.Error())
		return
	}

	delete(c.largePkg, session)

	call := c.grabSession(session)
	if call == nil {
		if err = c.onUnknown(session, respData); err != nil {
			return
		}
	}
	if ok {
		call.Resp = &Resp{
			Session: session,
			Data:    respData,
		}
	} else {
		call.Err = fmt.Errorf(respData[0].(string))
	}

	call.done()
	ok = true
	return
}

// dispatch until error
func (c *Channel) DispatchRes(ctx context.Context, closeChannle func()) {
	_, cancelFunction := context.WithCancel(ctx)
	defer func() {
		cancelFunction()
		closeChannle()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, err := c.DispatchResOnce()
		if err != nil {
			hlog.Debugf("dispatch error:%s", err.Error())
			if errors.Is(err, io.EOF) || err.Error() == io.EOF.Error() {
				return
			}
		}
	}
}

func (c *Channel) DispatchReqOnce(msgs []*MsgPart) (err error) {
	for _, msg := range msgs {
		defer share.Recover(func(e error) {
			hlog.Errorf("DispatchReqOnce error:%s", e.Error())
			err = e
		})
		err = c.WritePacket(*(msg.Msg), uint16(msg.Sz))
		if err != nil {
			return
		}
	}
	return
}

// dispatch until error
func (c *Channel) DispatchReq(ctx context.Context, closeChannle func()) {
	for {
		select {
		case <-ctx.Done():
			close(c.reqChan)
			return
		case req := <-c.reqChan:
			err := c.DispatchReqOnce(req)
			if err != nil {
				hlog.Debugf("dispatch error:%s", err.Error())
			}
		}
	}
}

func (c *Channel) testMultiPkg(msgs []*MsgPart) {
	msg := []byte{0, 26, 128, 4, 105, 107, 117, 110, 1, 0, 0, 0, 36, 73,
		107, 117, 110, 44, 104, 101, 108, 108, 111, 36, 105, 107, 117,
		110, 0, 26, 128, 4, 105, 107, 117, 110, 2, 0, 0, 0, 36, 73,
		107, 117, 110, 44, 104, 101, 108, 108, 111, 36, 105,
		107, 117, 110, 0, 26, 128, 4, 105, 107, 117, 110,
		3, 0, 0, 0, 36, 73, 107, 117, 110, 44, 104, 101,
		108, 108, 111, 36, 105, 107, 117, 110, 0, 26, 128,
		4, 105, 107, 117, 110, 4, 0, 0, 0, 36, 73, 107, 117,
		110, 44, 104, 101, 108, 108, 111, 36, 105, 107, 117,
		110, 0, 26, 128, 4, 105, 107, 117, 110, 5, 0, 0, 0, 36,
		73, 107, 117, 110, 44, 104, 101, 108, 108, 111, 36, 105,
		107, 117, 110, 0, 26, 128, 4, 105, 107, 117, 110, 6, 0,
		0, 0, 36, 73, 107, 117, 110, 44, 104, 101, 108, 108, 111,
		36, 105, 107, 117, 110, 0, 26, 128, 4, 105, 107, 117, 110,
		7, 0, 0, 0, 36, 73, 107, 117, 110, 44, 104, 101, 108, 108,
		111, 36, 105, 107, 117, 110, 0, 26, 128, 4, 105, 107, 117,
		110, 8, 0, 0, 0, 36, 73, 107, 117, 110, 44, 104, 101, 108,
		108, 111, 36, 105, 107, 117, 110,
		// wrong data
		00, 99, 1,
	}
	msgs = make([]*MsgPart, 1)
	msgs[0] = &MsgPart{
		Msg: &msg,
		Sz:  uint32(len(msg)),
	}
	c.reqChan <- msgs
}

func errDoneChan(addr interface{}, session uint32, done chan *Call) (err error) {
	if cap(done) == 0 {
		switch addr.(type) {
		case uint32:
			err = fmt.Errorf(
				"call addr:%d session:%d with unbuffered done channel",
				addr, session)
		case string:
			err = fmt.Errorf(
				"call addr:%s session:%d with unbuffered done channel",
				addr, session)
		}
	}
	return
}

// unblock call a Channel which has a reply
func (c *Channel) Go(addr interface{}, session uint32,
	req []interface{}, cb *Call, done chan *Call) (call *Call, err error) {
	rpc := c.rpc

	var msgs []*MsgPart
	if msgs, err = rpc.RequestEncode(addr, session, req); err != nil {
		return
	}
	if cb == nil && done == nil {
		done = make(chan *Call, 1)
	} else {
		if cb == nil && done != nil {
			err = errDoneChan(addr, session, done)
			if err != nil {
				return
			}
		}
	}
	if cb == nil {
		cb = &Call{
			DoneChan: done,
		}
	}
	call = cb
	c.setSession(session, call)
	c.reqChan <- msgs
	// c.testMultiPkg(msgs)
	return
}

// block call a Channel which has a reply
func (c *Channel) Call(addr interface{}, req ...interface{}) ([]interface{}, error) {
	// level := mlog.GetLogLevel()
	call, err := c.Go(addr, c.NextSession(), req, nil, nil)
	if err != nil {
		return nil, err
	}

	call = <-call.DoneChan
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Resp.Data.([]interface{}), nil
}

func (c *Channel) CallNoBlock(addr interface{}, req ...interface{}) {
	// level := mlog.GetLogLevel()
	index := len(req) - 1
	f := req[index].(CallbackFun)
	call := &Call{
		ResponseCallback: func(rc *Call) {
			if rc.Err != nil {
				f(nil, rc.Err)
				return
			}
			f(rc.Resp.Data.([]interface{}), nil)
		},
	}
	_, err := c.Go(addr, c.NextSession(), req[0:index], call, nil)
	if err != nil {
		f(nil, err)
		return
	}
}

// encode notify packet
// func (c *Channel) Encode(name string, req interface{}) ([]byte, error) {
// 	rpc := c.rpc
// 	return rpc.RequestEncode(name, 0, req)
// }

// invoke a Channel which has not a reply
func (c *Channel) Invoke(addr interface{}, req []interface{}) error {
	rpc := c.rpc
	msgs, err := rpc.RequestEncode(addr, c.NextSession(), req)
	if err != nil {
		return err
	}
	c.reqChan <- msgs
	return err
}

func (c *Channel) SetOnUnknownPacket(onUnknown OnUnknownPacket) {
	c.onUnknown = onUnknown
}

func NewChannel(name string, addr string, ctx context.Context, closeChannle func()) (ch *Channel, err error) {
	dialer := netpoll.NewDialer()
	var conn netpoll.Connection
	conn, err = dialer.DialConnection(
		"tcp", addr, time.Duration(5*time.Second))
	if err != nil {
		panic("dial netpoll connection failed")
	}

	if err != nil {
		hlog.Errorf(
			"node new channel fail, addr:%s, error:%s\n",
			addr, err.Error(),
		)
	}

	rpc, err := NewRpc()
	if err != nil {
		return nil, err
	}

	ch = &Channel{
		name:     name,
		addr:     addr,
		rpc:      rpc,
		rw:       conn,
		rdbuf:    make([]byte, MSG_MAX_LEN+2),
		wrbuf:    make([]byte, MSG_MAX_LEN+2),
		sessions: make(map[uint32]*Call),
		largePkg: make(map[uint32][]*MsgPart),
		onUnknown: func(session uint32, data interface{}) error {
			return fmt.Errorf("channle:%s addr:%s unknown packet, session:%d data:%v", name, addr, session, data)
		},
		session: 0,
		reqChan: make(chan []*MsgPart, 500),
	}

	go ch.DispatchRes(ctx, closeChannle)
	go ch.DispatchReq(ctx, closeChannle)

	return
}
