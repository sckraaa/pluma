package testinfra

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// Applying the schema to an empty database takes most of a MySQL start (the
// image initializes the datadir, loads the time zones and runs every init
// script on a temporary server, then starts the server again). StartMySQL
// does that once per schema: it keeps the initialized datadir in a local
// image, a schema image, and starts each server from a copy of it.
const (
	// mysqlImageRepo is the repository of the schema images; the tag is the
	// key of what the image was built from (mysqlImageKey).
	mysqlImageRepo = "pluma-testinfra-mysql"
	// mysqlImageLabel marks the schema images (value mysqlImageValue), the
	// only images testinfra removes, and the containers that build them
	// (mysqlBuildValue).
	mysqlImageLabel = "pluma.testinfra"
	mysqlImageValue = "mysql-schema"
	mysqlBuildValue = "mysql-schema-build"

	// mysqlImageVersion changes when the way a schema image is made changes
	// in a way the key does not see.
	mysqlImageVersion = "1"
	// mysqlSeed is the initialized datadir in a schema image, a tar
	// compressed with zstd: its 200 MB, mostly the preallocated redo log and
	// tablespaces, take 6 MB. It is outside /var/lib/mysql, a VOLUME of the
	// base image, which docker commit does not capture.
	mysqlSeed = "/var/lib/mysql-seed.tar.zst"

	mysqlBuildDone   = "testinfra: datadir saved"
	mysqlBuildFailed = "testinfra: datadir not saved"

	// keepMySQLImages is how many schema images stay on the Docker host,
	// the last used ones. A run of the integration tests uses about seven
	// (the whole schema, an empty database, the states before the tested
	// migrations), so a run finds what the run before it built, also across
	// branches; an edit of the schema builds a new image, and the oldest
	// goes.
	keepMySQLImages = 12
)

// mysqlBuildScript is the entrypoint of the container that builds a schema
// image. It runs the image's own entrypoint, which initializes the datadir
// and applies the init directory as the first start of production does,
// waits for the final server (the temporary one of the init does not listen
// on TCP), stops it cleanly and saves the datadir to mysqlSeed. Then it
// sleeps while the image is committed. The container removes itself when it
// ends (AutoRemove), also if the test binary that started it is gone.
const mysqlBuildScript = `set -o pipefail
docker-entrypoint.sh "$@" &
pid=$!
n=0
until MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqladmin --protocol=TCP -h127.0.0.1 -uroot ping >/dev/null 2>&1; do
	n=$((n + 1))
	if ! kill -0 "$pid" 2>/dev/null || [ "$n" -gt 1500 ]; then
		kill "$pid" 2>/dev/null
		echo "` + mysqlBuildFailed + `"
		exec sleep 600
	fi
	sleep 0.2
done
if kill "$pid" && wait "$pid" && tar -C /var/lib/mysql -cf - . | zstd -q -o ` + mysqlSeed + `; then
	echo "` + mysqlBuildDone + `"
else
	echo "` + mysqlBuildFailed + `"
fi
exec sleep 600`

// mysqlSeedScript is the entrypoint of a schema image: it unpacks the saved
// datadir to /var/lib/mysql (a tmpfs in StartMySQL), unless a datadir is
// there, and runs the image's own entrypoint, which finds the database and
// starts the server.
const mysqlSeedScript = `set -o pipefail
[ -d /var/lib/mysql/mysql ] || zstd -dcq ` + mysqlSeed + ` | tar -C /var/lib/mysql -xpf - || exit
exec docker-entrypoint.sh "$@"`

// schemaFile is an init script: its name in mysqlInitDir and its content.
type schemaFile struct {
	name string
	data []byte
}

// readSchemaFiles reads the files at paths, in that order. The init directory
// is applied in name order, so the files keep their names; a name may
// appear once.
func readSchemaFiles(paths []string) ([]schemaFile, error) {
	files := make([]schemaFile, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		name := filepath.Base(p)
		if seen[name] {
			return nil, fmt.Errorf("testinfra: schema file %s given twice", name)
		}
		seen[name] = true
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		files = append(files, schemaFile{name: name, data: b})
	}
	return files, nil
}

