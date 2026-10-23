package testinfra

import (
	"context"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// Pause freezes the processes of a container (docker pause): its ports
// stay open and its connections stay up, but nothing answers, as with a
// hung server.
func Pause(ctx context.Context, ctr *testcontainers.DockerContainer) error {
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ContainerPause(ctx, ctr.GetContainerID(), client.ContainerPauseOptions{})
	return err
}

// Unpause resumes a container Pause froze.
func Unpause(ctx context.Context, ctr *testcontainers.DockerContainer) error {
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ContainerUnpause(ctx, ctr.GetContainerID(), client.ContainerUnpauseOptions{})
	return err
}
