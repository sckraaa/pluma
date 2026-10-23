package testinfra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/logpolicy"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

// MySQLServerArgs are the server options of the mysql service in
// docker-compose-env.yaml.
var MySQLServerArgs = []string{
	"--character-set-server=utf8mb4",
	"--collation-server=utf8mb4_unicode_ci",
	"--default-authentication-plugin=mysql_native_password",
}

const (
	// MySQLDatabase and MySQLUser are what the services connect to and as
	// (teamgramd/etc2 DSNs, MYSQL_DATABASE and MYSQL_USER in compose).
	MySQLDatabase = "teamgram"
	MySQLUser     = "teamgram"

	mysqlInitDir = "/docker-entrypoint-initdb.d/"
	// Applying the schema to an empty database takes a few seconds; pulling
	// the image on a cold cache takes longer.
	mysqlStartupTimeout = 3 * time.Minute
)

// MySQL is a running MySQL server with the schema loaded.
type MySQL struct {
	Container    *testcontainers.DockerContainer
	rootPassword string
	dsn          string
	schema       []string // file names in mysqlInitDir, in the order applied
}

type mysqlOptions struct {
	schema []string
}

// MySQLOption configures StartMySQL.
type MySQLOption func(*mysqlOptions) error

// WithSchemaFiles loads files (paths, in this order) instead of the whole
// schema, e.g. SchemaFilesBefore("migrate-x.sql") to test migration x.
func WithSchemaFiles(files []string) MySQLOption {
	return func(o *mysqlOptions) error {
		o.schema = files
		return nil
	}
}

// StartMySQL starts the production MySQL image and loads the schema (all of
// SchemaFiles unless WithSchemaFiles says otherwise) the way production does.
// The data lives in memory. Like conf.Load in the services, it applies the
// production log policy: no SQL statements or Redis commands in the logs.
func StartMySQL(ctx context.Context, opts ...MySQLOption) (*MySQL, error) {
	logpolicy.Apply()
	o := &mysqlOptions{}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}
	if o.schema == nil {
		var err error
		if o.schema, err = SchemaFiles(); err != nil {
			return nil, err
		}
	}

	// The init directory is applied in name order, so the files keep their
	// names; a name may appear once.
	files := make([]testcontainers.ContainerFile, 0, len(o.schema))
	seen := map[string]bool{}
	for _, f := range o.schema {
		name := filepath.Base(f)
		if seen[name] {
			return nil, fmt.Errorf("testinfra: schema file %s given twice", name)
		}
		seen[name] = true
		files = append(files, testcontainers.ContainerFile{
			HostFilePath:      f,
			ContainerFilePath: mysqlInitDir + name,
			FileMode:          0o644,
		})
	}

	m := &MySQL{rootPassword: randomSecret()}
	for _, f := range files {
		m.schema = append(m.schema, filepath.Base(f.ContainerFilePath))
	}
	password := randomSecret()
	ctr, err := runService(ctx, "mysql",
		testcontainers.WithExposedPorts("3306/tcp"),
		testcontainers.WithEnv(map[string]string{
			"MYSQL_ROOT_PASSWORD": m.rootPassword,
			"MYSQL_DATABASE":      MySQLDatabase,
			"MYSQL_USER":          MySQLUser,
			"MYSQL_PASSWORD":      password,
		}),
		testcontainers.WithCmd(MySQLServerArgs...),
		testcontainers.WithTmpfs(map[string]string{"/var/lib/mysql": "rw"}),
		testcontainers.WithFiles(files...),
		// The init scripts run on a temporary server without networking; the
		// final server logs its port once they are done.
		testcontainers.WithWaitStrategyAndDeadline(mysqlStartupTimeout,
			wait.ForLog("port: 3306  MySQL Community Server"),
			wait.ForListeningPort("3306/tcp"),
		),
	)
	if err != nil {
		return nil, startError("mysql", ctr, err)
	}
	m.Container = ctr

	endpoint, err := ctr.PortEndpoint(ctx, "3306/tcp", "")
	if err != nil {
		_ = ctr.Terminate(ctx)
		return nil, err
	}
	// The DSN parameters of teamgramd/etc2.
	m.dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=true", MySQLUser, password, endpoint, MySQLDatabase)
	return m, nil
}

