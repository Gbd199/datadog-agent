// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// Package flare contains helpers and e2e tests of the flare command
package diagnose

import (
	"testing"

	"github.com/DataDog/datadog-agent/test/new-e2e/pkg/e2e"
	awshost "github.com/DataDog/datadog-agent/test/new-e2e/pkg/environments/aws/host"
	"github.com/DataDog/test-infra-definitions/components/datadog/agentparams"
	"github.com/DataDog/test-infra-definitions/components/os"
	"github.com/DataDog/test-infra-definitions/scenarios/aws/ec2"
	"github.com/stretchr/testify/assert"
)

type windowsDiagnoseSuite struct {
	baseDiagnoseSuite
}

func TestWindowsDiagnoseSuite(t *testing.T) {
	e2e.Run(t, &windowsDiagnoseSuite{}, e2e.WithProvisioner(awshost.Provisioner(awshost.WithEC2InstanceOptions(ec2.WithOS(os.WindowsDefault)))))
}

func (v *windowsDiagnoseSuite) TestDiagnoseOtherCmdPort() {
	params := agentparams.WithAgentConfig("cmd_port: 4567")
	v.UpdateEnv(awshost.Provisioner(awshost.WithEC2InstanceOptions(ec2.WithOS(os.WindowsDefault)), awshost.WithAgentOptions(params)))

	diagnose := getDiagnoseOutput(&v.baseDiagnoseSuite)
	assert.NotContains(v.T(), diagnose, "FAIL")
}
