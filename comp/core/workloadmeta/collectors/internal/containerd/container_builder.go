// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build containerd

// Package containerd provides the containerd collector for workloadmeta
package containerd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"

	"github.com/DataDog/datadog-agent/comp/core/workloadmeta"
	"github.com/DataDog/datadog-agent/comp/core/workloadmeta/collectors/util"
	cutil "github.com/DataDog/datadog-agent/pkg/util/containerd"
	"github.com/DataDog/datadog-agent/pkg/util/containers"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

// kataRuntimePrefix is the prefix used by Kata Containers runtime
const kataRuntimePrefix = "io.containerd.kata"

// buildWorkloadMetaContainer generates a workloadmeta.Container from a containerd.Container
func buildWorkloadMetaContainer(namespace string, container containerd.Container, containerdClient cutil.ContainerdItf, store workloadmeta.Component) (workloadmeta.Container, error) {
	if container == nil {
		return workloadmeta.Container{}, fmt.Errorf("cannot build workloadmeta container from nil containerd container")
	}

	info, err := containerdClient.Info(namespace, container)
	if err != nil {
		return workloadmeta.Container{}, err
	}
	runtimeFlavor := extractRuntimeFlavor(info.Runtime.Name)

	// Prepare context
	ctx := context.Background()
	ctx = namespaces.WithNamespace(ctx, namespace)

	// Get image id from container's image config
	var imageID string
	if img, err := container.Image(ctx); err != nil {
		log.Warnf("cannot get container %s's image: %v", container.ID(), err)
	} else {
		if imgConfig, err := img.Config(ctx); err != nil {
			log.Warnf("cannot get container %s's image's config: %v", container.ID(), err)
		} else {
			imageID = imgConfig.Digest.String()
		}
	}

	// Get Container PID
	var pid int
	task, err := container.Task(ctx, nil)
	if err == nil {
		pid = int(task.Pid())
	} else {
		pid = 0
		log.Debugf("cannot get container %s's process PID: %v", container.ID(), err)
	}

	image, err := workloadmeta.NewContainerImage(imageID, info.Image)
	if err != nil {
		log.Debugf("cannot split image name %q: %s", info.Image, err)
	}

	image.RepoDigest = util.ExtractRepoDigestFromImage(imageID, image.Registry, store) // "sha256:digest"
	if image.RepoDigest == "" {
		log.Debugf("cannot get repo digest for image %s from workloadmeta store", imageID)
		contImage, err := containerdClient.ImageOfContainer(namespace, container)
		if err == nil && contImage != nil {
			// Get repo digest from containerd client.
			// This is a fallback mechanism in case we cannot get the repo digest
			// from workloadmeta store.
			image.RepoDigest = contImage.Target().Digest.String()
		}
	}
	status, err := containerdClient.Status(namespace, container)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return workloadmeta.Container{}, err
		}

		// The container exists, but there isn't a task associated to it. That
		// means that the container is not running, which is all we need to know
		// in this function (we can set any status != containerd.Running).
		status = containerd.Unknown
	}

	networkIPs := make(map[string]string)
	ip, err := extractIP(namespace, container, containerdClient)
	if err != nil {
		log.Debugf("cannot get IP of container %s", err)
	} else if ip == "" {
		log.Debugf("no IPs for container")
	} else {
		networkIPs[""] = ip
	}

	// Some attributes in workloadmeta.Container cannot be fetched from
	// containerd. I've marked those as "Not available".
	workloadContainer := workloadmeta.Container{
		EntityID: workloadmeta.EntityID{
			Kind: workloadmeta.KindContainer,
			ID:   container.ID(),
		},
		EntityMeta: workloadmeta.EntityMeta{
			Name:   "", // Not available
			Labels: info.Labels,
		},
		Image:         image,
		Ports:         nil, // Not available
		Runtime:       workloadmeta.ContainerRuntimeContainerd,
		RuntimeFlavor: runtimeFlavor,
		State: workloadmeta.ContainerState{
			Running:    status == containerd.Running,
			Status:     extractStatus(status),
			CreatedAt:  info.CreatedAt,
			StartedAt:  info.CreatedAt, // StartedAt not available in containerd, mapped to CreatedAt
			FinishedAt: time.Time{},    // Not available
		},
		NetworkIPs: networkIPs,
		PID:        pid, // PID will be 0 for non-running containers
	}

	// Spec retrieval is slow if large due to JSON parsing
	spec, err := containerdClient.Spec(namespace, info, cutil.DefaultAllowedSpecMaxSize)
	if err == nil {
		if spec == nil {
			return workloadmeta.Container{}, fmt.Errorf("retrieved empty spec for container id: %s", info.ID)
		}

		envs, err := cutil.EnvVarsFromSpec(spec, containers.EnvVarFilterFromConfig().IsIncluded)
		if err != nil {
			return workloadmeta.Container{}, err
		}

		workloadContainer.EnvVars = envs
		workloadContainer.Hostname = spec.Hostname
		if spec.Linux != nil {
			workloadContainer.CgroupPath = extractCgroupPath(spec.Linux.CgroupsPath)
		}
	} else if errors.Is(err, cutil.ErrSpecTooLarge) {
		log.Warnf("Skipping parsing of container spec for container id: %s, spec is bigger than: %d", info.ID, cutil.DefaultAllowedSpecMaxSize)
	} else {
		return workloadmeta.Container{}, err
	}

	return workloadContainer, nil
}

// Containerd applies some transformations to the cgroup path, we need to revert them
// https://github.com/containerd/containerd/blob/b168147ca8fccf05003117324f493d40f97b6077/internal/cri/server/podsandbox/helpers_linux.go#L64-L65
// See https://github.com/opencontainers/runc/blob/main/docs/systemd.md
func extractCgroupPath(path string) string {
	res := path
	if l := strings.Split(path, ":"); len(l) == 3 {
		res = l[0] + "/" + l[1] + "-" + l[2] + ".scope"
	}
	return res
}

func extractStatus(status containerd.ProcessStatus) workloadmeta.ContainerStatus {
	switch status {
	case containerd.Paused, containerd.Pausing:
		return workloadmeta.ContainerStatusPaused
	case containerd.Created:
		return workloadmeta.ContainerStatusCreated
	case containerd.Running:
		return workloadmeta.ContainerStatusRunning
	case containerd.Stopped:
		return workloadmeta.ContainerStatusStopped
	}

	return workloadmeta.ContainerStatusUnknown
}

// extractRuntimeFlavor extracts the runtime from a runtime string.
func extractRuntimeFlavor(runtime string) workloadmeta.ContainerRuntimeFlavor {
	if strings.HasPrefix(runtime, kataRuntimePrefix) {
		return workloadmeta.ContainerRuntimeFlavorKata
	}
	return workloadmeta.ContainerRuntimeFlavorDefault
}