// DSN is the data source name of the service user, with the parameters the
// services use.
func (m *MySQL) DSN() string { return m.dsn }

// Config is a sqlx config for DSN, as the services configure MySQL.
func (m *MySQL) Config() sqlx.Config {
	return sqlx.Config{
		DSN:           m.dsn,
		Active:        8,
		Idle:          4,
		IdleTimeout:   "4h",
		SlowThreshold: 500,
	}
}

// ApplySQLFile runs the SQL file at path as root on the teamgram database with
// the mysql client in the container, as the image applies its init scripts
// (e.g. a migration on a database created with SchemaFilesBefore).
func (m *MySQL) ApplySQLFile(ctx context.Context, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	target := "/tmp/testinfra-" + filepath.Base(path)
	if err = m.Container.CopyToContainer(ctx, b, target, 0o644); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	exitCode, out, err := m.Container.Exec(ctx,
		[]string{"sh", "-c", `mysql --protocol=socket -uroot --database="$0" < "$1"`, MySQLDatabase, target},
		tcexec.Multiplexed(),
		tcexec.WithEnv([]string{"MYSQL_PWD=" + m.rootPassword}),
	)
	if err != nil {
		return fmt.Errorf("apply %s: %w", path, err)
	}
	if exitCode != 0 {
		msg, _ := io.ReadAll(out)
		return fmt.Errorf("apply %s: mysql exited with %d: %s", path, exitCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Reset drops every table of the teamgram database and loads the schema
// files of StartMySQL again, in the same order: the database is as it was
// after the start. A test that changes the database for good (a migration)
// resets it first, so it does not depend on the tests before it and can run
// again (go test -count=N). The database itself stays, so pooled
// connections keep working.
func (m *MySQL) Reset(ctx context.Context) error {
	const script = `set -e
db=$0; dir=$1; shift
tables=$(mysql --protocol=socket -uroot -N -B -e "set group_concat_max_len = 1048576; select group_concat(concat(char(96), table_name, char(96))) from information_schema.tables where table_schema = '$db'")
if [ "$tables" != NULL ]; then
	mysql --protocol=socket -uroot --database="$db" -e "set foreign_key_checks = 0; drop table $tables"
fi
for f in "$@"; do
	mysql --protocol=socket -uroot --database="$db" < "$dir$f"
done`
	exitCode, out, err := m.Container.Exec(ctx,
		append([]string{"sh", "-c", script, MySQLDatabase, mysqlInitDir}, m.schema...),
		tcexec.Multiplexed(),
		tcexec.WithEnv([]string{"MYSQL_PWD=" + m.rootPassword}),
	)
	if err != nil {
		return fmt.Errorf("testinfra: reset mysql: %w", err)
	}
	if exitCode != 0 {
		msg, _ := io.ReadAll(out)
		return fmt.Errorf("testinfra: reset mysql: exited with %d: %s", exitCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Terminate stops and removes the container.
func (m *MySQL) Terminate(ctx context.Context) error {
	if m == nil || m.Container == nil {
		return nil
	}
	return m.Container.Terminate(ctx)
}

// startError adds the end of the container log to a failed start, which is
// where MySQL reports a failed init script or Kafka a bad setting.
func startError(what string, ctr *testcontainers.DockerContainer, err error) error {
	if ctr == nil {
		return fmt.Errorf("testinfra: start %s: %w", what, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var tail string
	if r, logErr := ctr.Logs(ctx); logErr == nil {
		b, _ := io.ReadAll(r)
		_ = r.Close()
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) > 30 {
			lines = lines[len(lines)-30:]
		}
		tail = strings.Join(lines, "\n")
	}
	_ = ctr.Terminate(ctx)
	return errors.Join(fmt.Errorf("testinfra: start %s: %w", what, err), fmt.Errorf("container log (tail):\n%s", tail))
}
