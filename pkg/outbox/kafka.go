package outbox

import (
	"errors"
	"fmt"
	"time"

	"github.com/IBM/sarama"
)

// Settings of the relay's producer (ADR 0012, p. 4).
const (
	producerTimeout = 5 * time.Second
	// MaxMessageBytes is max.message.bytes of the push topics; an event
	// is at most userbox.MaxEventBytes, the rest is room for headers.
	MaxMessageBytes = 8 << 20
)

// KafkaVersion is the protocol version of the clients: the broker is 3.9,
// idempotence needs 0.11 and static membership 2.3.
var KafkaVersion = sarama.V3_6_0_0

// ProducerConfig is the config of the relay's producer: idempotent, one
// request in flight per broker (the order in a partition), bounded
// retries and timeouts that end before the relay's round does.
func ProducerConfig(clientID string) *sarama.Config {
	c := sarama.NewConfig()
	c.ClientID = clientID
	c.Version = KafkaVersion
	c.Producer.Idempotent = true
	c.Producer.RequiredAcks = sarama.WaitForAll
	c.Net.MaxOpenRequests = 1
	c.Producer.Retry.Max = 2
	c.Producer.Retry.Backoff = 200 * time.Millisecond
	c.Producer.Timeout = producerTimeout
	c.Net.DialTimeout = 3 * time.Second
	c.Net.ReadTimeout = producerTimeout + time.Second
	c.Net.WriteTimeout = producerTimeout
	c.Metadata.Timeout = 10 * time.Second
	c.Metadata.Retry.Max = 2
	c.Metadata.AllowAutoTopicCreation = false
	c.Producer.MaxMessageBytes = MaxMessageBytes
	c.Producer.Partitioner = sarama.NewHashPartitioner
	c.Producer.Return.Successes = true
	c.Producer.Return.Errors = true
	return c
}

// sendClass is what a producer error means for its row.
type sendClass uint8

const (
	sendTransient sendClass = iota // try again in a later round
	sendPermanent                  // the broker will never take this message
	sendTopic                      // the topic is wrong, not the message
)

// classifySend sorts a ProducerError.Err.
func classifySend(err error) sendClass {
	var kerr sarama.KError
	if errors.As(err, &kerr) {
		switch kerr {
		case sarama.ErrMessageSizeTooLarge, sarama.ErrInvalidMessage, sarama.ErrInvalidMessageSize, sarama.ErrInvalidRecord:
			return sendPermanent
		case sarama.ErrUnknownTopicOrPartition, sarama.ErrInvalidTopic, sarama.ErrTopicAuthorizationFailed:
			return sendTopic
		}
		return sendTransient
	}
	// Refused by the client before it reached a broker: the size check
	// (Producer.MaxMessageBytes) and encoding.
	var cerr sarama.ConfigurationError
	var perr sarama.PacketEncodingError
	if errors.As(err, &cerr) || errors.As(err, &perr) {
		return sendPermanent
	}
	return sendTransient
}

// kafkaConn is the relay's connection to Kafka: a producer, and the admin
// side for the topics, the committed offsets of the consumer groups and
// the bounds of the logs.
type kafkaConn interface {
	// Producer returns the current producer, creating it if needed.
	Producer() (sarama.AsyncProducer, error)
	// DropProducer abandons the current producer without waiting for it:
	// its late answers are drained and ignored.
	DropProducer()
	// Partitions returns the partition count of each topic, or an error per
	// topic that has none (missing, unauthorized).
	Partitions(topics []string) (map[string]int, map[string]error, error)
	// Committed returns the committed offsets of group on topic, -1 where
	// none is committed.
	Committed(group, topic string, partitions []int32) (map[int32]int64, error)
	// Bounds returns the first offset and the end of the log of a
	// partition.
	Bounds(topic string, partition int32) (first, end int64, err error)
	Close()
}

// dial connects to the brokers.
type dialer func(brokers []string, clientID string) (kafkaConn, error)

type saramaConn struct {
	brokers  []string
	clientID string
	client   sarama.Client
	admin    sarama.ClusterAdmin
	producer sarama.AsyncProducer
}

func dialSarama(brokers []string, clientID string) (kafkaConn, error) {
	cfg := ProducerConfig(clientID)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := sarama.NewClient(brokers, cfg)
	if err != nil {
		return nil, err
	}
	admin, err := sarama.NewClusterAdminFromClient(client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return &saramaConn{brokers: brokers, clientID: clientID, client: client, admin: admin}, nil
}

func (c *saramaConn) Producer() (sarama.AsyncProducer, error) {
	if c.producer != nil {
		return c.producer, nil
	}
	// A producer of its own client: dropping it after a stuck round must
	// not close the admin's connections.
	p, err := sarama.NewAsyncProducer(c.brokers, ProducerConfig(c.clientID))
	if err != nil {
		return nil, err
	}
	c.producer = p
	return p, nil
}

func (c *saramaConn) DropProducer() {
	if c.producer == nil {
		return
	}
	drain(c.producer)
	c.producer = nil
}

// drain closes p without waiting and reads its answers until it is done,
// as sarama requires after AsyncClose.
func drain(p sarama.AsyncProducer) {
	p.AsyncClose()
	go func() {
		for range p.Successes() {
		}
	}()
	go func() {
		for range p.Errors() {
		}
	}()
}

func (c *saramaConn) Partitions(topics []string) (map[string]int, map[string]error, error) {
	md, err := c.admin.DescribeTopics(topics)
	if err != nil {
		return nil, nil, err
	}
	counts, errs := map[string]int{}, map[string]error{}
	for _, t := range md {
		if t.Err != sarama.ErrNoError {
			errs[t.Name] = t.Err
			continue
		}
		counts[t.Name] = len(t.Partitions)
	}
	for _, t := range topics {
		if _, ok := counts[t]; !ok && errs[t] == nil {
			errs[t] = sarama.ErrUnknownTopicOrPartition
		}
	}
	return counts, errs, nil
}

func (c *saramaConn) Committed(group, topic string, partitions []int32) (map[int32]int64, error) {
	resp, err := c.admin.ListConsumerGroupOffsets(group, map[string][]int32{topic: partitions})
	if err != nil {
		return nil, err
	}
	if resp.Err != sarama.ErrNoError {
		return nil, resp.Err
	}
	out := make(map[int32]int64, len(partitions))
	for _, p := range partitions {
		out[p] = -1
		if b := resp.GetBlock(topic, p); b != nil {
			if b.Err != sarama.ErrNoError {
				return nil, fmt.Errorf("offset of %s/%d: %w", topic, p, b.Err)
			}
			out[p] = b.Offset
		}
	}
	return out, nil
}

func (c *saramaConn) Bounds(topic string, partition int32) (int64, int64, error) {
	first, err := c.client.GetOffset(topic, partition, sarama.OffsetOldest)
	if err != nil {
		return 0, 0, err
	}
	end, err := c.client.GetOffset(topic, partition, sarama.OffsetNewest)
	if err != nil {
		return 0, 0, err
	}
	return first, end, nil
}

func (c *saramaConn) Close() {
	c.DropProducer()
	_ = c.admin.Close() // closes the client too
}
