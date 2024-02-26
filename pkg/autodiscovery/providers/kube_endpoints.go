// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build clusterchecks && kubeapiserver

package providers

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/labels"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/DataDog/datadog-agent/pkg/autodiscovery/common/utils"
	"github.com/DataDog/datadog-agent/pkg/autodiscovery/integration"
	"github.com/DataDog/datadog-agent/pkg/autodiscovery/providers/names"
	"github.com/DataDog/datadog-agent/pkg/autodiscovery/telemetry"
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/util/kubernetes/apiserver"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

type endpointResolveMode string

const (
	kubeEndpointID               = "endpoints"
	kubeEndpointAnnotationPrefix = "ad.datadoghq.com/endpoints."
	kubeEndpointResolvePath      = "resolve"

	kubeEndpointResolveAuto endpointResolveMode = "auto"
	kubeEndpointResolveIP   endpointResolveMode = "ip"
)

// kubeEndpointsConfigProvider implements the ConfigProvider interface for the apiserver.
type kubeEndpointsConfigProvider struct {
	sync.RWMutex
	serviceLister      listersv1.ServiceLister
	endpointsLister    listersv1.EndpointsLister
	upToDate           bool
	monitoredEndpoints map[string]bool
	configErrors       map[string]ErrorMsgSet
}

// configInfo contains an endpoint check config template with its name and namespace
type configInfo struct {
	tpl         integration.Config
	namespace   string
	name        string
	resolveMode endpointResolveMode
}

// NewKubeEndpointsConfigProvider returns a new ConfigProvider connected to apiserver.
// Connectivity is not checked at this stage to allow for retries, Collect will do it.
func NewKubeEndpointsConfigProvider(*config.ConfigurationProviders) (ConfigProvider, error) {
	// Using GetAPIClient (no wait) as Client should already be initialized by Cluster Agent main entrypoint before
	ac, err := apiserver.GetAPIClient()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to apiserver: %s", err)
	}

	servicesInformer := ac.InformerFactory.Core().V1().Services()
	if servicesInformer == nil {
		return nil, fmt.Errorf("cannot get service informer: %s", err)
	}

	p := &kubeEndpointsConfigProvider{
		serviceLister:      servicesInformer.Lister(),
		monitoredEndpoints: make(map[string]bool),
		configErrors:       make(map[string]ErrorMsgSet),
	}

	if _, err := servicesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    p.invalidate,
		UpdateFunc: p.invalidateIfChangedService,
		DeleteFunc: p.invalidate,
	}); err != nil {
		return nil, fmt.Errorf("cannot add event handler to service informer: %s", err)
	}

	endpointsInformer := ac.InformerFactory.Core().V1().Endpoints()
	if endpointsInformer == nil {
		return nil, fmt.Errorf("cannot get endpoint informer: %s", err)
	}

	p.endpointsLister = endpointsInformer.Lister()

	if _, err := endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: p.invalidateIfChangedEndpoints,
	}); err != nil {
		return nil, fmt.Errorf("cannot add event handler to endpoint informer: %s", err)
	}

	if config.Datadog.GetBool("cluster_checks.support_hybrid_ignore_ad_tags") {
		log.Warnf("The `cluster_checks.support_hybrid_ignore_ad_tags` flag is" +
			" deprecated and will be removed in a future version. Please replace " +
			"`ad.datadoghq.com/endpoints.ignore_autodiscovery_tags` in your service annotations" +
			"using adv2 for check specification and adv1 for `ignore_autodiscovery_tags`.")
	}

	return p, nil
}

// String returns a string representation of the kubeEndpointsConfigProvider
func (k *kubeEndpointsConfigProvider) String() string {
	return names.KubeEndpoints
}

// Collect retrieves services from the apiserver, builds Config objects and returns them
func (k *kubeEndpointsConfigProvider) Collect(context.Context) ([]integration.Config, error) {
	services, err := k.serviceLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	k.setUpToDate(true)

	var generatedConfigs []integration.Config
	parsedConfigsInfo := k.parseServiceAnnotationsForEndpoints(services, config.Datadog)
	for _, conf := range parsedConfigsInfo {
		kep, err := k.endpointsLister.Endpoints(conf.namespace).Get(conf.name)
		if err != nil {
			log.Errorf("Cannot get Kubernetes endpoints: %s", err)
			continue
		}
		generatedConfigs = append(generatedConfigs, generateConfigs(conf.tpl, conf.resolveMode, kep)...)
		endpointsID := apiserver.EntityForEndpoints(conf.namespace, conf.name, "")
		k.Lock()
		k.monitoredEndpoints[endpointsID] = true
		k.Unlock()
	}
	return generatedConfigs, nil
}

