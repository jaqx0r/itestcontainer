//go:build linux

package containerd

import (
	"context"
	"errors"
	"testing"

	containerdclient "github.com/containerd/containerd/v2/client"
)

// fakeContainerdClient implements containerdClient for unit tests.
type fakeContainerdClient struct {
	getImageErr error
	pullCalled  bool
	pullErr     error
}

func (f *fakeContainerdClient) GetImage(_ context.Context, _ string) (containerdclient.Image, error) {
	if f.getImageErr != nil {
		return nil, f.getImageErr
	}
	return nil, nil
}

func (f *fakeContainerdClient) Pull(_ context.Context, _ string, _ ...containerdclient.RemoteOpt) (containerdclient.Image, error) {
	f.pullCalled = true
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	return nil, nil
}

func (f *fakeContainerdClient) NewContainer(_ context.Context, _ string, _ ...containerdclient.NewContainerOpts) (containerdclient.Container, error) {
	return nil, nil
}

func (f *fakeContainerdClient) Close() error { return nil }

func TestGetImage_LocalExists(t *testing.T) {
	fake := &fakeContainerdClient{getImageErr: nil}
	r := &ContainerdRuntime{client: fake}

	_, err := r.pullImage(context.Background(), "test/image:latest")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fake.pullCalled {
		t.Fatal("expected Pull not to be called when image exists locally")
	}
}

func TestGetImage_LocalMissing_PullSucceeds(t *testing.T) {
	fake := &fakeContainerdClient{
		getImageErr: errors.New("not found"),
		pullErr:     nil,
	}
	r := &ContainerdRuntime{client: fake}

	_, err := r.pullImage(context.Background(), "test/image:latest")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !fake.pullCalled {
		t.Fatal("expected Pull to be called when image is missing locally")
	}
}

func TestGetImage_LocalMissing_PullFails(t *testing.T) {
	fake := &fakeContainerdClient{
		getImageErr: errors.New("not found"),
		pullErr:     errors.New("registry error"),
	}
	r := &ContainerdRuntime{client: fake}

	_, err := r.pullImage(context.Background(), "test/image:latest")
	if err == nil {
		t.Fatal("expected error when pull fails, got nil")
	}
}
