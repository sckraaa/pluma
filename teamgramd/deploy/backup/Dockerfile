# Backups (ADR 0013, docs/ops/backup.md): the MySQL image for mysqldump and a
# throwaway mysqld for the restore check, plus the static restic binary.
FROM restic/restic:0.19.1@sha256:136600b6ff6843d61d355f7f71f460a166429f35de6fd11b568fece3c9a4d510 AS restic

FROM mysql:8.0.46@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b
COPY --from=restic /usr/bin/restic /usr/local/bin/restic
COPY teamgramd/deploy/backup/backup.sh /usr/local/bin/backup.sh
ENTRYPOINT ["/usr/local/bin/backup.sh"]
CMD ["run"]
