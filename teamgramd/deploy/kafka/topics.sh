#!/bin/bash
# Creates the Kafka topics of the delivery path (ADR 0012, p. 4) and sets
# their parameters. The broker creates no topic by itself
# (KAFKA_AUTO_CREATE_TOPICS_ENABLE=false): this script is the only place
# that does, for the kafka-init service of docker-compose-env.yaml, for an
# install without Docker (docs/ops) and for the integration tests
# (pkg/testinfra.StartKafka).
#
#   topics.sh [bootstrap-server]     default localhost:9092
#
# Safe to run again: an existing topic keeps its data and gets the
# parameters below. A topic with another partition count is left alone and
# the script fails: changing it moves keys between partitions and needs the
# procedure of docs/ops/user-state.md.
set -euo pipefail

BOOTSTRAP=${1:-localhost:9092}
BIN=${KAFKA_BIN:-/opt/kafka/bin}

# name partitions configs
TOPICS=(
	"Inbox-T 16 retention.ms=604800000,min.insync.replicas=1"
	"Sync-T 16 retention.ms=86400000,min.insync.replicas=1,max.message.bytes=8388608"
	"BotSync 16 retention.ms=86400000,min.insync.replicas=1,max.message.bytes=8388608"
)

for entry in "${TOPICS[@]}"; do
	read -r name partitions configs <<<"$entry"
	"$BIN/kafka-topics.sh" --bootstrap-server "$BOOTSTRAP" --create --if-not-exists \
		--topic "$name" --partitions "$partitions" --replication-factor 1
	"$BIN/kafka-configs.sh" --bootstrap-server "$BOOTSTRAP" --alter \
		--entity-type topics --entity-name "$name" --add-config "$configs"
done

# Every topic must have its partition count: the relay publishes only to a
# topic that has.
described=$("$BIN/kafka-topics.sh" --bootstrap-server "$BOOTSTRAP" --describe \
	--topic "$(printf '%s\n' "${TOPICS[@]}" | awk '{ print $1 }' | paste -sd '|' -)")
failed=0
for entry in "${TOPICS[@]}"; do
	read -r name partitions _ <<<"$entry"
	actual=$(awk -v t="$name" '$1 == "Topic:" && $2 == t { for (i = 1; i < NF; i++) if ($i == "PartitionCount:") print $(i + 1) }' <<<"$described")
	if [[ $actual != "$partitions" ]]; then
		echo "topics.sh: $name has ${actual:-no} partitions, want $partitions; see docs/ops/user-state.md" >&2
		failed=1
	fi
done
if [[ $failed != 0 ]]; then
	exit 1
fi
echo "topics.sh: ${#TOPICS[@]} topics ready"
