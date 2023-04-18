package skynet

import (
	"errors"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/netpoll"
	share "github.com/llsw/goskynet/lib/share"
)

var (
	ErrRepeatedRpc     = errors.New("sproto rpc: repeated rpc")
	ErrUnknownProtocol = errors.New("sproto rpc: unknown protocol")
	ErrUnknownSession  = errors.New("sproto rpc: unknown session")
)

type rpcHeader struct {
	Type    *int32 `sproto:"integer,0,name=type"`
	Session *int32 `sproto:"integer,1,name=session"`
}

type Rpc struct {
	idMap     map[int32]int
	nameMap   map[string]int
	methodMap map[string]int
	sessions  map[int32]int
}

func (rpc *Rpc) Dispatch(reader netpoll.Reader, sz int) (session uint32, ok bool, data *MsgPart, padding bool, err error) {
	var msg []byte
	msg, err = reader.Next(sz)
	if err != nil {
		return
	}

	defer share.Recover(func(e error) {
		hlog.Errorf("Dispatch error:%s", e.Error())
		reader.Release()
		err = e
	})
	session, ok, data, padding, err = UnpcakResponse(&msg, uint32(sz))
	reader.Release()
	if err != nil {
		hlog.Errorf("dispatch error:%s", err.Error())
		return
	}
	if !ok {
		ok = false
		return
	}
	ok = true
	return
}

// session > 0: need response
func (rpc *Rpc) RequestEncode(addr interface{},
	session uint32, req []interface{}) (msgs []*MsgPart, err error) {
	msg, sz, err := Pack(req)
	if err != nil {
		return
	}

	_, msgs, err = PackRequest(
		addr, session, msg, uint32(sz),
	)
	if err != nil {
		return
	}
	return
}

func NewRpc() (*Rpc, error) {
	idMap := make(map[int32]int)
	nameMap := make(map[string]int)
	methodMap := make(map[string]int)
	rpc := &Rpc{
		idMap:     idMap,
		nameMap:   nameMap,
		methodMap: methodMap,
		sessions:  make(map[int32]int),
	}
	return rpc, nil
}
