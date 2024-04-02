// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2023-present Datadog, Inc.

// Package local implements the agentless-scanner local subcommand
package local

import (
	"fmt"

	"github.com/DataDog/datadog-agent/cmd/agentless-scanner/common"
	"github.com/DataDog/datadog-agent/cmd/agentless-scanner/flags"

	"github.com/DataDog/datadog-agent/pkg/agentless/runner"
	"github.com/DataDog/datadog-agent/pkg/agentless/types"
	"github.com/DataDog/datadog-go/v5/statsd"

	"github.com/spf13/cobra"
)

// GroupCommand returns the local commands
func GroupCommand(parent *cobra.Command, statsd statsd.ClientInterface, sc *types.ScannerConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "local",
		Short:             "Datadog Agentless Scanner at your service.",
		Long:              `Datadog Agentless Scanner scans your cloud environment for vulnerabilities, compliance and security issues.`,
		SilenceUsage:      true,
		PersistentPreRunE: parent.PersistentPreRunE,
	}
	cmd.AddCommand(localScanCommand(statsd, sc))
	return cmd
}

func localScanCommand(statsd statsd.ClientInterface, sc *types.ScannerConfig) *cobra.Command {
	var localFlags struct {
		Hostname string
	}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Executes a scan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resourceID, err := types.HumanParseCloudID(args[0], types.CloudProviderNone, "", "")
			if err != nil {
				return err
			}
			return localScanCmd(statsd, sc, resourceID, localFlags.Hostname, flags.GlobalFlags.DefaultActions, flags.GlobalFlags.DiskMode, flags.GlobalFlags.NoForkScanners)
		},
	}

	cmd.Flags().StringVar(&localFlags.Hostname, "hostname", "unknown", "scan hostname")
	return cmd
}

func localScanCmd(statsd statsd.ClientInterface, sc *types.ScannerConfig, resourceID types.CloudID, targetHostname string, actions []types.ScanAction, diskMode types.DiskMode, noForkScanners bool) error {
	ctx := common.CtxTerminated()

	hostname := common.TryGetHostname(ctx)
	taskType, err := types.DefaultTaskType(resourceID)
	if err != nil {
		return err
	}
	scannerID := types.NewScannerID(types.CloudProviderNone, hostname)
	task, err := types.NewScanTask(
		taskType,
		resourceID.AsText(),
		scannerID,
		targetHostname,
		nil,
		actions,
		sc.DefaultRolesMapping,
		diskMode)
	if err != nil {
		return err
	}

	scanner, err := runner.New(runner.Options{
		ScannerConfig:  sc,
		ScannerID:      scannerID,
		DdEnv:          sc.Env,
		Workers:        1,
		ScannersMax:    8,
		PrintResults:   true,
		NoFork:         noForkScanners,
		DefaultActions: actions,
		Statsd:         statsd,
	})
	if err != nil {
		return fmt.Errorf("could not initialize agentless-scanner: %w", err)
	}
	go func() {
		scanner.PushConfig(ctx, &types.ScanConfig{
			Type:  types.ConfigTypeAWS,
			Tasks: []*types.ScanTask{task},
		})
		scanner.Stop()
	}()
	scanner.Start(ctx, statsd, sc)
	return nil
}
