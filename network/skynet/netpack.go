package skynet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	share "github.com/llsw/goskynet/lib/share"
)

type MsgPart struct {
	Msg *[]byte
	Sz  uint32
	Id  uint8
}

const (
	TEMP_LENGTH = 0x8200
	MULTI_PART  = 0x8000

	TYPE_NIL     = 0
	TYPE_BOOLEAN = 1
	// hibits 0 false 1 true
	TYPE_NUMBER = 2
	// hibits 0 : 0 , 1: byte, 2:word, 4: dword, 6: qword, 8 : double
	TYPE_NUMBER_ZERO  = 0
	TYPE_NUMBER_BYTE  = 1
	TYPE_NUMBER_WORD  = 2
	TYPE_NUMBER_DWORD = 4
	TYPE_NUMBER_QWORD = 6
	TYPE_NUMBER_REAL  = 8

	TYPE_USERDATA     = 3
	TYPE_SHORT_STRING = 4
	// hibits 0~31 : len
	TYPE_LONG_STRING = 5
	TYPE_TABLE       = 6
	MAX_COOKIE       = 32
	MAX_DEPTH        = 32
)

func assert(t bool, msg string) {
	if !t {
		panic(msg)
	}
}

func fillUint32(buf *[]byte, n uint32) {
	(*buf)[0] = byte(n & 0xff)
	(*buf)[1] = byte((n >> 8) & 0xff)
	(*buf)[2] = byte((n >> 16) & 0xff)
	(*buf)[3] = byte((n >> 24) & 0xff)
}

func unpackUint32(buf *[]byte) uint32 {
	if buf == nil || len(*buf) < 4 {
		return 0
	}
	return uint32((*buf)[0]) | uint32((*buf)[1])<<8 | uint32((*buf)[2])<<16 | uint32((*buf)[3])<<24
}

func fillHeader(buf *[]byte, sz int) {
	assert(sz < 0x10000, "fillHeader error")
	(*buf)[0] = byte((sz >> 8) & 0xff)
	(*buf)[1] = byte(sz & 0xff)
}

func packReqNumber(addr uint32, session uint32, msg *[]byte, sz uint32) (request *[]byte, reqsz uint32, multipak int, err error) {
	if sz < MULTI_PART {
		reqsz = sz + 11
		// TODO 使用对象池，不要每次都申请内存，gc压力大
		buf := make([]byte, reqsz)
		fillHeader(&buf, int(sz)+9)
		buf[2] = 0
		bufa := buf[3:7]
		fillUint32(&bufa, addr)
		bufs := buf[7:11]
		fillUint32(&bufs, session)
		copy(buf[11:reqsz], *msg)
		request = &buf
		multipak = 0
	} else {
		reqsz = 15
		multipak = int(sz - uint32(1)/MULTI_PART + uint32(1))
		buf := make([]byte, 15)
		fillHeader(&buf, 13)
		buf[2] = 1
		bufa := buf[3:7]
		fillUint32(&bufa, addr)
		bufs := buf[7:11]
		fillUint32(&bufs, session)
		bufsz := buf[11:reqsz]
		fillUint32(&bufsz, sz)
		request = &buf
	}
	return
}

func packReqString(addr string, session uint32, msg *[]byte, sz uint32) (request *[]byte, reqsz uint32, multipak int, err error) {
	namelen := len(addr)
	namebytes := []byte(addr)
	if sz < MULTI_PART {
		reqsz = sz + uint32(8) + uint32(namelen)
		buf := make([]byte, reqsz)
		fillHeader(&buf, int(sz)+6+namelen)
		buf[2] = 0x80
		buf[3] = uint8(namelen)
		copy(buf[4:4+namelen], namebytes)
		bufs := buf[4+namelen : 8+namelen]
		fillUint32(&bufs, session)
		copy(buf[8+namelen:reqsz], *msg)
		request = &buf
		multipak = 0
	} else {
		reqsz = uint32(12 + namelen)
		multipak = int(sz - uint32(1)/MULTI_PART + uint32(1))
		buf := make([]byte, reqsz)
		fillHeader(&buf, 10+namelen)
		buf[2] = 0x81
		buf[3] = uint8(namelen)
		copy(buf[4:4+namelen], namebytes)
		bufs := buf[4+namelen : 8+namelen]
		fillUint32(&bufs, session)
		bufsz := buf[8+namelen : reqsz]
		fillUint32(&bufsz, sz)
		request = &buf
	}
	return
}