// IsUpToDate allows to cache configs as long as no changes are detected in the apiserver
func (k *kubeEndpointsConfigProvider) IsUpToDate(context.Context) (bool, error) {
	return k.upToDate, nil
}

func (k *kubeEndpointsConfigProvider) invalidate(obj interface{}) {
	castedObj, ok := obj.(*v1.Service)
	if !ok {
		// It's possible that we got a DeletedFinalStateUnknown here
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Received unexpected object: %T", obj)
			return
		}

		castedObj, ok = deletedState.Obj.(*v1.Service)
		if !ok {
			log.Errorf("Expected DeletedFinalStateUnknown to contain *v1.Service, got: %T", deletedState.Obj)
			return
		}
	}
	endpointsID := apiserver.EntityForEndpoints(castedObj.Namespace, castedObj.Name, "")
	log.Tracef("Invalidating configs on new/deleted service, endpoints entity: %s", endpointsID)
	k.Lock()
	defer k.Unlock()
	delete(k.monitoredEndpoints, endpointsID)
	k.upToDate = false
}

func (k *kubeEndpointsConfigProvider) invalidateIfChangedService(old, obj interface{}) {
	// Cast the updated object, don't invalidate on casting error.
	// nil pointers are safely handled by the casting logic.
	castedObj, ok := obj.(*v1.Service)
	if !ok {
		log.Errorf("Expected a *v1.Service type, got: %T", obj)
		return
	}
	// Cast the old object, invalidate on casting error
	castedOld, ok := old.(*v1.Service)
	if !ok {
		log.Errorf("Expected a *v1.Service type, got: %T", old)
		k.setUpToDate(false)
		return
	}
	// Quick exit if resversion did not change
	if castedObj.ResourceVersion == castedOld.ResourceVersion {
		return
	}
	if valuesDiffer(castedObj.Annotations, castedOld.Annotations, kubeEndpointAnnotationPrefix) {
		log.Trace("Invalidating configs on service end annotations change")
		k.setUpToDate(false)
		return
	}
}

func (k *kubeEndpointsConfigProvider) invalidateIfChangedEndpoints(old, obj interface{}) {
	// Cast the updated object, don't invalidate on casting error.
	// nil pointers are safely handled by the casting logic.
	castedObj, ok := obj.(*v1.Endpoints)
	if !ok {
		log.Errorf("Expected an *v1.Endpoints type, got: %T", obj)
		return
	}
	// Cast the old object, invalidate on casting error
	castedOld, ok := old.(*v1.Endpoints)
	if !ok {
		log.Errorf("Expected a *v1.Endpoints type, got: %T", old)
		k.setUpToDate(false)
		return
	}
	// Quick exit if resversion did not change
	if castedObj.ResourceVersion == castedOld.ResourceVersion {
		return
	}
	// Make sure we invalidate a monitored endpoints object
	endpointsID := apiserver.EntityForEndpoints(castedObj.Namespace, castedObj.Name, "")
	k.Lock()
	defer k.Unlock()
	if found := k.monitoredEndpoints[endpointsID]; found {
		// Invalidate only when subsets change
		k.upToDate = equality.Semantic.DeepEqual(castedObj.Subsets, castedOld.Subsets)
	}
}

// setUpToDate is a thread-safe method to update the upToDate value
func (k *kubeEndpointsConfigProvider) setUpToDate(v bool) {
	k.Lock()
	defer k.Unlock()
	k.upToDate = v
}