// mysqlImageKey identifies a schema image by all that shapes the datadir it
// holds: the base image, the server options, the database and user the
// init creates, the init scripts (names, order, contents) and the scripts
// that build and start the image. The passwords are not part of it:
// StartMySQL sets its own at each start.
func mysqlImageKey(base string, schema []schemaFile) string {
	recipe := struct {
		Version, Base       string
		Args                []string
		Database, User      string
		BuildScript, Script string
		Files               [][2]string
	}{
		Version: mysqlImageVersion, Base: base,
		Args:     MySQLServerArgs,
		Database: MySQLDatabase, User: MySQLUser,
		BuildScript: mysqlBuildScript, Script: mysqlSeedScript,
	}
	for _, f := range schema {
		sum := sha256.Sum256(f.data)
		recipe.Files = append(recipe.Files, [2]string{f.name, hex.EncodeToString(sum[:])})
	}
	b, err := json.Marshal(recipe)
	if err != nil {
		panic(fmt.Sprintf("testinfra: mysql image key: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// mysqlSchemaImage returns the schema image of schema, building it if the
// Docker host does not have it.
func mysqlSchemaImage(ctx context.Context, schema []schemaFile) (string, error) {
	base, err := ComposeImage("mysql")
	if err != nil {
		return "", err
	}
	key := mysqlImageKey(base, schema)
	tag := mysqlImageRepo + ":" + key

	dir := mysqlImageStateDir()
	unlock, err := lockMySQLImage(ctx, filepath.Join(dir, "mysql-"+key))
	if err != nil {
		return "", fmt.Errorf("testinfra: lock mysql image %s: %w", tag, err)
	}
	defer unlock()

	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return "", err
	}
	defer cli.Close()
	if _, err = cli.ImageInspect(ctx, tag); err == nil {
		return tag, nil
	} else if !cerrdefs.IsNotFound(err) {
		return "", err
	}

	if err = buildMySQLImage(ctx, cli, base, tag, schema); err != nil {
		return "", err
	}
	pruneMySQLImages(ctx, cli, dir)
	return tag, nil
}

// mysqlImageStateDir is where the build locks of the schema images are: the
// user cache directory, else the temporary one. A lock file's mtime is the
// last use of its image, for pruneMySQLImages.
func mysqlImageStateDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(dir, "pluma-testinfra")
		if err = os.MkdirAll(dir, 0o750); err == nil {
			return dir
		}
	}
	dir := filepath.Join(os.TempDir(), "pluma-testinfra")
	_ = os.MkdirAll(dir, 0o750)
	return dir
}

// lockMySQLImage takes the build lock of a schema image, the file at path:
// go test runs the packages in parallel, and the first binary that needs an
// image builds it while the others wait and then use it. It marks the image
// as used. Without a lock (no flock, or a Docker host shared with another
// machine) two binaries may build the same image: the later commit takes the
// tag, and pruneMySQLImages removes the untagged copy.
func lockMySQLImage(ctx context.Context, path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(ctx, f); err != nil {
		_ = f.Close()
		return nil, err
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	// Closing the file releases the lock.
	return func() { _ = f.Close() }, nil
}

// buildMySQLImage builds tag, the schema image of schema on base:
// it starts base with schema in the init directory under mysqlBuildScript,
// and commits the container once the script saved the datadir.
//
// The container is created with the Docker client, not testcontainers:
// docker commit copies the container's labels to the image, and the reaper
// removes the images that carry the labels of its session.
func buildMySQLImage(ctx context.Context, cli *testcontainers.DockerClient, base, tag string, schema []schemaFile) error {
	if err := pullMissing(ctx, cli, base); err != nil {
		return fmt.Errorf("testinfra: pull %s: %w", base, err)
	}
	start := time.Now()
	log.Printf("testinfra: building %s from %s with %d schema files", tag, base, len(schema))
	initCtx, cancel := context.WithTimeout(ctx, mysqlStartupTimeout)
	defer cancel()

	// The passwords of the build are thrown away: the users keep them in the
	// saved datadir, and StartMySQL replaces them at each start.
	created, err := cli.ContainerCreate(initCtx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      base,
			Entrypoint: []string{"bash", "-c", mysqlBuildScript, "bash"},
			Cmd:        MySQLServerArgs,
			Env: []string{
				"MYSQL_ROOT_PASSWORD=" + randomSecret(),
				"MYSQL_DATABASE=" + MySQLDatabase,
				"MYSQL_USER=" + MySQLUser,
				"MYSQL_PASSWORD=" + randomSecret(),
			},
			Labels: map[string]string{mysqlImageLabel: mysqlBuildValue},
		},
		HostConfig: &container.HostConfig{
			AutoRemove: true,
			// As in StartMySQL: the init runs in memory.
			Tmpfs: map[string]string{"/var/lib/mysql": "rw"},
		},
	})
	if err != nil {
		return fmt.Errorf("testinfra: build mysql image: %w", err)
	}
	defer func() {
		// Running, it sleeps: stop it, and it removes itself.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_, _ = cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	}()

	if len(schema) > 0 {
		if _, err = cli.CopyToContainer(initCtx, created.ID, client.CopyToContainerOptions{
			DestinationPath: "/",
			Content:         initDirArchive(schema),
		}); err != nil {
			return fmt.Errorf("testinfra: build mysql image: copy the schema: %w", err)
		}
	}
	if _, err = cli.ContainerStart(initCtx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("testinfra: build mysql image: %w", err)
	}
	if err = waitMySQLSeed(initCtx, cli, created.ID); err != nil {
		return err
	}
	saved := time.Now()
	commitCtx, cancel := context.WithTimeout(ctx, mysqlStartupTimeout)
	defer cancel()
	_, err = cli.ContainerCommit(commitCtx, created.ID, client.ContainerCommitOptions{
		Reference: tag,
		Config: &container.Config{
			Entrypoint: []string{"bash", "-c", mysqlSeedScript, "bash"},
			Cmd:        []string{"mysqld"},
			// The rest of the environment comes from the container; the
			// build's passwords stay out of the image.
			Env:    []string{"MYSQL_ROOT_PASSWORD=", "MYSQL_PASSWORD="},
			Labels: map[string]string{mysqlImageLabel: mysqlImageValue},
		},
	})
	if err != nil {
		return fmt.Errorf("testinfra: build mysql image: commit: %w", err)
	}
	log.Printf("testinfra: built %s in %v (the commit %v)", tag,
		time.Since(start).Round(100*time.Millisecond), time.Since(saved).Round(100*time.Millisecond))
	return nil
}

// initDirArchive is a tar of schema in mysqlInitDir, to copy to "/".
func initDirArchive(schema []schemaFile) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Now()
	for _, f := range schema {
		// Writes to a bytes.Buffer do not fail; a header is checked
		// against the size written, which is its own.
		_ = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     strings.TrimPrefix(mysqlInitDir, "/") + f.name,
			Mode:     0o644,
			Size:     int64(len(f.data)),
			ModTime:  now,
		})
		_, _ = tw.Write(f.data)
	}
	_ = tw.Close()
	return &buf
}