func packReqMulti(multipak int, session uint32, msg *[]byte, sz uint32, msgs []*MsgPart) (padding []*MsgPart, err error) {
	if multipak <= 0 {
		padding = msgs
		return
	}

	part := (sz-1)/MULTI_PART + 1
	index := uint32(0)
	for i := 0; i < int(part); i++ {
		var reqsz uint32
		var buf []byte
		var s uint32
		if sz > MULTI_PART {
			s = MULTI_PART
			reqsz = s + 7
			buf = make([]byte, reqsz)
			buf[2] = 2
		} else {
			s = sz
			reqsz = s + 7
			buf = make([]byte, reqsz)
			buf[2] = 3
		}

		fillHeader(&buf, int(s)+5)
		bufs := buf[3:7]
		fillUint32(&bufs, session)
		m := (*msg)[index : index+s]
		copy(buf[7:reqsz], m)
		index = index + s
		sz = sz - s
		item := &MsgPart{
			Msg: &buf,
			Sz:  reqsz,
		}
		msgs = append(msgs, item)
	}
	padding = msgs
	return
}

func PackRequest(addr interface{}, session uint32,
	msg *[]byte, sz uint32) (nextSession uint32, msgs []*MsgPart, err error) {
	nextSession = session
	if msg == nil {
		err = fmt.Errorf("packrequest Invalid request message")
		return
	}
	if session <= 0 {
		err = fmt.Errorf("packrequest Invalid request session %d", session)
		return
	}
	var (
		request  *[]byte
		reqsz    uint32
		multipak int
	)
	switch v := addr.(type) {
	case uint32:
		request, reqsz, multipak, err = packReqNumber(v, session, msg, sz)
	case string:
		request, reqsz, multipak, err = packReqString(v, session, msg, sz)
	default:
		err = fmt.Errorf("packrequest Invalid addr %v", addr)
		return
	}
	if err != nil {
		return
	}
	msgs = make([]*MsgPart, 1)
	msgs[0] = &MsgPart{
		Msg: request,
		Sz:  reqsz,
	}

	msgs, err = packReqMulti(multipak, session, msg, sz, msgs)
	if err != nil {
		return
	}
	nextSession = session + 1
	if nextSession <= 0 {
		nextSession = 1
	}
	return
}

func unpackReqNumber(msg *[]byte, sz uint32) (addr interface{},
	session uint32, data *MsgPart, padding int, err error) {
	if msg == nil || len(*msg) < int(sz) {
		err = fmt.Errorf("unpcak req number invalid msg %v need:%d", msg, sz)
		return
	}
	padding = 0
	bufa := (*msg)[1:5]
	addr = unpackUint32(&bufa)
	bufs := (*msg)[5:9]
	session = unpackUint32(&bufs)
	reqsz := sz - 9
	dm := make([]byte, reqsz)
	copy(dm, (*msg)[9:sz])
	data = &MsgPart{
		Msg: &dm,
		Sz:  reqsz,
	}
	return
}

func unpackMReqNumber(msg *[]byte, sz uint32) (addr interface{},
	session uint32, data *MsgPart, padding int, err error) {
	if msg == nil || len(*msg) < 13 || sz < 13 {
		err = fmt.Errorf("unpcak multi req number invalid msg %v need:%d", msg, 13)
		return
	}
	padding = 1
	bufa := (*msg)[1:5]
	addr = unpackUint32(&bufa)
	bufs := (*msg)[5:9]
	session = unpackUint32(&bufs)
	bufsz := (*msg)[9:13]
	reqsz := unpackUint32(&bufsz)
	data = &MsgPart{
		Sz: reqsz,
	}
	return
}

func unpackMReqPart(msg *[]byte, sz uint32) (addr interface{},
	session uint32, data *MsgPart, padding int, err error) {
	if msg == nil || len(*msg) < 5 || sz < 5 {
		err = fmt.Errorf("unpcak multi req part invalid msg %v need:%d", msg, 5)
		return
	}
	if (*msg)[0] == 2 {
		padding = 1
	} else {
		padding = 2
	}

	addr = 0
	bufs := (*msg)[1:5]
	session = unpackUint32(&bufs)
	reqsz := sz - 5
	dm := make([]byte, reqsz)
	copy(dm, (*msg)[5:sz])
	data = &MsgPart{
		Msg: &dm,
		Sz:  reqsz,
	}
	return
}

