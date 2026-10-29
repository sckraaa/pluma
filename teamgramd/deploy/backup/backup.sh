#!/usr/bin/env bash
# Backups of Pluma (ADR 0013, docs/ops/backup.md): a consistent MySQL dump and
# the object storage files, encrypted by restic and sent to an S3-compatible
# store outside the server; once a week the latest dump is restored into a
# throwaway MySQL and checked. Results go to node-exporter's textfile
# collector as backup_* metrics (alerts BackupMissing, RestoreCheckFailed).
#
#   backup.sh run                   daily loop (the compose service)
#   backup.sh now                   one backup
#   backup.sh check-restore         restore the latest dump into a throwaway MySQL
#   backup.sh snapshots             list snapshots
#   backup.sh restore-mysql [ID]    load a dump (default: latest) into the live MySQL
#   backup.sh restore-files DIR [ID] restore the files snapshot into DIR
#   backup.sh restore-secrets DIR [ID] restore the server keys into DIR

set -euo pipefail

: "${RESTIC_REPOSITORY:?set RESTIC_REPOSITORY}"
: "${RESTIC_PASSWORD:?set RESTIC_PASSWORD}"
: "${MYSQL_ROOT_PASSWORD:?set MYSQL_ROOT_PASSWORD}"
MYSQL_HOST=${MYSQL_HOST:-mysql}
DATABASE=${MYSQL_DATABASE:-teamgram}
FILES_DIR=${FILES_DIR:-/data/files}
SECRETS_DIR=${SECRETS_DIR:-/data/secrets} # the server RSA key: clients pin it
FILES_EXCLUDE=${FILES_EXCLUDE:-}             # a pattern of files not to copy
TEXTFILE_DIR=${TEXTFILE_DIR:-/textfile}
BACKUP_AT=${BACKUP_AT:-03:30}          # UTC, HH:MM
RESTORE_CHECK_DAY=${RESTORE_CHECK_DAY:-7} # ISO weekday, 7 = Sunday
HOST_TAG=pluma
DUMP_NAME="mysql/$DATABASE.sql"

