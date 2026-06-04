package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type Client struct {
	raw *client.Client
}

func New() (*Client, error) {
	raw, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{raw: raw}, nil
}

func NewWithHost(host string) (*Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if strings.TrimSpace(host) != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	raw, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{raw: raw}, nil
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("image reference is required")
	}

	authConfig := registry.AuthConfig{}
	encoded, _ := json.Marshal(authConfig)
	reader, err := c.raw.ImagePull(ctx, ref, image.PullOptions{RegistryAuth: base64.URLEncoding.EncodeToString(encoded)})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("read pull stream for %q: %w", ref, err)
	}
	return nil
}

func (c *Client) BuildImage(ctx context.Context, contextDir, tag string) error {
	if strings.TrimSpace(contextDir) == "" {
		return fmt.Errorf("build context is required")
	}
	if strings.TrimSpace(tag) == "" {
		return fmt.Errorf("image tag is required")
	}

	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	err := filepath.Walk(contextDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(contextDir, path)
		if err != nil || rel == "." {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := io.Copy(tw, file); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("prepare build context: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close build context tar: %w", err)
	}

	resp, err := c.raw.ImageBuild(ctx, buf, types.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("build image %q: %w", tag, err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("read build stream for %q: %w", tag, err)
	}
	return nil
}

func (c *Client) EnsureVolume(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := c.raw.VolumeCreate(ctx, volume.CreateOptions{Name: name})
	if err != nil {
		return fmt.Errorf("create volume %q: %w", name, err)
	}
	return nil
}

func (c *Client) EnsureNetwork(ctx context.Context, name, driver, subnet, gateway string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if strings.TrimSpace(driver) == "" {
		driver = "bridge"
	}
	if _, err := c.raw.NetworkInspect(ctx, name, network.InspectOptions{}); err == nil {
		return nil
	}
	createOpts := network.CreateOptions{Driver: driver}
	if strings.TrimSpace(subnet) != "" || strings.TrimSpace(gateway) != "" {
		cfg := network.IPAMConfig{Subnet: subnet, Gateway: gateway}
		createOpts.IPAM = &network.IPAM{Config: []network.IPAMConfig{cfg}}
	}
	_, err := c.raw.NetworkCreate(ctx, name, createOpts)
	if err != nil {
		return fmt.Errorf("create network %q: %w", name, err)
	}
	return nil
}

func (c *Client) CreateSecretIfSupported(ctx context.Context, name, value string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
		return nil
	}
	spec := swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name},
		Data:        []byte(value),
	}
	_, err := c.raw.SecretCreate(ctx, spec)
	if err != nil {
		msg := strings.ToLower(err.Error())
		// Local non-swarm engines often reject secrets; fallback to env-only mode.
		if strings.Contains(msg, "swarm") || strings.Contains(msg, "not supported") || strings.Contains(msg, "this node is not a swarm manager") {
			return nil
		}
		if strings.Contains(msg, "already exists") {
			return nil
		}
		return fmt.Errorf("create secret %q: %w", name, err)
	}
	return nil
}

func (c *Client) CreateAndStartContainer(ctx context.Context, req CreateContainerRequest) (string, string, error) {
	exposed := nat.PortSet{}
	portMap := nat.PortMap{}
	for containerPort, hostPort := range req.PortBindings {
		p := nat.Port(containerPort)
		exposed[p] = struct{}{}
		portMap[p] = []nat.PortBinding{{HostPort: hostPort}}
	}

	cfg := &container.Config{
		Image:        req.Image,
		Cmd:          req.Command,
		Env:          req.Env,
		Labels:       req.Labels,
		ExposedPorts: exposed,
	}

	hostCfg := &container.HostConfig{
		PortBindings: portMap,
		Binds:        req.Binds,
	}

	created, err := c.raw.ContainerCreate(ctx, cfg, hostCfg, nil, nil, req.Name)
	if err != nil {
		return "", "", fmt.Errorf("create container %q: %w", req.Name, err)
	}

	if err := c.raw.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return "", "", fmt.Errorf("start container %q: %w", req.Name, err)
	}

	inspect, err := c.raw.ContainerInspect(ctx, created.ID)
	if err != nil {
		return created.ID, created.ID[:12], nil
	}

	return created.ID, inspect.Name, nil
}

func (c *Client) ContainerRunning(ctx context.Context, containerID string) (bool, error) {
	inspect, err := c.raw.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}
	return inspect.State != nil && inspect.State.Running, nil
}

func (c *Client) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}
	if err := c.raw.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("remove container %q: %w", containerID, err)
	}
	return nil
}

func (c *Client) ConnectContainerToNetwork(ctx context.Context, containerID, networkName string) error {
	if strings.TrimSpace(containerID) == "" || strings.TrimSpace(networkName) == "" {
		return nil
	}
	if err := c.raw.NetworkConnect(ctx, networkName, containerID, nil); err != nil {
		return fmt.Errorf("connect container %q to network %q: %w", containerID, networkName, err)
	}
	return nil
}

type CreateContainerRequest struct {
	Name         string
	Image        string
	Command      []string
	Env          []string
	Labels       map[string]string
	Binds        []string
	PortBindings map[string]string
}
