// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build kubeapiserver

package kubeapiserver

import (
	"context"
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/tools/cache"

	"github.com/DataDog/datadog-agent/comp/core/workloadmeta"
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

func newMetadataStore(ctx context.Context, wlmetaStore workloadmeta.Component, metadataclient metadata.Interface, gvr schema.GroupVersionResource) (*cache.Reflector, *reflectorStore) {
	metadataListerWatcher := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return metadataclient.Resource(gvr).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return metadataclient.Resource(gvr).Watch(ctx, options)
		},
	}

	annotationsExclude := config.Datadog().GetStringSlice("cluster_agent.kube_metadata_collection.resource_annotations_exclude")
	parser, err := newMetadataParser(gvr, annotationsExclude)
	if err != nil {
		_ = log.Errorf("unable to parse all resource_annotations_exclude: %v, err:", err)
		parser, _ = newMetadataParser(gvr, nil)
	}

	metadataStore := &reflectorStore{
		wlmetaStore: wlmetaStore,
		seen:        make(map[string]workloadmeta.EntityID),
		parser:      parser,
		filter:      nil,
	}
	metadataReflector := cache.NewNamedReflector(
		componentName,
		metadataListerWatcher,
		&metav1.PartialObjectMetadata{},
		metadataStore,
		noResync,
	)
	return metadataReflector, metadataStore
}

type metadataParser struct {
	gvr               schema.GroupVersionResource
	annotationsFilter []*regexp.Regexp
}

func newMetadataParser(gvr schema.GroupVersionResource, annotationsExclude []string) (objectParser, error) {
	filters, err := parseFilters(annotationsExclude)
	if err != nil {
		return nil, err
	}

	return metadataParser{gvr: gvr, annotationsFilter: filters}, nil
}

// generateEntityID generates and returns a unique entity id for KubernetesMetadata entity
// for namespaced objects, the id will have the format {resourceType}/{namespace}/{name} (e.g. deployments/default/app )
// for cluster scoped objects, the id will have the format {resourceType}//{name} (e.g. node//master-node)
func (p metadataParser) generateEntityID(resource, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", resource, namespace, name)
}

func (p metadataParser) Parse(obj interface{}) workloadmeta.Entity {
	partialObjectMetadata := obj.(*metav1.PartialObjectMetadata)
	id := p.generateEntityID(p.gvr.Resource, partialObjectMetadata.Namespace, partialObjectMetadata.Name)

	return &workloadmeta.KubernetesMetadata{
		EntityID: workloadmeta.EntityID{
			Kind: workloadmeta.KindKubernetesMetadata,
			ID:   id,
		},
		EntityMeta: workloadmeta.EntityMeta{
			Name:        partialObjectMetadata.Name,
			Namespace:   partialObjectMetadata.Namespace,
			Labels:      partialObjectMetadata.Labels,
			Annotations: filterMapStringKey(partialObjectMetadata.Annotations, p.annotationsFilter),
		},
		GVR: p.gvr,
	}
}
