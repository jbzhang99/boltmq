package stgbroker

import (
	"fmt"
	"git.oschina.net/cloudzone/smartgo/stgbroker/mqtrace"
	"git.oschina.net/cloudzone/smartgo/stgclient/consumer/listener"
	"git.oschina.net/cloudzone/smartgo/stgcommon"
	"git.oschina.net/cloudzone/smartgo/stgcommon/constant"
	"git.oschina.net/cloudzone/smartgo/stgcommon/message"
	commonprotocol "git.oschina.net/cloudzone/smartgo/stgcommon/protocol"
	"git.oschina.net/cloudzone/smartgo/stgcommon/protocol/header"
	"git.oschina.net/cloudzone/smartgo/stgcommon/sysflag"
	"git.oschina.net/cloudzone/smartgo/stgnet/protocol"
	"git.oschina.net/cloudzone/smartgo/stgstorelog"
	"net"
)

// SendMessageProcessor 处理客户端发送消息的请求
// Author gaoyanlei
// Since 2017/8/24
type SendMessageProcessor struct {
	*AbstractSendMessageProcessor
	BrokerController *BrokerController
}

func NewSendMessageProcessor(brokerController *BrokerController) *SendMessageProcessor {
	var sendMessageProcessor = new(SendMessageProcessor)
	sendMessageProcessor.BrokerController = brokerController
	return sendMessageProcessor
}
func (smp *SendMessageProcessor) ProcessRequest(addr string, conn net.Conn, request *protocol.RemotingCommand) (*protocol.RemotingCommand, error){

	if request.Code == commonprotocol.CONSUMER_SEND_MSG_BACK {
		return smp.consumerSendMsgBack(request),nil
	}

	requestHeader := smp.parseRequestHeader(request)
	if requestHeader == nil {
		return nil,nil
	}

	mqtraceContext := smp.buildMsgContext(requestHeader)
	// TODO  this.executeSendMessageHookBefore(ctx, request, mqtraceContext)
	response := smp.sendMessage(conn, request, mqtraceContext, requestHeader)
	// TODO this.executeSendMessageHookAfter(response, mqtraceContext);
	return response,nil
}

