package testinfra

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	kafkaPort    = "9092/tcp"
	kafkaStarter = "/tmp/testinfra-kafka-start.sh"
)

// Kafka is a running single-node Kafka (KRaft) broker.
type Kafka struct {
	Container *testcontainers.DockerContainer
	// Brokers are the bootstrap addresses for clients on the host.
	Brokers []string
}

// StartKafka starts the production Kafka image as a single KRaft node with the
// broker settings of docker-compose-env.yaml (topics created on first use
// with 3 partitions). Clients on the host reach it at Brokers.
func StartKafka(ctx context.Context) (*Kafka, error) {
	ctr, err := runService(ctx, "kafka",
		testcontainers.WithExposedPorts(kafkaPort),
		testcontainers.WithEnv(map[string]string{
			"KAFKA_NODE_ID":       "1",
			"KAFKA_PROCESS_ROLES": "broker,controller",
			// PLAINTEXT is for clients on the host (advertised with the
			// mapped port by the starter script), BROKER for the broker
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
			"KAFKA_NUM_PARTITIONS":                           "3",
		}),
		// The advertised address needs the mapped port, known only once the
		// container runs: wait for the starter script, which a hook writes.
		testcontainers.WithEntrypoint("sh"),
		testcontainers.WithCmd("-c", "while [ ! -f "+kafkaStarter+" ]; do sleep 0.1; done; exec bash "+kafkaStarter),
		testcontainers.WithLifecycleHooks(testcontainers.ContainerLifecycleHooks{
			PostStarts: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					if err := wait.ForMappedPort(kafkaPort).WaitUntilReady(ctx, c); err != nil {
						return err
					}
					endpoint, err := c.PortEndpoint(ctx, kafkaPort, "")
					if err != nil {
						return err
					}
					script := fmt.Sprintf("#!/bin/bash\nexport KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://%s,BROKER://localhost:9093\nexec /etc/kafka/docker/run\n", endpoint)
					if err = c.CopyToContainer(ctx, []byte(script), kafkaStarter, 0o755); err != nil {
						return err
					}
					return wait.ForLog("Kafka Server started").WithStartupTimeout(2*time.Minute).WaitUntilReady(ctx, c)
				},
			},
		}),
	)
	if err != nil {
		return nil, startError("kafka", ctr, err)
	}
	endpoint, err := ctr.PortEndpoint(ctx, kafkaPort, "")
	if err != nil {
		_ = ctr.Terminate(ctx)
		return nil, err
	}
	return &Kafka{Container: ctr, Brokers: []string{endpoint}}, nil
}

// Terminate stops and removes the container.
func (k *Kafka) Terminate(ctx context.Context) error {
	if k == nil || k.Container == nil {
		return nil
	}
	return k.Container.Terminate(ctx)
}
