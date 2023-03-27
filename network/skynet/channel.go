package skynet

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/common/hlog"
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
	Resp *Resp
	Err  error
	Done chan *Call
}

func (call *Call) done() {
	select {
	case call.Done <- call:
	default:
		log.Panicf("method block")
	}
}

type Channel struct {
	rpc          *Rpc
	readMutex    sync.Mutex // gates read one at a time
	writeMutex   sync.Mutex // gates write one at a time
	rw           io.ReadWriter
	rdbuf        []byte // read buffer
	wrbuf        []byte // write buffer
	session      uint32
	methodMutex  sync.Mutex
	sessionMutex sync.Mutex
	sessions     map[uint32]*Call
	largePkg     map[uint32][]*MsgPart
	onUnknown    OnUnknownPacket
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

	// // hlog.Debugf("WritePacket sz:%d, msg:%v\n", sz, msg)

	copy(c.wrbuf[:], msg)
	_, err := c.rw.Write(c.wrbuf[:sz])
	return err
}

func (c *Channel) readPacket() (msg *[]byte, sz uint16, err error) {
	// c.readMutex.Lock()
	// defer c.readMutex.Unlock()
	// stime := time.Now().UnixMilli()

	szh := c.rdbuf[0:2]
	var n int
	n, err = c.rw.Read(szh)
	if err != nil || n < 2 {
		return
	}
	sz = binary.BigEndian.Uint16(szh)

	// hlog.Debugf(
	// "readPacket head cost:%d n:%d sz:%d\n",
	// time.Now().UnixMilli()-stime, n, sz,
	// )
	to := uint16(0)
	// var to uint16 = 0
	buf := c.rdbuf[2 : sz+2]
	for to < sz-2 {
		var n int
		n, err = c.rw.Read(buf[to:])
		if err != nil {
			// // hlog.Debugf(
			// 	"readPacket err: %s\n", err,
			// )
			return
		}
		to += uint16(n)
		if n == 0 {
			break
		}
	}
	sz = to
	msg = &buf
	return
}

func word2Int(bytes *[]byte, index int) int {
	return int(binary.BigEndian.Uint16((*bytes)[index : index+2]))
}

func getInt16Bytes(bytes *[]byte, index int, x int) *[]byte {
	binary.BigEndian.PutUint16((*bytes)[index:index+2], uint16(x))
	return bytes
}

func (c *Channel) grabLargePkg(session uint32) []*MsgPart {
	if msgs, ok := c.largePkg[session]; ok {
		return msgs
	}
	return nil
}

// dispatch one packet
func (c *Channel) DispatchOnce() (ok bool, err error) {
	msg, sz, err := c.readPacket()
	// hlog.Debugf("DispatchOnce1 %d %v\n", msg, sz)
	if err != nil {
		return
	}

	rpc := c.rpc

	session, data, padding, err := rpc.Dispatch(msg, sz)
	// hlog.Debugf("DispatchOnce2 %d %v %v \n", session, *data, padding)
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
	msg, dsz, err = Concat(msgs)

	if err != nil {
		return
	}

	// hlog.Debugf("DispatchOnce3 %d %v %v\n", session, *msg, dsz)

	respData, err := Unpack(msg, dsz)
	if err != nil {
		hlog.Errorf("DispatchOnce error session:%d %v\n", session, err.Error())
		return
	}

	// hlog.Debugf("DispatchOnce4 session:%d %v\n", session, respData)

	delete(c.largePkg, session)

	call := c.grabSession(session)
	if call == nil {
		if err = c.onUnknown(session, respData); err != nil {
			return
		}
	}
	call.Resp = &Resp{
		Session: session,
		Data:    respData,
	}
	call.done()
	ok = true
	return
}

// dispatch until error
func (c *Channel) Dispatch() (err error) {
	for {
		var ok bool
		ok, err = c.DispatchOnce()

		if ok {
			return
		}
		if err != nil {
			return
		}
	}
}

// unblock call a Channel which has a reply
func (c *Channel) Go(addr interface{}, session uint32, req []interface{}, done chan *Call) (call *Call, err error) {
	// stime := time.Now().UnixMilli()
	rpc := c.rpc

	var msgs []*MsgPart
	if msgs, err = rpc.RequestEncode(addr, session, req); err != nil {
		return
	}
	if done == nil {
		done = make(chan *Call, 1)
	} else {
		if cap(done) == 0 {
			switch addr.(type) {
			case uint32:
				err = fmt.Errorf("call addr:%d session:%d with unbuffered done channel", addr, session)
			case string:
				err = fmt.Errorf("call addr:%s session:%d with unbuffered done channel", addr, session)
			}
			return
		}
	}
	call = &Call{
		Done: done,
	}
	c.setSession(session, call)
	for _, msg := range msgs {
		c.WritePacket(*(msg.Msg), uint16(msg.Sz))
	}
	return
}

// block call a Channel which has a reply
func (c *Channel) Call(addr interface{}, req ...interface{}) ([]interface{}, error) {
	// level := mlog.GetLogLevel()
	call, err := c.Go(addr, c.NextSession(), req, nil)
	if err != nil {
		return nil, err
	}
	go c.Dispatch()
	call = <-call.Done

	return call.Resp.Data.([]interface{}), call.Err
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
	for _, msg := range msgs {
		err = c.WritePacket(*(msg.Msg), uint16(msg.Sz))
		if err != nil {
			return err
		}
	}
	return err
}

func (c *Channel) SetOnUnknownPacket(onUnknown OnUnknownPacket) {
	c.onUnknown = onUnknown
}

func NewChannel(rw io.ReadWriter) (*Channel, error) {
	rpc, err := NewRpc()
	if err != nil {
		return nil, err
	}
	return &Channel{
		rpc:       rpc,
		rw:        rw,
		rdbuf:     make([]byte, MSG_MAX_LEN+2),
		wrbuf:     make([]byte, MSG_MAX_LEN+2),
		sessions:  make(map[uint32]*Call),
		largePkg:  make(map[uint32][]*MsgPart),
		onUnknown: defaultOnUnknownPacket,
		session:   0,
	}, nil
}