// consumerSendMsgBack 客户端返回未消费消息
// Author gaoyanlei
// Since 2017/8/17
func (smp *SendMessageProcessor) consumerSendMsgBack( // TODO ChannelHandlerContext ctx
	request *protocol.RemotingCommand) (remotingCommand *protocol.RemotingCommand) {
	response := &protocol.RemotingCommand{}
	requestHeader := header.NewConsumerSendMsgBackRequestHeader()

	// 消息轨迹：记录消费失败的消息
	if len(requestHeader.OriginMsgId) > 0 {
		context := new(mqtrace.ConsumeMessageContext)
		context.ConsumerGroup = requestHeader.Group
		context.Topic = requestHeader.OriginTopic
		// TODO context.ClientHost=RemotingHelper.parseChannelRemoteAddr(ctx.channel()
		context.Success = false
		context.Status = string(listener.RECONSUME_LATER)
		messageIds := make(map[string]int64)
		messageIds[requestHeader.OriginMsgId] = requestHeader.Offset
		context.MessageIds = messageIds
		// TODO this.executeConsumeMessageHookAfter(context);
	}

	// 确保订阅组存在
	subscriptionGroupConfig := smp.BrokerController.SubscriptionGroupManager.findSubscriptionGroupConfig(requestHeader.Group)
	if subscriptionGroupConfig == nil {
		response.Code = commonprotocol.SUBSCRIPTION_GROUP_NOT_EXIST
		response.Remark = "subscription group not exist"
	}

	// 检查Broker权限
	if constant.IsWriteable(smp.BrokerController.BrokerConfig.BrokerPermission) {
		response.Code = commonprotocol.NO_PERMISSION
		response.Remark = "the broker[" + smp.BrokerController.BrokerConfig.BrokerIP1 + "] sending message is forbidden"
		return response
	}

	// 如果重试队列数目为0，则直接丢弃消息
	if subscriptionGroupConfig.RetryQueueNums <= 0 {
		response.Code = commonprotocol.SUCCESS
		response.Remark = ""
		return response
	}

	newTopic := stgcommon.GetRetryTopic(requestHeader.Group)
	var queueIdInt int32
	if queueIdInt < 0 {
		num := (smp.Rand.Int31() % 99999999) % subscriptionGroupConfig.RetryQueueNums
		if num > 0 {
			queueIdInt = num
		} else {
			queueIdInt = -num
		}
	}

	// 如果是单元化模式，则对 topic 进行设置
	topicSysFlag := 0
	if requestHeader.UnitMode {
		topicSysFlag = sysflag.TopicBuildSysFlag(false, true)
	}

	// 检查topic是否存在
	topicConfig, err := smp.BrokerController.TopicConfigManager.createTopicInSendMessageBackMethod(newTopic, subscriptionGroupConfig.RetryQueueNums,
		constant.PERM_WRITE|constant.PERM_READ, topicSysFlag)
	if topicConfig == nil || err != nil {
		response.Code = commonprotocol.SYSTEM_ERROR
		response.Remark = "topic[" + newTopic + "] not exist"
		return response
	}

	// 检查topic权限
	if !constant.IsWriteable(topicConfig.Perm) {
		response.Code = commonprotocol.NO_PERMISSION
		response.Remark = "the topic[" + newTopic + "] sending message is forbidden"
		return response
	}

	// 查询消息，这里如果堆积消息过多，会访问磁盘
	// 另外如果频繁调用，是否会引起gc问题，需要关注
	// TODO  msgExt :=smp.BrokerController.getMessageStore().lookMessageByOffset(requestHeader.getOffset());
	msgExt := new(message.MessageExt)
	if nil == msgExt {
		response.Code = commonprotocol.SYSTEM_ERROR
		response.Remark = "look message by offset failed, " + string(requestHeader.Offset)
		return response
	}

	// 构造消息
	retryTopic := msgExt.GetProperty(message.PROPERTY_RETRY_TOPIC)
	if "" == retryTopic {
		message.PutProperty(&msgExt.Message, message.PROPERTY_RETRY_TOPIC, msgExt.Topic)
	}
	msgExt.SetWaitStoreMsgOK(false)

	// 客户端自动决定定时级别
	delayLevel := requestHeader.DelayLevel

	// 死信消息处理
	if msgExt.ReconsumeTimes >= subscriptionGroupConfig.RetryMaxTimes || delayLevel < 0 {
		newTopic = stgcommon.GetDLQTopic(requestHeader.Group)
		if queueIdInt < 0 {
			num := (smp.Rand.Int31() % 99999999) % DLQ_NUMS_PER_GROUP
			if num > 0 {
				queueIdInt = num
			} else {
				queueIdInt = -num
			}
		}

		topicConfig, err =
			smp.BrokerController.TopicConfigManager.createTopicInSendMessageBackMethod(
				newTopic, DLQ_NUMS_PER_GROUP, constant.PERM_WRITE, 0)
		if nil == topicConfig {
			response.Code = commonprotocol.SYSTEM_ERROR
			response.Remark = "topic[" + newTopic + "] not exist"
			return response
		}
	} else {
		if 0 == delayLevel {
			delayLevel = 3 + msgExt.ReconsumeTimes
		}

		msgExt.SetDelayTimeLevel(int(delayLevel))
	}

	msgInner := new(stgstorelog.MessageExtBrokerInner)
	msgInner.Topic = newTopic
	msgInner.Body = msgExt.Body
	msgInner.Flag = msgExt.Flag
	message.SetPropertiesMap(&msgInner.Message, msgExt.Properties)
	// TODO msgInner.PropertiesString(MessageDecoder.messageProperties2String(msgExt.getProperties()));
	// TODO msgInner.TagsCode(MessageExtBrokerInner.tagsString2tagsCode(null, msgExt.getTags()));

	msgInner.QueueId = int32(queueIdInt)
	msgInner.SysFlag = msgExt.SysFlag
	msgInner.BornTimestamp = msgExt.BornTimestamp
	msgInner.BornHost = msgExt.BornHost
	msgInner.StoreHost = smp.StoreHost
	msgInner.ReconsumeTimes = msgExt.ReconsumeTimes + 1

	// 保存源生消息的 msgId
	originMsgId := message.GetOriginMessageId(msgExt.Message)
	if originMsgId == "" || len(originMsgId) <= 0 {
		originMsgId = msgExt.MsgId

	}
	message.SetOriginMessageId(&msgInner.Message, originMsgId)

	// TODO this.brokerController.getMessageStore().putMessage(msgInner)
	putMessageResult := new(stgstorelog.PutMessageResult)

	if putMessageResult != nil {
		switch putMessageResult.PutMessageStatus {
		case stgstorelog.PUTMESSAGE_PUT_OK:
			backTopic := msgExt.Topic
			correctTopic := msgExt.GetProperty(message.PROPERTY_RETRY_TOPIC)
			if correctTopic == "" || len(correctTopic) <= 0 {
				backTopic = correctTopic
			}
			fmt.Println(backTopic)
			// TODO smp.BrokerController.getBrokerStatsManager().incSendBackNums(requestHeader.getGroup(), backTopic);

			response.Code = commonprotocol.SUCCESS
			response.Remark = ""

			return response
		default:
			break
		}
		response.Code = commonprotocol.SYSTEM_ERROR
		response.Remark = putMessageResult.PutMessageStatus.PutMessageString()
		return response
	}
	response.Code = commonprotocol.SYSTEM_ERROR
	response.Remark = "putMessageResult is null"
	return response
}