func unpackReqString(msg *[]byte, sz uint32) (addr interface{},
	session uint32, data *MsgPart, padding int, err error) {
	if msg == nil || len(*msg) < 2 {
		err = fmt.Errorf("unpcak req string invalid msg %v need:%d", msg, 2)
		return
	}
	padding = 0
	namesz := uint32((*msg)[1])
	if sz < namesz+6 {
		err = fmt.Errorf(
			"unpack req string invalid cluster message (size=%d)", sz)
		return
	}

	if len(*msg) < int(namesz+6) {
		err = fmt.Errorf("unpcak multi req part invalid msg %v need:%d", msg, namesz+6)
		return
	}

	bufa := make([]byte, namesz)
	copy(bufa, (*msg)[2:2+namesz])
	addr = string(bufa)
	bufs := (*msg)[2+namesz : 6+namesz]
	session = unpackUint32(&bufs)
	reqsz := sz - 6 - namesz
	dm := make([]byte, reqsz)
	copy(dm, (*msg)[6+namesz:sz])
	data = &MsgPart{
		Msg: &dm,
		Sz:  reqsz,
	}
	return
}

func unpackMReqString(msg *[]byte, sz uint32) (addr interface{},
	session uint32, data *MsgPart, padding int, err error) {
	if msg == nil || len(*msg) < 2 {
		err = fmt.Errorf("unpcak multi req string invalid msg %v need:%d", msg, 2)
		return
	}
	padding = 1
	namesz := uint32((*msg)[1])
	if sz < namesz+10 || len(*msg) < int(namesz+10) {
		err = fmt.Errorf(
			"unpack multi req string invalid cluster message (size=%d)", sz)
		return
	}
	bufa := make([]byte, namesz)
	copy(bufa, (*msg)[2:2+namesz])
	addr = string(bufa)
	bufs := (*msg)[2+namesz : 6+namesz]
	session = unpackUint32(&bufs)
	bufsz := (*msg)[6+namesz : 10+namesz]
	reqsz := unpackUint32(&bufsz)
	data = &MsgPart{
		Sz: reqsz,
	}
	return
}

func UnpcakRequest(msg *[]byte, sz uint32) (addr interface{},
	session uint32, data *MsgPart, padding int, err error) {
	if msg == nil || len(*msg) < 1 {
		err = fmt.Errorf("unpcak request invalid msg %v", msg)
		return
	}
	switch (*msg)[0] {
	case 0:
		addr, session, data, padding, err = unpackReqNumber(msg, sz)
	case 1:
		addr, session, data, padding, err = unpackMReqNumber(msg, sz)
	case 2:
		fallthrough
	case 3:
		addr, session, data, padding, err = unpackMReqPart(msg, sz)
	case 0x80:
		addr, session, data, padding, err = unpackReqString(msg, sz)
	case 0x81:
		addr, session, data, padding, err = unpackMReqString(msg, sz)
	}
	return
}

