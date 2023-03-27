package skynet

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
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
	idMap        map[int32]int
	nameMap      map[string]int
	methodMap    map[string]int
	sessionMutex sync.Mutex
	sessions     map[int32]int
}

func (rpc *Rpc) Dispatch(msg *[]byte, sz uint16) (session uint32, data *MsgPart, padding bool, err error) {
	var ok bool
	session, ok, data, padding, err = UnpcakResponse(msg, uint32(sz))
	if err != nil {
		return
	}
	if !ok {
		err = fmt.Errorf("call error session:%d error:%s", session, string(*data.Msg))
		return
	}

	return
}

// func (rpc *Rpc) ResponseEncode(session uint32, response interface{}) (data []byte, err error) {
// 	// TODO ResponseEncode
// 	return
// }

// session > 0: need response
func (rpc *Rpc) RequestEncode(addr interface{}, session uint32, req []interface{}) (msgs []*MsgPart, err error) {
	// hlog.Debugf("RequestEncode0 %d %v\n", session, addr)
	msg, sz, err := Pack(req)
	// hlog.Debugf("RequestEncode1 %d %v\n", msg, sz)
	if err != nil {
		return
	}

	_, msgs, err = PackRequest(
		addr, session, msg, uint32(sz),
	)
	hlog.Debugf("RequestEncode2 %v\n", msgs)
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
