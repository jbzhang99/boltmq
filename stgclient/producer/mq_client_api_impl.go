package producer

import (
	"git.oschina.net/cloudzone/smartgo/stgcommon/protocol/heartbeat"
	"git.oschina.net/cloudzone/smartgo/stgcommon/protocol/route"
	"git.oschina.net/cloudzone/smartgo/stgcommon/message"
	"git.oschina.net/cloudzone/smartgo/stgcommon/protocol/header"
	"strings"
	"git.oschina.net/cloudzone/smartgo/stgclient"
)

// MQClientAPIImpl: 内部使用核心处理api
// Author: yintongqiang
// Since:  2017/8/8

type MQClientAPIImpl struct {
	ClientRemotingProcessor *ClientRemotingProcessor
	ProjectGroupPrefix      string
}

func NewMQClientAPIImpl(clientRemotingProcessor *ClientRemotingProcessor) *MQClientAPIImpl {

	return &MQClientAPIImpl{
		ClientRemotingProcessor:clientRemotingProcessor,
	}
}
// 调用romoting的start
func (impl *MQClientAPIImpl)Start() {

}

func (impl *MQClientAPIImpl)SendHeartbeat(addr string, heartbeatData *heartbeat.HeartbeatData, timeoutMillis int64) {

}

func (impl *MQClientAPIImpl)GetDefaultTopicRouteInfoFromNameServer(topic string, timeoutMillis int64) *route.TopicRouteData {
	return &route.TopicRouteData{}
}

func (impl *MQClientAPIImpl)GetTopicRouteInfoFromNameServer(topic string, timeoutMillis int64) *route.TopicRouteData {
	routeData := &route.TopicRouteData{}
	routeData.QueueDatas = append(routeData.QueueDatas, &route.QueueData{BrokerName:"broker-master2", ReadQueueNums:8, WriteQueueNums:8, Perm:6, TopicSynFlag:0})
	mapBrokerAddrs := make(map[int]string)
	mapBrokerAddrs[0] = "10.128.31.124:10911"
	mapBrokerAddrs[1] = "10.128.31.125:10911"
	routeData.BrokerDatas = append(routeData.BrokerDatas, &route.BrokerData{BrokerName:"broker-master2", BrokerAddrs:mapBrokerAddrs})
	return routeData
}

func (impl *MQClientAPIImpl)sendMessage(addr string, brokerName string, msg message.Message, requestHeader header.SendMessageRequestHeader, timeoutMillis int64, communicationMode CommunicationMode, sendCallback SendCallback) SendResult {
	if !strings.EqualFold(impl.ProjectGroupPrefix, "") {
		msg.Topic=stgclient.BuildWithProjectGroup(msg.Topic, impl.ProjectGroupPrefix)
		requestHeader.ProducerGroup=stgclient.BuildWithProjectGroup(requestHeader.ProducerGroup, impl.ProjectGroupPrefix)
		requestHeader.Topic=stgclient.BuildWithProjectGroup(requestHeader.Topic, impl.ProjectGroupPrefix)
	}
	// 默认send采用v2版本
	//requestHeaderV2:=header.CreateSendMessageRequestHeaderV2(requestHeader)
	//todo  request = RemotingCommand.createRequestCommand(RequestCode.SEND_MESSAGE_V2, requestHeaderV2)
	switch (communicationMode) {
	case ONEWAY:
	case ASYNC:
	case SYNC:
		return impl.sendMessageSync(addr, brokerName, msg, timeoutMillis)
	default:
		break
	}

	return SendResult{}
}

func (impl *MQClientAPIImpl)sendMessageSync(addr string,brokerName string,msg message.Message,timeoutMillis int64)SendResult  {
return  SendResult{}
}