func PackResponse(session uint32, ok bool,
	msg *[]byte, sz uint32) (padding []*MsgPart, err error) {
	if !ok {
		if sz > MULTI_PART {
			// truncate the error msg if too long
			sz = MULTI_PART
		}
	} else {
		if sz > MULTI_PART {
			part := (sz-1)/MULTI_PART + 2
			padding = make([]*MsgPart, part)
			reqsz := sz + 11
			buf := make([]byte, reqsz)
			fillHeader(&buf, int(sz)+9)
			bufs := buf[2:6]
			fillUint32(&bufs, session)
			buf[6] = 2
			bufsz := buf[7:11]
			fillUint32(&bufsz, sz)
			padding[0] = &MsgPart{
				Msg: &buf,
				Sz:  reqsz,
			}

			index := uint32(0)
			for i := 0; i < int(part); i++ {
				var buf []byte
				var reqsz uint32
				var s uint32
				if sz > MULTI_PART {
					s = MULTI_PART
					reqsz = s + 7
					buf = make([]byte, reqsz)
					if buf == nil {
						err = fmt.Errorf("packReqNumber multipak memmory error")
						return
					}
					buf[6] = byte(3)

				} else {
					s = sz
					reqsz = s + 7
					buf = make([]byte, reqsz)
					if buf == nil {
						err = fmt.Errorf("packReqNumber multipak memmory error")
						return
					}
					buf[6] = byte(4)
				}
				fillHeader(&buf, int(s)+5)
				bufs := buf[2:6]
				fillUint32(&bufs, session)
				m := (*msg)[index : index+s]
				copy(buf[7:reqsz], m)
				sz = sz - s
				index = index + s
				padding[i+1] = &MsgPart{
					Msg: &buf,
					Sz:  reqsz,
				}
			}
			return
		}
	}
	part := (sz-1)/MULTI_PART + 1
	reqsz := sz + 7
	padding = make([]*MsgPart, part)
	// TODO这里要改
	for i := 0; i < int(part); i++ {
		buf := make([]byte, reqsz)
		if buf == nil {
			err = fmt.Errorf("packReqNumber multipak memmory error")
			return
		}
		fillHeader(&buf, int(sz)+5)
		bufs := buf[2:6]
		fillUint32(&bufs, session)
		if ok {
			buf[6] = byte(1)
		} else {
			buf[6] = byte(0)
		}
		copy(buf[7:reqsz], *msg)

		padding[i] = &MsgPart{
			Msg: &buf,
			Sz:  reqsz,
		}
	}
	return
}

func UnpcakResponse(msg *[]byte, sz uint32) (session uint32,
	ok bool, data *MsgPart, padding bool, err error) {
	if sz < 5 {
		err = fmt.Errorf("UnpcakResponse msg sz < 5 sz:%d", sz)
		return
	}
	session = unpackUint32(msg)
	data = &MsgPart{}
	switch (*msg)[4] {
	case 0:
		ok = false
		dmsg := make([]byte, sz-5)
		copy(dmsg, (*msg)[5:sz])
		data.Msg = &dmsg
		data.Sz = sz - 5
	case 1:
		fallthrough
	case 4:
		ok = true
		dmsg := make([]byte, sz-5)
		copy(dmsg, (*msg)[5:sz])
		data.Msg = &dmsg
		data.Sz = sz - 5
	case 2:
		if sz != 9 {
			err = fmt.Errorf("UnpcakResponse msg sz < 9 sz:%d", sz)
			return
		}
		bufsz := (*msg)[5:9]
		sz = unpackUint32(&bufsz)
		ok = true
		data.Sz = sz
		padding = true
	case 3:
		ok = true
		dmsg := make([]byte, sz-5)
		copy(dmsg, (*msg)[5:sz])
		data.Msg = &dmsg
		data.Sz = sz - 5
		padding = true
	}
	return
}

func Concat(msgs []*MsgPart) (msg *[]byte, sz uint32, err error) {
	msgslen := len(msgs)
	if msgslen == 1 {
		msg = msgs[0].Msg
		sz = msgs[0].Sz
	} else {
		// 收到的多个包进行排序
		sort.SliceStable(msgs, func(i, j int) bool {
			return msgs[i].Id < msgs[j].Id
		})

		if msgs[0].Msg == nil || msgs[0].Sz < 4 || len(*msgs[0].Msg) < 4 {
			err = fmt.Errorf("concat error msg:%v need:%d  no enough", *msgs[0].Msg, msgs[0].Sz)
			return
		}
		sz = unpackUint32(msgs[0].Msg)
		buf := make([]byte, sz)
		offset := uint32(0)

		for i := 1; i < msgslen; i++ {
			temp := msgs[i]
			s := temp.Sz
			roffset := offset + s
			if roffset < sz {
				err = fmt.Errorf("concat error buff:%d need:%d no enough", sz, roffset)
				return
			}

			if temp.Msg == nil || len(*temp.Msg) < int(s) {
				err = fmt.Errorf("concat error msg:%v need:%d  no enough", *temp.Msg, temp.Sz)
				return
			}
			copy(buf[offset:roffset], *temp.Msg)
			offset = roffset
		}
		if offset != sz {
			err = fmt.Errorf(
				"concat msg no enough sz:%d offset:%d", sz, offset)
			return
		}
		msg = &buf
	}
	return
}

type block struct {
	buffer *bytes.Buffer // 缓冲区
	offset int           // 偏移量
}

