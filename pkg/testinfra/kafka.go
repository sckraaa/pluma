package testinfra

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	kafkaPort    = "9092/tcp"
	kafkaStarter = "/tmp/testinfra-kafka-start.sh"
	kafkaTopics  = "/tmp/testinfra-kafka-topics.sh"
	// kafkaInside is the bootstrap address of tools inside the container:
	// the broker listener, advertised as localhost.
	kafkaInside = "localhost:9093"

	// TopicsScript creates the topics of the delivery path, relative to the
	// repository root (docker-compose-env.yaml runs it in kafka-init).
	TopicsScript = "teamgramd/deploy/kafka/topics.sh"
)

// Kafka is a running single-node Kafka (KRaft) broker.
type Kafka struct {
	Container *testcontainers.DockerContainer
	// Brokers are the bootstrap addresses for clients on the host. They
	// stay the same when the container is stopped and started again.
	Brokers []string
}

// StartKafka starts the production Kafka image as a single KRaft node with
// the broker settings of docker-compose-env.yaml (no topic is created on
// first use) and creates the topics with TopicsScript, as kafka-init does.
// Clients on the host reach it at Brokers; the port is fixed for the life
// of the container, so Stop and Start keep the address.
func StartKafka(ctx context.Context) (*Kafka, error) {
	root, err := RepoRoot()
	if err != nil {
		return nil, err
	}
	topics, err := os.ReadFile(filepath.Join(root, TopicsScript))
	if err != nil {
		return nil, err
	}
	hostPort, err := freePort()
	if err != nil {
		return nil, err
	}
	endpoint := "127.0.0.1:" + hostPort
	starter := fmt.Sprintf("#!/bin/bash\nexport KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://%s,BROKER://localhost:9093\nexec /etc/kafka/docker/run\n", endpoint)
	ctr, err := runService(ctx, "kafka",
		testcontainers.WithExposedPorts(kafkaPort),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.PortBindings = network.PortMap{
				network.MustParsePort(kafkaPort): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: hostPort}},
			}
		}),
		testcontainers.WithEnv(map[string]string{
			"KAFKA_NODE_ID":       "1",
			"KAFKA_PROCESS_ROLES": "broker,controller",
			// PLAINTEXT is for clients on the host (advertised with the
			// host port by the starter script), BROKER for the broker
			// itself. Kafka 3.9 advertises the controller listener too, which
			// therefore cannot be 0.0.0.0 (as in docker-compose-env.yaml).
			"KAFKA_LISTENERS":                                "PLAINTEXT://0.0.0.0:9092,BROKER://0.0.0.0:9093,CONTROLLER://localhost:9094",
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,BROKER:PLAINTEXT",
			"KAFKA_INTER_BROKER_LISTENER_NAME":               "BROKER",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9094",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
			"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                "false",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(starter),
			ContainerFilePath: kafkaStarter,
			FileMode:          0o755,
		}, testcontainers.ContainerFile{
			Reader:            strings.NewReader(string(topics)),
			ContainerFilePath: kafkaTopics,
			FileMode:          0o755,
		}),
		testcontainers.WithEntrypoint("bash"),
		testcontainers.WithCmd(kafkaStarter),
		testcontainers.WithWaitStrategyAndDeadline(2*time.Minute, wait.ForLog("Kafka Server started")),
	)
	if err != nil {
		return nil, startError("kafka", ctr, err)
	}
	k := &Kafka{Container: ctr, Brokers: []string{endpoint}}
	if err = k.exec(ctx, "bash", kafkaTopics, kafkaInside); err != nil {
		_ = ctr.Terminate(ctx)
		return nil, fmt.Errorf("testinfra: kafka topics: %w", err)
	}
	return k, nil
}

// freePort returns a port of 127.0.0.1 nothing listens on now.
func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	return port, err
}

// exec runs a command in the container and fails with its output.
func (k *Kafka) exec(ctx context.Context, cmd ...string) error {
	exitCode, out, err := k.Container.Exec(ctx, cmd, tcexec.Multiplexed())
	if err != nil {
		return err
	}
	if exitCode != 0 {
		msg, _ := io.ReadAll(out)
		return fmt.Errorf("%s exited with %d: %s", strings.Join(cmd, " "), exitCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Topics runs the Kafka CLI kafka-topics.sh with args in the container,
// e.g. to delete or alter a topic in a test.
func (k *Kafka) Topics(ctx context.Context, args ...string) error {
	return k.exec(ctx, append([]string{"/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", kafkaInside}, args...)...)
}

// Configs runs the Kafka CLI kafka-configs.sh with args in the container,
// e.g. to change a topic's parameters in a test.
func (k *Kafka) Configs(ctx context.Context, args ...string) error {
	return k.exec(ctx, append([]string{"/opt/kafka/bin/kafka-configs.sh", "--bootstrap-server", kafkaInside}, args...)...)
}

// Terminate stops and removes the container.
func (k *Kafka) Terminate(ctx context.Context) error {
	if k == nil || k.Container == nil {
		return nil
	}
	return k.Container.Terminate(ctx)
}
