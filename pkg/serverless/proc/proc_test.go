// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build !windows

package proc

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPidListInvalid(t *testing.T) {
	pids := getPidList("/incorrect/folder")
	assert.Equal(t, 0, len(pids))
}

func TestGetPidListValid(t *testing.T) {
	pids := getPidList("./testData")
	sort.Ints(pids)
	assert.Equal(t, 2, len(pids))
	assert.Equal(t, 13, pids[0])
	assert.Equal(t, 142, pids[1])
}

func TestSearchProcsForEnvVariableFromPidIncorrect(t *testing.T) {
	envVars := getEnvVariablesFromPid("./testData", 999)
	assert.Equal(t, 0, len(envVars))
}

func TestSearchProcsForEnvVariableFromPidCorrect(t *testing.T) {
	envVars := getEnvVariablesFromPid("./testData", 13)
	assert.Equal(t, "value0", envVars["env0"])
	assert.Equal(t, "value1", envVars["env1"])
	assert.Equal(t, "AWS_Lambda_nodejs14.x", envVars["AWS_EXECUTION_ENV"])
	assert.Equal(t, "value3", envVars["env3"])
	assert.Equal(t, 4, len(envVars))
}

func TestSearchProcsForEnvVariableFound(t *testing.T) {
	result := SearchProcsForEnvVariable("./testData", "env1")
	expected := []string{"value1"}
	assert.Equal(t, 1, len(result))
	assert.Equal(t, expected[0], result[0])
}
func TestSearchProcsForEnvVariableNotFound(t *testing.T) {
	result := SearchProcsForEnvVariable("./testData", "xxx")
	assert.Equal(t, 0, len(result))
}

func TestGetCPUData(t *testing.T) {
	path := "./testData/stat/valid_stat"
	cpuData, err := GetCPUData(path)
	assert.Nil(t, err)
	assert.Equal(t, float64(23370), cpuData.TotalUserTimeMs)
	assert.Equal(t, float64(1880), cpuData.TotalSystemTimeMs)
	assert.Equal(t, float64(178380), cpuData.TotalIdleTimeMs)
	assert.Equal(t, 2, len(cpuData.IndividualCPUIdleTimes))
	assert.Equal(t, float64(91880), cpuData.IndividualCPUIdleTimes["cpu0"])
	assert.Equal(t, float64(86490), cpuData.IndividualCPUIdleTimes["cpu1"])

	path = "./testData/stat/invalid_stat_non_numerical_value_1"
	cpuData, err = GetCPUData(path)
	assert.NotNil(t, err)
	assert.Nil(t, cpuData)

	path = "./testData/stat/invalid_stat_non_numerical_value_2"
	cpuData, err = GetCPUData(path)
	assert.NotNil(t, err)
	assert.Nil(t, cpuData)

	path = "./testData/stat/invalid_stat_malformed_first_line"
	cpuData, err = GetCPUData(path)
	assert.NotNil(t, err)
	assert.Nil(t, cpuData)

	path = "./testData/stat/invalid_stat_malformed_per_cpu_line"
	cpuData, err = GetCPUData(path)
	assert.Nil(t, err)
	assert.Equal(t, float64(23370), cpuData.TotalUserTimeMs)
	assert.Equal(t, float64(1880), cpuData.TotalSystemTimeMs)
	assert.Equal(t, float64(178380), cpuData.TotalIdleTimeMs)
	assert.Equal(t, 0, len(cpuData.IndividualCPUIdleTimes))

	path = "./testData/stat/nonexistent_stat"
	cpuData, err = GetCPUData(path)
	assert.NotNil(t, err)
	assert.Nil(t, cpuData)
}

func TestGetUptime(t *testing.T) {
	path := "./testData/uptime/valid_uptime"
	uptime, err := GetUptime(path)
	assert.Nil(t, err)
	assert.Equal(t, float64(3213103123000), uptime)

	path = "./testData/uptime/invalid_data_uptime"
	uptime, err = GetUptime(path)
	assert.NotNil(t, err)
	assert.Equal(t, float64(0), uptime)

	path = "./testData/uptime/malformed_uptime"
	uptime, err = GetUptime(path)
	assert.NotNil(t, err)
	assert.Equal(t, float64(0), uptime)

	path = "./testData/uptime/nonexistent_uptime"
	uptime, err = GetUptime(path)
	assert.NotNil(t, err)
	assert.Equal(t, float64(0), uptime)
}