// 定义一个write函数，用于向block中写入数据
func (b *block) write(data interface{}) {
	err := binary.Write(b.buffer, binary.LittleEndian, data)
	if err != nil {
		panic(err)
	}
	b.offset += binary.Size(data)
}

func combineType(t uint8, v uint8) uint8 {
	return t | v<<3
}

func wbNil(b *block) {
	b.write(byte(0))
}

func wbIntegerInt(b *block, v int64) {
	var vt uint8 = TYPE_NUMBER
	if v == 0 {
		b.write(byte(combineType(vt, TYPE_NUMBER_ZERO)))
	} else if v > 0x7fffffff || v < -0x80000000 {
		b.write(byte(combineType(vt, TYPE_NUMBER_QWORD)))
		b.write(v)
	} else if v < 0 && v >= -0x80000000 {
		b.write(byte(combineType(vt, TYPE_NUMBER_DWORD)))
		b.write(int32(v))
	} else if v < 0x100 {
		b.write(byte(combineType(vt, TYPE_NUMBER_BYTE)))
		b.write(uint8(v))
	} else if v < 0x10000 {
		b.write(byte(combineType(vt, TYPE_NUMBER_WORD)))
		b.write(uint16(v))
	} else {
		b.write(byte(combineType(vt, TYPE_NUMBER_DWORD)))
		b.write(uint32(v))
	}
}

func wbString(b *block, v string) {
	// 如果是string，写入一个字节5，表示字符串，然后写入4个字节的长度，再写入字符串的内容
	sz := len(v)
	if sz < MAX_COOKIE {
		b.write(byte(combineType(TYPE_SHORT_STRING, uint8(sz))))
		if sz > 0 {
			b.write([]byte(v))
		}
	} else {
		if sz > 0x10000 {
			b.write(byte(combineType(TYPE_LONG_STRING, 2)))
			b.write(uint16(sz))
		} else {
			b.write(byte(combineType(TYPE_LONG_STRING, 4)))
			b.write(uint32(sz))
		}
		b.write([]byte(v))
	}
}

func wbTableArray(b *block, v []interface{}, depth int) {
	sz := len(v)
	if sz >= MAX_COOKIE-1 {
		b.write(byte(combineType(TYPE_TABLE, MAX_COOKIE-1)))
		wbIntegerInt(b, int64(sz))
	} else {
		b.write(byte(combineType(TYPE_TABLE, uint8(sz))))
	}
	for _, e := range v {
		packOne(e, b, depth)
	}
	wbNil(b)
}

func wbTableHash(b *block, v map[string]interface{}, depth int) (err error) {
	if depth > MAX_DEPTH {
		err = fmt.Errorf("serialize can't pack too depth table")
		return
	}
	b.write(byte(combineType(TYPE_TABLE, 0)))
	for key, value := range v {
		err = packOne(key, b, depth)
		if err != nil {
			return
		}
		err = packOne(value, b, depth)
		if err != nil {
			return
		}
	}
	wbNil(b)
	return
}

func packOne(v interface{}, b *block, depth int) (err error) {
	// 判断lua数据的类型
	switch v := v.(type) {
	case nil:
		wbNil(b)
	case bool:
		if v {
			b.write(byte(combineType(TYPE_BOOLEAN, 1)))
		} else {
			b.write(byte(combineType(TYPE_BOOLEAN, 0)))
		}
	case int:
		wbIntegerInt(b, int64(v))
	case int64:
		wbIntegerInt(b, v)
	case float64:
		b.write(byte(combineType(TYPE_NUMBER, TYPE_NUMBER_REAL)))
		b.write(v)
	case string:
		wbString(b, v)
	// 对应lua的TYPE_USERDATA指针，go用不到
	// case *[]byte:
	// 	b.write(byte(TYPE_USERDATA))
	// 	b.write(v)
	case []interface{}:
		wbTableArray(b, v, depth)
	case map[string]interface{}:
		err = wbTableHash(b, v, depth+1)
	default:
		// 如果是其他类型，抛出异常
		err = fmt.Errorf("invalid type: %v", reflect.TypeOf(v))
	}
	return err
}

