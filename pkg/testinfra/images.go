package testinfra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"gopkg.in/yaml.v3"
)

// ComposeFile is the compose file of the infrastructure, relative to the
// repository root. testinfra runs the images it names, so the tests run what
// production runs, and an image update there (Dependabot, docs/ops/testing.md)
// reaches the tests by itself.
const ComposeFile = "docker-compose-env.yaml"

// composeFile is the part of ComposeFile testinfra reads.
type composeFile struct {
	Services map[string]struct {
		Image       string `yaml:"image"`
		Command     any    `yaml:"command"`
		Environment any    `yaml:"environment"` // a map or a list of NAME=value
	} `yaml:"services"`
}

func readCompose() (*composeFile, error) {
	root, err := RepoRoot()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(root, ComposeFile))
	if err != nil {
		return nil, err
	}
	var c composeFile
	if err = yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("testinfra: %s: %w", ComposeFile, err)
	}
	return &c, nil
}

// pinnedImage matches an image reference pinned by tag and digest.
var pinnedImage = regexp.MustCompile(`^[^@\s]+:[^@:/\s]+@sha256:[0-9a-f]{64}$`)

// ComposeImage returns the image of service in ComposeFile. It must be pinned
// by tag and digest.
func ComposeImage(service string) (string, error) {
	c, err := readCompose()
	if err != nil {
		return "", err
	}
	s, ok := c.Services[service]
	if !ok {
		return "", fmt.Errorf("testinfra: %s has no service %s", ComposeFile, service)
	}
	if !pinnedImage.MatchString(s.Image) {
		return "", fmt.Errorf("testinfra: %s service %s image %q is not pinned as name:tag@sha256:digest", ComposeFile, service, s.Image)
	}
	return s.Image, nil
}

// RyukImage is the reaper that testcontainers-go starts next to the test
// containers (testcontainers.ReaperDefaultImage), pinned by digest. The
// reaper mounts the Docker socket, so whoever controls its image controls the
// Docker host. testcontainers-go starts it by tag and pulls the tag only when
// it is missing locally; pinReaper makes the local tag point at this digest
// first. When a testcontainers-go update changes the reaper image,
// TestRyukImagePinned fails until this is the digest of the new one.
const RyukImage = "testcontainers/ryuk:0.14.0@sha256:7c1a8a9a47c780ed0f983770a662f80deb115d95cce3e2daa3d12115b8cd28f0"

// reaperRefs returns RyukImage by digest and the tag testcontainers-go starts,
// or an error if RyukImage is not that image.
func reaperRefs() (byDigest, tag string, err error) {
	//nolint:staticcheck // SA1019: the only exported name of the reaper image testcontainers-go starts; the pin must follow it
	want := testcontainers.ReaperDefaultImage
	name, digest, ok := strings.Cut(RyukImage, "@")
	if !ok || name != want {
		return "", "", fmt.Errorf("testinfra: RyukImage is %s, but testcontainers-go starts %s: pin the digest of that image", RyukImage, want)
	}
	repo := name[:strings.LastIndex(name, ":")]
	return repo + "@" + digest, name, nil
}

var (
	reaperOnce sync.Once
	reaperErr  error
)

// pinReaper pulls RyukImage by digest, unless the Docker host has it, and
// tags it as the image testcontainers-go starts. It runs once per process.
func pinReaper(ctx context.Context) error {
	reaperOnce.Do(func() {
		reaperErr = tagReaper(ctx)
		if reaperErr != nil {
			reaperErr = fmt.Errorf("testinfra: pin the reaper image: %w", reaperErr)
		}
	})
	return reaperErr
}

func tagReaper(ctx context.Context) error {
	byDigest, tag, err := reaperRefs()
	if err != nil {
		return err
	}
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	if err = pullMissing(ctx, cli, byDigest); err != nil {
		return err
	}
	_, err = cli.ImageTag(ctx, client.ImageTagOptions{Source: byDigest, Target: tag})
	return err
}

// pullMissing pulls ref unless the Docker host has it.
func pullMissing(ctx context.Context, cli *testcontainers.DockerClient, ref string) error {
	_, err := cli.ImageInspect(ctx, ref)
	if !cerrdefs.IsNotFound(err) {
		return err
	}
	pull, err := cli.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer pull.Close()
	return pull.Wait(ctx)
}

// runService starts the image of the compose service with opts.
func runService(ctx context.Context, service string, opts ...testcontainers.ContainerCustomizer) (*testcontainers.DockerContainer, error) {
	image, err := ComposeImage(service)
	if err != nil {
		return nil, err
	}
	return runImage(ctx, image, opts...)
}

// runImage starts image with opts, watched by the pinned reaper.
func runImage(ctx context.Context, image string, opts ...testcontainers.ContainerCustomizer) (*testcontainers.DockerContainer, error) {
	if err := pinReaper(ctx); err != nil {
		return nil, err
	}
	return testcontainers.Run(ctx, image, opts...)
}