func (k *kubeEndpointsConfigProvider) parseServiceAnnotationsForEndpoints(services []*v1.Service, cfg config.Config) []configInfo {
	var configsInfo []configInfo

	setEndpointIDs := map[string]struct{}{}

	for _, svc := range services {
		if svc == nil || svc.ObjectMeta.UID == "" {
			log.Debug("Ignoring a nil service")
			continue
		}

		endpointsID := apiserver.EntityForEndpoints(svc.Namespace, svc.Name, "")
		setEndpointIDs[endpointsID] = struct{}{}

		endptConf, errors := utils.ExtractTemplatesFromAnnotations(endpointsID, svc.GetAnnotations(), kubeEndpointID)
		for _, err := range errors {
			log.Errorf("Cannot parse endpoint template for service %s/%s: %s", svc.Namespace, svc.Name, err)
		}

		if len(errors) > 0 {
			errMsgSet := make(ErrorMsgSet)
			for _, err := range errors {
				log.Errorf("Cannot parse endpoint template for service %s/%s: %s", svc.Namespace, svc.Name, err)
				errMsgSet[err.Error()] = struct{}{}
			}
			k.configErrors[endpointsID] = errMsgSet
		} else {
			delete(k.configErrors, endpointsID)
		}

		var resolveMode endpointResolveMode
		if value, found := svc.Annotations[kubeEndpointAnnotationPrefix+kubeEndpointResolvePath]; found {
			resolveMode = endpointResolveMode(value)
		}

		ignoreAdForHybridScenariosTags := ignoreADTagsFromAnnotations(svc.GetAnnotations(), kubeEndpointAnnotationPrefix)
		for i := range endptConf {
			endptConf[i].Source = "kube_endpoints:" + endpointsID
			if cfg.GetBool("cluster_checks.support_hybrid_ignore_ad_tags") {
				endptConf[i].IgnoreAutodiscoveryTags = endptConf[i].IgnoreAutodiscoveryTags || ignoreAdForHybridScenariosTags
			}
			configsInfo = append(configsInfo, configInfo{
				tpl:         endptConf[i],
				namespace:   svc.Namespace,
				name:        svc.Name,
				resolveMode: resolveMode,
			})
		}
	}

	k.cleanErrorsOfDeletedEndpoints(setEndpointIDs)

	telemetry.Errors.Set(float64(len(k.configErrors)), names.KubeEndpoints)

	return configsInfo
}

// generateConfigs creates a config template for each Endpoints IP
func generateConfigs(tpl integration.Config, resolveMode endpointResolveMode, kep *v1.Endpoints) []integration.Config {
	if kep == nil {
		log.Warn("Nil Kubernetes Endpoints object, cannot generate config templates")
		return []integration.Config{tpl}
	}
	generatedConfigs := make([]integration.Config, 0)
	namespace := kep.Namespace
	name := kep.Name

	// Check resolve annotation to know how we should process this endpoint
	var resolveFunc func(*integration.Config, v1.EndpointAddress)
	switch resolveMode {
	// IP: we explicitly ignore what's behind this address (nothing to do)
	case kubeEndpointResolveIP:
	// In case of unknown value, fallback to auto
	default:
		log.Warnf("Unknown resolve value: %s for endpoint: %s/%s - fallback to auto mode", resolveMode, namespace, name)
		fallthrough
	// Auto or empty (default to auto): we try to resolve the POD behind this address
	case "":
		fallthrough
	case kubeEndpointResolveAuto:
		resolveFunc = utils.ResolveEndpointConfigAuto
	}

	for i := range kep.Subsets {
		for j := range kep.Subsets[i].Addresses {
			// Set a new entity containing the endpoint's IP
			entity := apiserver.EntityForEndpoints(namespace, name, kep.Subsets[i].Addresses[j].IP)
			newConfig := integration.Config{
				ServiceID:               entity,
				Name:                    tpl.Name,
				Instances:               tpl.Instances,
				InitConfig:              tpl.InitConfig,
				MetricConfig:            tpl.MetricConfig,
				LogsConfig:              tpl.LogsConfig,
				ADIdentifiers:           []string{entity},
				ClusterCheck:            true,
				Provider:                tpl.Provider,
				Source:                  tpl.Source,
				IgnoreAutodiscoveryTags: tpl.IgnoreAutodiscoveryTags,
			}

			if resolveFunc != nil {
				resolveFunc(&newConfig, kep.Subsets[i].Addresses[j])
			}

			generatedConfigs = append(generatedConfigs, newConfig)
		}
	}
	return generatedConfigs
}

func (k *kubeEndpointsConfigProvider) cleanErrorsOfDeletedEndpoints(setCurrentEndpointIDs map[string]struct{}) {
	setEndpointIDsWithErrors := map[string]struct{}{}
	for endpointID := range k.configErrors {
		setEndpointIDsWithErrors[endpointID] = struct{}{}
	}

	for endpointID := range setEndpointIDsWithErrors {
		if _, exists := setCurrentEndpointIDs[endpointID]; !exists {
			delete(k.configErrors, endpointID)
		}
	}
}

func init() {
	RegisterProvider(names.KubeEndpointsRegisterName, NewKubeEndpointsConfigProvider)
}

// GetConfigErrors returns a map of configuration errors for each Kubernetes endpoint
func (k *kubeEndpointsConfigProvider) GetConfigErrors() map[string]ErrorMsgSet {
	return k.configErrors
}