// 定义一个pack函数，用于序列化lua数据
func Pack(args []interface{}) (msg *[]byte, sz int, err error) {
	defer share.Recover(func(e error) {
		hlog.Errorf("pack error:%s", e.Error())
		err = e
	})
	// 创建一个block
	b := &block{
		buffer: new(bytes.Buffer),
		offset: 0,
	}

	for _, v := range args {
		// 调用pack_one函数，递归地处理lua数据
		err = packOne(v, b, 0)
		if err != nil {
			return
		}
	}
	// 返回block中的字节切片
	buf := b.buffer.Bytes()
	msg = &buf
	sz = b.offset
	return
}

func separateType(vtb uint8) (uint8, uint8) {
	return vtb & 0x7, vtb >> 3
}

func getFloat64(bytes []byte) float64 {
	bits := binary.LittleEndian.Uint64(bytes)
	return math.Float64frombits(bits)
}

func getInt(msg *[]byte, offset uint32,
	vc uint8) (roffset uint32, arg interface{}, err error) {
	switch vc {
	case TYPE_NUMBER_ZERO:
		roffset = offset
		arg = 0
	case TYPE_NUMBER_BYTE:
		roffset = offset + 1
		if msg == nil || len(*msg) < int(roffset) {
			err = fmt.Errorf("get int invalid msg:%v need:%d", msg, roffset)
			return
		}
		arg = int((*msg)[offset])
	case TYPE_NUMBER_WORD:
		roffset = offset + 2
		if msg == nil || len(*msg) < int(roffset) {
			err = fmt.Errorf("get int invalid msg:%v need:%d", msg, roffset)
			return
		}
		arg = binary.LittleEndian.Uint16((*msg)[offset:roffset])
		if arg.(uint16) >= 0x8000 {
			arg = arg.(int16)
		} else {
			arg = arg.(uint16)
		}
	case TYPE_NUMBER_DWORD:
		roffset = offset + 4
		if msg == nil || len(*msg) < int(roffset) {
			err = fmt.Errorf("get int invalid msg:%v need:%d", msg, roffset)
			return
		}
		arg = binary.LittleEndian.Uint32((*msg)[offset:roffset])
		arg = int32(arg.(uint32))
	case TYPE_NUMBER_QWORD:
		roffset = offset + 8
		if msg == nil || len(*msg) < int(roffset) {
			err = fmt.Errorf("get int invalid msg:%v need:%d", msg, roffset)
			return
		}
		arg = binary.LittleEndian.Uint64((*msg)[offset:roffset])
		arg = arg.(int64)
	default:
		err = fmt.Errorf("invalid serialize integer type:%d", vc)
	}
	return
}

func unpackOne(msg *[]byte, offset uint32,
	allsz uint32) (roffset uint32, arg interface{}, err error) {
	if msg == nil || len(*msg) < int(offset)+1 {
		err = fmt.Errorf("unpack one invalid msg:%v need:%d", msg, offset+1)
		return
	}
	// if offset >= allsz-1 {
	// 	roffset = offset
	// 	return
	// }
	vtb := (*msg)[offset]
	roffset, arg, err = pushValue(msg, offset, vtb, allsz)
	if err != nil {
		return
	}
	return
}

func getTableHash(msg *[]byte, offset uint32,
	allsz uint32) (roffset uint32, arg map[string]interface{}, err error) {
	arg = make(map[string]interface{})
	st := time.Now().Unix()
	for {
		if time.Now().Unix()-st > 10 {
			err = fmt.Errorf("getTableHash maybe infinite loop")
			break
		}
		var (
			k interface{}
			v interface{}
		)
		offset, k, err = unpackOne(msg, offset, allsz)
		if err != nil {
			break
		}

		if offset >= allsz-1 {
			break
		}

		if k == nil {
			break
		}

		offset, v, err = unpackOne(msg, offset, allsz)
		if err != nil {
			break
		}
		arg[k.(string)] = v

		if offset >= allsz-1 {
			break
		}
	}
	roffset = offset
	return
}

