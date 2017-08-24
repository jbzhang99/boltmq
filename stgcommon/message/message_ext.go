package message

import (
	"bytes"
	"encoding/binary"
	"net"
	"strconv"

	"git.oschina.net/cloudzone/smartgo/stgcommon/sysflag"
)

type MessageExt struct {
	//// 消息主题
	//Topic                     string
	//// 消息标志，系统不做干预，完全由应用决定如何使用
	//Flag                      int
	//// 消息属性，都是系统属性，禁止应用设置
	//Properties                map[string]string
	//// 消息体
	//Body                      []byte
	Message
	// 队列ID <PUT>
	QueueId                   int32
	// 存储记录大小
	StoreSize                 int32
	// 队列偏移量
	QueueOffset               int64
	// 消息标志位 <PUT>
	SysFlag                   int32
	// 消息在客户端创建时间戳 <PUT>
	BornTimestamp             int64
	// 消息来自哪里 <PUT>
	BornHost                  string
	// 消息在服务器存储时间戳
	StoreTimestamp            int64
	// 消息存储在哪个服务器 <PUT>
	StoreHost                 string
	// 消息ID
	MsgId                     string
	// 消息对应的Commit Log Offset
	CommitLogOffset           int64
	// 消息体CRC
	BodyCRC                   int32
	// 当前消息被某个订阅组重新消费了几次（订阅组之间独立计数）
	ReconsumeTimes            int32
	PreparedTransactionOffset int64
}

// Encode 编码MessageExt
func (msgExt *MessageExt) Encode() ([]byte, error) {
	var (
		buf        = bytes.NewBuffer([]byte{})
		magicCode  int32
		port       int32
		bodyLength int32
		newBody    []byte
		e          error
	)

	// 1 TOTALSIZE
	e = binary.Write(buf, binary.BigEndian, &msgExt.StoreSize)
	if e != nil {
		return nil, e
	}

	// 2 MAGICCODE
	magicCode = int32(MessageMagicCode)
	e = binary.Write(buf, binary.BigEndian, &magicCode)
	if e != nil {
		return nil, e
	}

	// 3 BODYCRC
	e = binary.Write(buf, binary.BigEndian, &msgExt.BodyCRC)
	if e != nil {
		return nil, e
	}

	// 4 QUEUEID
	e = binary.Write(buf, binary.BigEndian, &msgExt.QueueId)
	if e != nil {
		return nil, e
	}

	// 5 FLAG
	e = binary.Write(buf, binary.BigEndian, &msgExt.Flag)
	if e != nil {
		return nil, e
	}

	// 6 QUEUEOFFSET
	e = binary.Write(buf, binary.BigEndian, &msgExt.QueueOffset)
	if e != nil {
		return nil, e
	}

	// 7 PHYSICALOFFSET
	e = binary.Write(buf, binary.BigEndian, &msgExt.CommitLogOffset)
	if e != nil {
		return nil, e
	}

	// 8 SYSFLAG
	e = binary.Write(buf, binary.BigEndian, &msgExt.SysFlag)
	if e != nil {
		return nil, e
	}

	// 9 BORNTIMESTAMP
	e = binary.Write(buf, binary.BigEndian, &msgExt.BornTimestamp)
	if e != nil {
		return nil, e
	}

	// 10 BORNHOST
	bornHost, bornPortStr, e := net.SplitHostPort(msgExt.BornHost)
	if e != nil {
		return nil, e
	}
	_, e = buf.Write(ipv4StringToBytes(bornHost))
	if e != nil {
		return nil, e
	}

	bornPort, e := strconv.ParseInt(bornPortStr, 10, 32)
	if e != nil {
		return nil, e
	}
	port = int32(bornPort)
	e = binary.Write(buf, binary.BigEndian, &port)
	if e != nil {
		return nil, e
	}

	// 11 STORETIMESTAMP
	e = binary.Write(buf, binary.BigEndian, &msgExt.StoreTimestamp)
	if e != nil {
		return nil, e
	}

	// 12 STOREHOST
	storeHost, storePortStr, e := net.SplitHostPort(msgExt.StoreHost)
	if e != nil {
		return nil, e
	}
	_, e = buf.Write(ipv4StringToBytes(storeHost))
	if e != nil {
		return nil, e
	}

	storePort, e := strconv.ParseInt(storePortStr, 10, 32)
	if e != nil {
		return nil, e
	}
	port = int32(storePort)
	e = binary.Write(buf, binary.BigEndian, &port)
	if e != nil {
		return nil, e
	}

	// 13 RECONSUMETIMES
	e = binary.Write(buf, binary.BigEndian, &msgExt.ReconsumeTimes)
	if e != nil {
		return nil, e
	}

	// 14 Prepared Transaction Offset
	e = binary.Write(buf, binary.BigEndian, &msgExt.PreparedTransactionOffset)
	if e != nil {
		return nil, e
	}

	// 15 BODY
	bodyLength = int32(len(msgExt.Body))
	e = binary.Write(buf, binary.BigEndian, &bodyLength)
	if e != nil {
		return nil, e
	}
	if bodyLength > 0 {
		if (msgExt.SysFlag & sysflag.CompressedFlag) == sysflag.CompressedFlag {
			// 压缩报文
			newBody, e = zip(msgExt.Body)
			if e != nil {
				return nil, e
			}
		} else {
			newBody = msgExt.Body
		}

		_, e = buf.Write(newBody)
		if e != nil {
			return nil, e
		}
	}

	// 16 TOPIC
	e = binary.Write(buf, binary.BigEndian, byte(len(msgExt.Topic)))
	if e != nil {
		return nil, e
	}
	_, e = buf.WriteString(msgExt.Topic)
	if e != nil {
		return nil, e
	}

	// 17 properties
	properties := MessageProperties2String(msgExt.Properties)
	e = binary.Write(buf, binary.BigEndian, int16(len(properties)))
	if e != nil {
		return nil, e
	}
	_, e = buf.WriteString(properties)
	if e != nil {
		return nil, e
	}

	return buf.Bytes(), nil
}
