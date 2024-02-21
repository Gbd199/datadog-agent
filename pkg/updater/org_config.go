// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package updater

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/DataDog/datadog-agent/pkg/config/remote/client"
	"github.com/DataDog/datadog-agent/pkg/remoteconfig/state"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

//go:embed data/defaults.json
var rawDefaults []byte

//go:embed data/catalog.json
var rawCatalog []byte

// orgConfig represents the (remote) configuration of an organization.
// More precisely it hides away the RC details to obtain:
// - the catalog of packages
// - the default version of a package and its corresponding catalog entry
type orgConfig struct {
	m                   sync.Mutex
	catalogReceived     chan struct{}
	catalogReceivedSync sync.Once

	rc      *client.Client
	catalog catalog
}

// NewOrgConfig returns a new OrgConfig.
func newOrgConfig(rc *client.Client) (*orgConfig, error) {
	c := &orgConfig{
		catalogReceived: make(chan struct{}),
		rc:              rc,
	}
	rc.Subscribe(state.ProductUpdaterCatalogDD, c.onCatalogUpdate)
	return c, nil
}

// Package represents a downloadable package.
type Package struct {
	Name     string `json:"package"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
}

type catalog struct {
	Packages []Package `json:"packages"`
}

// GetPackage returns the package with the given name and version.
// The function will block until the catalog is received from RC.
func (c *orgConfig) GetPackage(ctx context.Context, pkg string, version string) (Package, error) {
	err := c.waitForCatalog(ctx)
	if err != nil {
		return Package{}, fmt.Errorf("context canceled while waiting for catalog: %w", err)
	}
	c.m.Lock()
	defer c.m.Unlock()
	for _, p := range c.catalog.Packages {
		if p.Name == pkg &&
			p.Version == version {
			return p, nil
		}
	}
	return Package{}, fmt.Errorf("package %s version %s not found %v", pkg, version, c.catalog.Packages)
}

// GetDefaultPackage returns the default version for the given package.
// The function blocks until the catalog and org preferences are received from RC.
// TODO: Implement with RC support instead of hardcoded default file.
func (c *orgConfig) GetDefaultPackage(ctx context.Context, pkg string) (Package, error) {
	var defaults map[string]string
	err := json.Unmarshal(rawDefaults, &defaults)
	if err != nil {
		return Package{}, fmt.Errorf("could not unmarshal defaults: %w", err)
	}
	return c.GetPackage(ctx, pkg, defaults[pkg])
}

func (c *orgConfig) waitForCatalog(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.catalogReceived:
		return nil
	}
}

func (c *orgConfig) onCatalogUpdate(catalogConfigs map[string]state.RawConfig, applyStateCallback func(string, state.ApplyStatus)) {
	var mergedCatalog catalog
	err := json.Unmarshal(rawCatalog, &mergedCatalog)
	if err != nil {
		log.Errorf("could not unmarshal default catalog: %s", err)
	}
	for configPath, config := range catalogConfigs {
		var catalog catalog
		err := json.Unmarshal(config.Config, &catalog)
		if err != nil {
			log.Errorf("could not unmarshal updater catalog: %s", err)
			applyStateCallback(configPath, state.ApplyStatus{State: state.ApplyStateError, Error: err.Error()})
			return
		}
		mergedCatalog.Packages = append(mergedCatalog.Packages, catalog.Packages...)
	}
	for configPath := range catalogConfigs {
		applyStateCallback(configPath, state.ApplyStatus{State: state.ApplyStateAcknowledged})
	}
	c.m.Lock()
	defer c.m.Unlock()
	log.Info("datadog packages catalog was updated")
	c.catalog = mergedCatalog
	c.catalogReceivedSync.Do(func() {
		close(c.catalogReceived)
	})
}