func getTableArray(msg *[]byte, offset uint32,
	vc uint8, allsz uint32) (roffset uint32, arg []interface{}, err error) {
	var arraysz interface{}
	if vc == MAX_COOKIE-1 {
		offset, arraysz, err = getInt(msg, offset, 1)
		if err != nil {
			return
		}
		vc = arraysz.(uint8)
		if (vc & 7) != TYPE_NUMBER {
			err = fmt.Errorf("getTableArray arraysz:%d error", vc&7)
			return
		}
		vc = vc >> 3

		if vc == TYPE_NUMBER_REAL {
			err = fmt.Errorf("getTableArray arraysz:%d error", vc&7)
			return
		}

		offset, arraysz, err = getInt(msg, offset, vc)
		if err != nil {
			return
		}
	} else {
		arraysz = vc
	}
	var asz int
	switch v := arraysz.(type) {
	case uint8:
		asz = int(v)
	case uint16:
		asz = int(v)
	case uint32:
		asz = int(v)
	case uint64:
		asz = int(v)
	case int32:
		asz = int(v)
	case int64:
		asz = int(v)
	default:
		err = fmt.Errorf("getTableArray arraysz type:%v error", arraysz)
		return
	}

	arg = make([]interface{}, asz)
	for i := 0; i < asz; i++ {
		var item interface{}
		offset, item, err = unpackOne(msg, offset, allsz)
		if err != nil {
			return
		}
		arg[i] = item
	}
	roffset = offset
	return
}

func getTable(msg *[]byte, offset uint32,
	vc uint8, allsz uint32) (roffset uint32, arg interface{}, err error) {
	if vc == 0 {
		offset, arg, err = getTableHash(msg, offset, allsz)
		// hash table 已经跳过了最后面的pack的nil了
	} else {
		offset, arg, err = getTableArray(msg, offset, vc, allsz)
		if err == nil {
			// 跳过arrat最后面的pack的nil
			offset++
		}
	}
	roffset = offset
	return
}

func pushValue(msg *[]byte, offset uint32,
	vtb uint8, allsz uint32) (roffset uint32, arg interface{}, err error) {
	vt, vc := separateType(vtb)
	switch vt {
	case TYPE_NIL:
		offset = offset + 1
	case TYPE_BOOLEAN:
		arg = false
		if vc == 1 {
			arg = true
		}
		offset = offset + 1
	case TYPE_NUMBER:
		if vc == TYPE_NUMBER_REAL {
			start := offset
			offset = offset + 8
			if msg == nil || len(*msg) < int(offset)+8 {
				err = fmt.Errorf("push value invalid 1 msg:%v need:%d", msg, offset+8)
				return
			}
			buf := (*msg)[start:offset]
			arg = getFloat64(buf)
		} else {
			offset, arg, err = getInt(msg, offset, vc)
		}
	case TYPE_USERDATA:
		// go不能使用lua的指针
		offset += 8
	case TYPE_SHORT_STRING:
		start := offset
		l := uint32(vc)
		offset = start + l
		if msg == nil || len(*msg) < int(offset) {
			err = fmt.Errorf("push value invalid 2 msg:%v need:%d", msg, offset)
			return
		}
		buf := make([]byte, l)
		copy(buf, (*msg)[start:offset])
		arg = string(buf)
	case TYPE_LONG_STRING:
		if vc != 2 && vc != 4 {
			err = fmt.Errorf("push value invalid 3 serialize stream vc:%d", vc)
			return
		}
		dt := uint32(vc)
		start := offset
		if msg == nil || len(*msg) < int(start+dt) {
			err = fmt.Errorf("push value invalid 4 msg:%v need:%d", msg, start+dt)
			return
		}
		sz := binary.LittleEndian.Uint16((*msg)[start : start+dt])
		start = start + dt
		l := uint32(sz)
		offset = start + l
		if len(*msg) < int(offset) {
			err = fmt.Errorf("push value invalid 5 msg:%v need:%d", msg, offset)
			return
		}
		buf := make([]byte, l)
		copy(buf, (*msg)[start:offset])
		arg = string(buf)
	case TYPE_TABLE:
		offset, arg, err = getTable(msg, offset, vc, allsz)
	default:
		err = fmt.Errorf("invalid serialize 6 type vt:%d", vt)
	}
	roffset = offset
	return
}

func Unpack(msg *[]byte, sz uint32) (args []interface{}, err error) {
	args = make([]interface{}, 0, 10)
	var offset uint32 = 0
	st := time.Now().Unix()
	for {
		if time.Now().Unix()-st > 10 {
			err = fmt.Errorf("unpack maybe infinite loop")
			break
		}
		if offset >= sz {
			break
		}
		var arg interface{}
		offset, arg, err = unpackOne(msg, offset, sz)
		if err != nil {
			return
		}
		args = append(args, arg)
	}
	return
}