// waitMySQLSeed follows the log of the build container until mysqlBuildScript
// reports the datadir saved. If it reports a failure, ends or times out, the
// error has the end of the log, where MySQL reports a failed init script.
func waitMySQLSeed(ctx context.Context, cli *testcontainers.DockerClient, id string) error {
	logs, err := cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Follow: true})
	if err != nil {
		return fmt.Errorf("testinfra: build mysql image: %w", err)
	}
	defer logs.Close()
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, logs)
		pw.CloseWithError(err)
	}()

	var tail []string
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == mysqlBuildDone {
			return nil
		}
		if line == mysqlBuildFailed {
			err = errors.New("the init failed")
			break
		}
		tail = append(tail, line)
		if len(tail) > 30 {
			tail = tail[1:]
		}
	}
	switch {
	case err != nil:
	case ctx.Err() != nil:
		err = ctx.Err()
	case sc.Err() != nil:
		err = sc.Err()
	default:
		err = errors.New("the container ended")
	}
	return errors.Join(fmt.Errorf("testinfra: build mysql image: %w", err), fmt.Errorf("container log (tail):\n%s", strings.Join(tail, "\n")))
}

// pruneMySQLImages removes the schema images that staleMySQLImages finds,
// and their lock files. An image a container runs stays: the Docker host
// refuses to remove it. Errors are ignored; the next build prunes again.
func pruneMySQLImages(ctx context.Context, cli *testcontainers.DockerClient, dir string) {
	list, err := cli.ImageList(ctx, client.ImageListOptions{
		Filters: make(client.Filters).Add("label", mysqlImageLabel+"="+mysqlImageValue),
	})
	if err != nil {
		return
	}
	for _, ref := range staleMySQLImages(list.Items, dir, keepMySQLImages) {
		if _, err = cli.ImageRemove(ctx, ref, client.ImageRemoveOptions{}); err != nil {
			continue
		}
		if key, ok := strings.CutPrefix(ref, mysqlImageRepo+":"); ok {
			_ = os.Remove(filepath.Join(dir, "mysql-"+key))
		}
	}
}

// staleMySQLImages returns the schema images of images to remove, by tag or,
// untagged, by ID: the tagged ones beyond the keep last used (the mtime of
// the lock file in dir, else the creation), and the untagged ones (see
// lockMySQLImage). An image without the schema label, or tagged also
// elsewhere than mysqlImageRepo, is not testinfra's to remove.
func staleMySQLImages(images []image.Summary, dir string, keep int) []string {
	type schemaImage struct {
		tag  string
		used time.Time
	}
	var stale []string
	tagged := make([]schemaImage, 0, len(images))
next:
	for _, item := range images {
		if item.Labels[mysqlImageLabel] != mysqlImageValue {
			continue
		}
		var tags []string
		for _, tag := range item.RepoTags {
			if tag == "<none>:<none>" {
				continue
			}
			if !strings.HasPrefix(tag, mysqlImageRepo+":") {
				continue next
			}
			tags = append(tags, tag)
		}
		if len(tags) == 0 {
			stale = append(stale, item.ID)
			continue
		}
		for _, tag := range tags {
			img := schemaImage{tag: tag, used: time.Unix(item.Created, 0)}
			if fi, err := os.Stat(filepath.Join(dir, "mysql-"+strings.TrimPrefix(tag, mysqlImageRepo+":"))); err == nil {
				img.used = fi.ModTime()
			}
			tagged = append(tagged, img)
		}
	}
	slices.SortStableFunc(tagged, func(a, b schemaImage) int { return b.used.Compare(a.used) })
	for i, img := range tagged {
		if i >= keep {
			stale = append(stale, img.tag)
		}
	}
	return stale
}