log() { printf '%s backup: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }

# metric NAME VALUE writes one gauge atomically for the textfile collector.
metric() {
	local file="$TEXTFILE_DIR/$1.prom"
	mkdir -p "$TEXTFILE_DIR"
	printf '# TYPE %s gauge\n%s %s\n' "$1" "$1" "$2" >"$file.tmp"
	mv "$file.tmp" "$file"
}

# ensure_repo creates the repository (and its bucket) when it does not exist.
# restic init answers at once when it does; checking with cat config would
# retry for minutes while the bucket is missing.
ensure_repo() {
	local out
	if out=$(restic init 2>&1); then
		log "repository initialised"
		return 0
	fi
	if [[ $out == *"already initialized"* || $out == *"already exists"* ]]; then
		return 0
	fi
	log "repository: $out"
	return 1
}

backup_now() {
	local start
	start=$(date +%s)
	ensure_repo
	log "dumping MySQL $DATABASE"
	# --stdin-from-command: a failing mysqldump leaves no snapshot behind.
	MYSQL_PWD=$MYSQL_ROOT_PASSWORD restic backup --host "$HOST_TAG" --tag mysql \
		--stdin-filename "$DUMP_NAME" --stdin-from-command -- \
		mysqldump -h "$MYSQL_HOST" -uroot --single-transaction --quick \
		--routines --triggers --events --hex-blob --set-gtid-purged=OFF \
		--databases "$DATABASE"
	if [[ -d $FILES_DIR ]]; then
		log "backing up files of $FILES_DIR"
		local exclude=()
		[[ -n $FILES_EXCLUDE ]] && exclude=(--exclude "$FILES_EXCLUDE")
		restic backup --host "$HOST_TAG" --tag files "${exclude[@]}" "$FILES_DIR"
	else
		log "no $FILES_DIR: files skipped"
	fi
	if [[ -d $SECRETS_DIR ]]; then
		log "backing up the server keys"
		restic backup --host "$HOST_TAG" --tag secrets "$SECRETS_DIR"
	else
		log "no $SECRETS_DIR: server keys skipped"
	fi
	metric backup_last_success_timestamp_seconds "$(date +%s)"
	metric backup_last_duration_seconds "$(($(date +%s) - start))"
	log "done in $(($(date +%s) - start))s"
}

# The throwaway mysqld of the restore check and its data directory: globals,
# as the exit trap runs after check_restore's locals are gone.
restore_pid=
restore_dir=/var/tmp/restore-check

stop_restore_mysqld() {
	if [[ -n $restore_pid ]]; then
		kill "$restore_pid" 2>/dev/null || true
		wait "$restore_pid" 2>/dev/null || true
		restore_pid=
	fi
	rm -rf "$restore_dir"
}

# check_restore loads the latest dump into a throwaway mysqld and checks it,
# then verifies part of the repository data. The mysqld is stopped when it
# returns or, when set -e stops the process, on exit.
check_restore() {
	local dir=$restore_dir sock=/var/tmp/restore-check.sock users history
	rm -rf "$dir" && mkdir -p "$dir" && chown mysql:mysql "$dir"
	mysqld --initialize-insecure --user=mysql --datadir="$dir" >/dev/null 2>&1
	mysqld --user=mysql --datadir="$dir" --socket="$sock" --skip-networking \
		--mysqlx=OFF --pid-file="$dir/mysqld.pid" >/var/tmp/restore-check.log 2>&1 &
	restore_pid=$!
	trap stop_restore_mysqld RETURN EXIT
	for _ in $(seq 60); do
		mysqladmin --socket="$sock" -uroot ping >/dev/null 2>&1 && break
		sleep 1
	done
	log "restoring the latest dump into a throwaway MySQL"
	restic dump --host "$HOST_TAG" --tag mysql latest "$DUMP_NAME" | mysql --socket="$sock" -uroot
	users=$(mysql --socket="$sock" -uroot -N -B -e "SELECT COUNT(*) FROM \`$DATABASE\`.users")
	history=$(mysql --socket="$sock" -uroot -N -B -e "SELECT COUNT(*) FROM \`$DATABASE\`.schema_migrations")
	if ((users < 1 || history < 1)); then
		log "restored dump looks empty: $users users, $history migrations"
		return 1
	fi
	log "restored dump: $users users, $history migrations"
	restic check --read-data-subset=5%
	metric backup_last_restore_check_timestamp_seconds "$(date +%s)"
}

prune() {
	restic forget --host "$HOST_TAG" --group-by host,tags \
		--keep-daily 14 --keep-weekly 8 --keep-monthly 12 --prune
}

# seconds_until HH:MM (UTC)
seconds_until() {
	local now target
	now=$(date -u +%s)
	target=$(date -u -d "today $1" +%s)
	((target <= now)) && target=$(date -u -d "tomorrow $1" +%s)
	echo $((target - now))
}

# Each step runs as a process of its own, where set -e stops it at the first
# failure. Bash ignores set -e inside a function called from a || or &&
# list: a backup whose restic failed would go on and record its success, and
# BackupMissing would never fire.
run_loop() {
	log "daily at $BACKUP_AT UTC, restore check on weekday $RESTORE_CHECK_DAY"
	if [[ ! -f $TEXTFILE_DIR/backup_last_success_timestamp_seconds.prom ]]; then
		"$0" now || log "first backup failed"
	fi
	while :; do
		sleep "$(seconds_until "$BACKUP_AT")"
		"$0" now || {
			log "backup failed"
			continue
		}
		if [[ $(date -u +%u) == "$RESTORE_CHECK_DAY" ]]; then
			"$0" check-restore || log "restore check failed"
			"$0" prune || log "prune failed"
		fi
	done
}

case ${1:-run} in
run) run_loop ;;
now) backup_now ;;
check-restore)
	ensure_repo
	check_restore
	;;
prune) prune ;;
snapshots) restic snapshots ;;
restore-mysql)
	log "loading snapshot ${2:-latest} into $MYSQL_HOST"
	restic dump --host "$HOST_TAG" --tag mysql "${2:-latest}" "$DUMP_NAME" |
		MYSQL_PWD=$MYSQL_ROOT_PASSWORD mysql -h "$MYSQL_HOST" -uroot
	;;
restore-files)
	: "${2:?restore-files DIR [ID]}"
	restic restore --host "$HOST_TAG" --tag files "${3:-latest}:$FILES_DIR" --target "$2"
	;;
restore-secrets)
	: "${2:?restore-secrets DIR [ID]}"
	restic restore --host "$HOST_TAG" --tag secrets "${3:-latest}:$SECRETS_DIR" --target "$2"
	;;
*)
	echo "usage: backup.sh run|now|check-restore|prune|snapshots|restore-mysql [ID]|restore-files DIR [ID]|restore-secrets DIR [ID]" >&2
	exit 2
	;;
esac
