package docker

import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	"github.com/moby/moby/api/types/jsonstream"
	mobyclient "github.com/moby/moby/client"
)

// fakeImagePullResponse implements mobyclient.ImagePullResponse for tests.
type fakeImagePullResponse struct {
	waitErr error
}

func (f *fakeImagePullResponse) Read(_ []byte) (int, error)                                  { return 0, io.EOF }
func (f *fakeImagePullResponse) Close() error                                                 { return nil }
func (f *fakeImagePullResponse) JSONMessages(_ context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(yield func(jsonstream.Message, error) bool) {}
}
func (f *fakeImagePullResponse) Wait(_ context.Context) error { return f.waitErr }

// fakeDockerClient implements dockerClient for unit tests.
type fakeDockerClient struct {
	inspectErr  error
	pullCalled  bool
	pullErr     error
}

func (f *fakeDockerClient) Close() error { return nil }

func (f *fakeDockerClient) ImageInspect(_ context.Context, _ string, _ ...mobyclient.ImageInspectOption) (mobyclient.ImageInspectResult, error) {
	return mobyclient.ImageInspectResult{}, f.inspectErr
}

func (f *fakeDockerClient) ImagePull(_ context.Context, _ string, _ mobyclient.ImagePullOptions) (mobyclient.ImagePullResponse, error) {
	f.pullCalled = true
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	return &fakeImagePullResponse{}, nil
}

func (f *fakeDockerClient) ContainerCreate(_ context.Context, _ mobyclient.ContainerCreateOptions) (mobyclient.ContainerCreateResult, error) {
	return mobyclient.ContainerCreateResult{}, nil
}

func (f *fakeDockerClient) ContainerStart(_ context.Context, _ string, _ mobyclient.ContainerStartOptions) (mobyclient.ContainerStartResult, error) {
	return mobyclient.ContainerStartResult{}, nil
}

func (f *fakeDockerClient) ContainerStop(_ context.Context, _ string, _ mobyclient.ContainerStopOptions) (mobyclient.ContainerStopResult, error) {
	return mobyclient.ContainerStopResult{}, nil
}

func (f *fakeDockerClient) ContainerRemove(_ context.Context, _ string, _ mobyclient.ContainerRemoveOptions) (mobyclient.ContainerRemoveResult, error) {
	return mobyclient.ContainerRemoveResult{}, nil
}

func (f *fakeDockerClient) ContainerLogs(_ context.Context, _ string, _ mobyclient.ContainerLogsOptions) (mobyclient.ContainerLogsResult, error) {
	return nil, nil
}

func TestPullImage_LocalExists(t *testing.T) {
	fake := &fakeDockerClient{inspectErr: nil}
	r := &DockerRuntime{client: fake}

	err := r.pullImage(context.Background(), "test/image:latest")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fake.pullCalled {
		t.Fatal("expected ImagePull not to be called when image exists locally")
	}
}

func TestPullImage_LocalMissing_PullSucceeds(t *testing.T) {
	fake := &fakeDockerClient{
		inspectErr: errors.New("not found"),
		pullErr:    nil,
	}
	r := &DockerRuntime{client: fake}

	err := r.pullImage(context.Background(), "test/image:latest")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !fake.pullCalled {
		t.Fatal("expected ImagePull to be called when image is missing locally")
	}
}

func TestPullImage_LocalMissing_PullFails(t *testing.T) {
	fake := &fakeDockerClient{
		inspectErr: errors.New("not found"),
		pullErr:    errors.New("registry error"),
	}
	r := &DockerRuntime{client: fake}

	err := r.pullImage(context.Background(), "test/image:latest")
	if err == nil {
		t.Fatal("expected error when pull fails, got nil")
	}
}