// sendMessage 正常消息
// Author gaoyanlei
// Since 2017/8/17
func (smp *SendMessageProcessor) sendMessage(conn net.Conn, request *protocol.RemotingCommand,
	mqtraceContext *mqtrace.SendMessageContext, requestHeader *header.SendMessageRequestHeader) *protocol.RemotingCommand {
	response := &protocol.RemotingCommand{}
	responseHeader := new(header.SendMessageResponseHeader)
	response.Opaque = request.Opaque
	response.Code = -1
	smp.msgCheck(conn, requestHeader, response)
	if response.Code != -1 {
		return response
	}

	body := request.Body

	queueIdInt := requestHeader.QueueId

	topicConfig := smp.BrokerController.TopicConfigManager.selectTopicConfig(requestHeader.Topic)

	if queueIdInt < 0 {
		num := (smp.Rand.Int31() % 99999999) % topicConfig.WriteQueueNums
		if num > 0 {
			queueIdInt = int32(num)
		} else {
			queueIdInt = -int32(num)
		}

	}

	sysFlag := requestHeader.SysFlag
	if stgcommon.MULTI_TAG == topicConfig.TopicFilterType {
		sysFlag |= sysflag.MultiTagsFlag
	}
	msgInner := new(stgstorelog.MessageExtBrokerInner)
	msgInner.Topic = requestHeader.Topic
	msgInner.Body = body
	message.SetPropertiesMap(&msgInner.Message, message.String2messageProperties(requestHeader.Properties))
	msgInner.PropertiesString = requestHeader.Properties
	msgInner.TagsCode = stgstorelog.TagsString2tagsCode(topicConfig.TopicFilterType, msgInner.GetTags())
	msgInner.QueueId = queueIdInt
	msgInner.SysFlag = sysFlag
	msgInner.BornTimestamp = requestHeader.BornTimestamp
	msgInner.BornHost = conn.LocalAddr().String()
	msgInner.StoreHost = smp.StoreHost
	if requestHeader.ReconsumeTimes == 0 {
		msgInner.ReconsumeTimes = 0
	} else {
		msgInner.ReconsumeTimes = requestHeader.ReconsumeTimes
	}

	if smp.BrokerController.BrokerConfig.RejectTransactionMessage {
		traFlag := msgInner.GetProperty(message.PROPERTY_TRANSACTION_PREPARED)
		if len(traFlag) > 0 {
			response.Code = commonprotocol.NO_PERMISSION
			response.Remark = "the broker[" + smp.BrokerController.BrokerConfig.BrokerIP1 + "] sending transaction message is forbidden"
			return response
		}
	}

	// TODO this.brokerController.getMessageStore().putMessage(msgInner)
	putMessageResult := new(stgstorelog.PutMessageResult)

	if putMessageResult != nil {
		sendOK := false
		switch putMessageResult.PutMessageStatus {
		case stgstorelog.PUTMESSAGE_PUT_OK:
			sendOK = true
			response.Code = commonprotocol.SUCCESS
		case stgstorelog.FLUSH_DISK_TIMEOUT:
			response.Code = commonprotocol.FLUSH_DISK_TIMEOUT
			sendOK = true
			break
		case stgstorelog.FLUSH_SLAVE_TIMEOUT:
			response.Code = commonprotocol.FLUSH_SLAVE_TIMEOUT
			sendOK = true
		case stgstorelog.SLAVE_NOT_AVAILABLE:
			response.Code = commonprotocol.SLAVE_NOT_AVAILABLE
			sendOK = true

		case stgstorelog.CREATE_MAPEDFILE_FAILED:
			response.Code = commonprotocol.SYSTEM_ERROR
			response.Remark = "create maped file failed, please make sure OS and JDK both 64bit."
		case stgstorelog.MESSAGE_ILLEGAL:
			response.Code = commonprotocol.MESSAGE_ILLEGAL
			response.Remark = "the message is illegal, maybe length not matched."
			break
		case stgstorelog.SERVICE_NOT_AVAILABLE:
			response.Code = commonprotocol.SERVICE_NOT_AVAILABLE
			response.Remark = "service not available now, maybe disk full, " + smp.diskUtil() + ", maybe your broker machine memory too small."
		case stgstorelog.PUTMESSAGE_UNKNOWN_ERROR:
			response.Code = commonprotocol.SYSTEM_ERROR
			response.Remark = "UNKNOWN_ERROR"
		default:
			response.Code = commonprotocol.SYSTEM_ERROR
			response.Remark = "UNKNOWN_ERROR DEFAULT"
		}

		if sendOK {
			//TODO   this.brokerController.getBrokerStatsManager().incTopicPutNums(msgInner.getTopic());
			//TODO this.brokerController.getBrokerStatsManager().incTopicPutSize(msgInner.getTopic(),
			//TODO 	putMessageResult.getAppendMessageResult().getWroteBytes());
			//TODO this.brokerController.getBrokerStatsManager().incBrokerPutNums();
			response.Remark = ""
			responseHeader.MsgId = putMessageResult.AppendMessageResult.MsgId
			responseHeader.QueueId = queueIdInt
			responseHeader.QueueOffset = putMessageResult.AppendMessageResult.LogicsOffset

			DoResponse( // TODO  ctx
				request, response)
			if smp.BrokerController.BrokerConfig.LongPollingEnable {
				// TODO 	  this.brokerController.getPullRequestHoldService().notifyMessageArriving(
				// TODO requestHeader.getTopic(), queueIdInt,
				// TODO 	putMessageResult.getAppendMessageResult().getLogicsOffset() + 1);
			}

		}

	} else {
		response.Code = commonprotocol.SYSTEM_ERROR
		response.Remark = "store putMessage return null"
	}

	return response
}

func (smp *SendMessageProcessor) diskUtil() string {
	// TODO
	return ""
}
