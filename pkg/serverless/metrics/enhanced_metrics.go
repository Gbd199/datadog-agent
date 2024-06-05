// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//nolint:revive // TODO(SERV) Fix revive linter
package metrics

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/DataDog/datadog-agent/pkg/aggregator"
	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/DataDog/datadog-agent/pkg/serverless/proc"
	serverlessTags "github.com/DataDog/datadog-agent/pkg/serverless/tags"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

const (
	// Latest Lambda pricing per https://aws.amazon.com/lambda/pricing/
	baseLambdaInvocationPrice = 0.0000002
	x86LambdaPricePerGbSecond = 0.0000166667
	armLambdaPricePerGbSecond = 0.0000133334
	msToSec                   = 0.001

	// Enhanced metrics
	maxMemoryUsedMetric       = "aws.lambda.enhanced.max_memory_used"
	memorySizeMetric          = "aws.lambda.enhanced.memorysize"
	runtimeDurationMetric     = "aws.lambda.enhanced.runtime_duration"
	billedDurationMetric      = "aws.lambda.enhanced.billed_duration"
	durationMetric            = "aws.lambda.enhanced.duration"
	postRuntimeDurationMetric = "aws.lambda.enhanced.post_runtime_duration"
	estimatedCostMetric       = "aws.lambda.enhanced.estimated_cost"
	initDurationMetric        = "aws.lambda.enhanced.init_duration"
	responseLatencyMetric     = "aws.lambda.enhanced.response_latency"
	responseDurationMetric    = "aws.lambda.enhanced.response_duration"
	producedBytesMetric       = "aws.lambda.enhanced.produced_bytes"
	// OutOfMemoryMetric is the name of the out of memory enhanced Lambda metric
	OutOfMemoryMetric = "aws.lambda.enhanced.out_of_memory"
	timeoutsMetric    = "aws.lambda.enhanced.timeouts"
	// ErrorsMetric is the name of the errors enhanced Lambda metric
	ErrorsMetric              = "aws.lambda.enhanced.errors"
	invocationsMetric         = "aws.lambda.enhanced.invocations"
	asmInvocationsMetric      = "aws.lambda.enhanced.asm.invocations"
	cpuSystemTimeMetric       = "aws.lambda.enhanced.cpu_system_time"
	cpuUserTimeMetric         = "aws.lambda.enhanced.cpu_user_time"
	cpuTotalTimeMetric        = "aws.lambda.enhanced.cpu_total_time"
	cpuTotalUtilizationMetric = "aws.lambda.enhanced.cpu_total_utilization"
	numCoresMetric            = "aws.lambda.enhanced.num_cores"
	cpuMaxUtilizationMetric   = "aws.lambda.enhanced.cpu_max_utilization"
	cpuMinUtilizationMetric   = "aws.lambda.enhanced.cpu_min_utilization"
	enhancedMetricsEnvVar     = "DD_ENHANCED_METRICS"
)

func getOutOfMemorySubstrings() []string {
	return []string{
		"fatal error: runtime: out of memory",       // Go
		"java.lang.OutOfMemoryError",                // Java
		"JavaScript heap out of memory",             // Node
		"Runtime exited with error: signal: killed", // Node
		"MemoryError", // Python
		"failed to allocate memory (NoMemoryError)", // Ruby
		"OutOfMemoryException",                      // .NET
	}
}

// GenerateEnhancedMetricsFromRuntimeDoneLogArgs are the arguments required for
// the GenerateEnhancedMetricsFromRuntimeDoneLog func
type GenerateEnhancedMetricsFromRuntimeDoneLogArgs struct {
	Start            time.Time
	End              time.Time
	ResponseLatency  float64
	ResponseDuration float64
	ProducedBytes    float64
	Tags             []string
	Demux            aggregator.Demultiplexer
}

// GenerateEnhancedMetricsFromRuntimeDoneLog generates the runtime duration metric
func GenerateEnhancedMetricsFromRuntimeDoneLog(args GenerateEnhancedMetricsFromRuntimeDoneLogArgs) {
	// first check if both date are set
	if args.Start.IsZero() || args.End.IsZero() {
		log.Debug("Impossible to compute aws.lambda.enhanced.runtime_duration due to an invalid interval")
	} else {
		duration := args.End.Sub(args.Start).Milliseconds()
		args.Demux.AggregateSample(metrics.MetricSample{
			Name:       runtimeDurationMetric,
			Value:      float64(duration),
			Mtype:      metrics.DistributionType,
			Tags:       args.Tags,
			SampleRate: 1,
			Timestamp:  float64(args.End.UnixNano()) / float64(time.Second),
		})
	}
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       responseLatencyMetric,
		Value:      args.ResponseLatency,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  float64(args.End.UnixNano()) / float64(time.Second),
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       responseDurationMetric,
		Value:      args.ResponseDuration,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  float64(args.End.UnixNano()) / float64(time.Second),
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       producedBytesMetric,
		Value:      args.ProducedBytes,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  float64(args.End.UnixNano()) / float64(time.Second),
	})
}

// ContainsOutOfMemoryLog determines whether a runtime specific out of memory string is found in the log line
func ContainsOutOfMemoryLog(logString string) bool {
	for _, substring := range getOutOfMemorySubstrings() {
		if strings.Contains(logString, substring) {
			return true
		}
	}
	return false
}

// GenerateOutOfMemoryEnhancedMetrics generates enhanced metrics specific to an out of memory error
func GenerateOutOfMemoryEnhancedMetrics(time time.Time, tags []string, demux aggregator.Demultiplexer) {
	SendOutOfMemoryEnhancedMetric(tags, time, demux)
	SendErrorsEnhancedMetric(tags, time, demux)
}

// GenerateEnhancedMetricsFromReportLogArgs provides the arguments required for
// the GenerateEnhancedMetricsFromReportLog func
type GenerateEnhancedMetricsFromReportLogArgs struct {
	InitDurationMs   float64
	DurationMs       float64
	BilledDurationMs int
	MemorySizeMb     int
	MaxMemoryUsedMb  int
	RuntimeStart     time.Time
	RuntimeEnd       time.Time
	T                time.Time
	Tags             []string
	Demux            aggregator.Demultiplexer
}

// GenerateEnhancedMetricsFromReportLog generates enhanced metrics from a LogTypePlatformReport log message
func GenerateEnhancedMetricsFromReportLog(args GenerateEnhancedMetricsFromReportLogArgs) {
	timestamp := float64(args.T.UnixNano()) / float64(time.Second)
	billedDuration := float64(args.BilledDurationMs)
	memorySize := float64(args.MemorySizeMb)
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       maxMemoryUsedMetric,
		Value:      float64(args.MaxMemoryUsedMb),
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  timestamp,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       memorySizeMetric,
		Value:      memorySize,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  timestamp,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       billedDurationMetric,
		Value:      billedDuration * msToSec,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  timestamp,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       durationMetric,
		Value:      args.DurationMs * msToSec,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  timestamp,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       estimatedCostMetric,
		Value:      calculateEstimatedCost(billedDuration, memorySize, serverlessTags.ResolveRuntimeArch()),
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  timestamp,
	})
	if args.RuntimeStart.IsZero() || args.RuntimeEnd.IsZero() {
		log.Debug("Impossible to compute aws.lambda.enhanced.post_runtime_duration due to an invalid interval")
	} else {
		postRuntimeDuration := args.DurationMs - float64(args.RuntimeEnd.Sub(args.RuntimeStart).Milliseconds())
		args.Demux.AggregateSample(metrics.MetricSample{
			Name:       postRuntimeDurationMetric,
			Value:      postRuntimeDuration,
			Mtype:      metrics.DistributionType,
			Tags:       args.Tags,
			SampleRate: 1,
			Timestamp:  timestamp,
		})
	}
	if args.InitDurationMs > 0 {
		args.Demux.AggregateSample(metrics.MetricSample{
			Name:       initDurationMetric,
			Value:      args.InitDurationMs * msToSec,
			Mtype:      metrics.DistributionType,
			Tags:       args.Tags,
			SampleRate: 1,
			Timestamp:  timestamp,
		})
	}
}

// SendOutOfMemoryEnhancedMetric sends an enhanced metric representing a function running out of memory at a given time
func SendOutOfMemoryEnhancedMetric(tags []string, t time.Time, demux aggregator.Demultiplexer) {
	incrementEnhancedMetric(OutOfMemoryMetric, tags, float64(t.UnixNano())/float64(time.Second), demux, false)
}

// SendErrorsEnhancedMetric sends an enhanced metric representing an error at a given time
func SendErrorsEnhancedMetric(tags []string, t time.Time, demux aggregator.Demultiplexer) {
	incrementEnhancedMetric(ErrorsMetric, tags, float64(t.UnixNano())/float64(time.Second), demux, false)
}

// SendTimeoutEnhancedMetric sends an enhanced metric representing a timeout at the current time
func SendTimeoutEnhancedMetric(tags []string, demux aggregator.Demultiplexer) {
	incrementEnhancedMetric(timeoutsMetric, tags, float64(time.Now().UnixNano())/float64(time.Second), demux, false)
}

// SendInvocationEnhancedMetric sends an enhanced metric representing an invocation at the current time
func SendInvocationEnhancedMetric(tags []string, demux aggregator.Demultiplexer) {
	incrementEnhancedMetric(invocationsMetric, tags, float64(time.Now().UnixNano())/float64(time.Second), demux, false)
}

// SendASMInvocationEnhancedMetric sends an enhanced metric representing an appsec supported invocation at the current time
// Metric is sent even if enhanced metrics are disabled
func SendASMInvocationEnhancedMetric(tags []string, demux aggregator.Demultiplexer) {
	incrementEnhancedMetric(asmInvocationsMetric, tags, float64(time.Now().UnixNano())/float64(time.Second), demux, true)
}

type GenerateCPUEnhancedMetricsArgs struct {
	UserCPUTimeMs   float64
	SystemCPUTimeMs float64
	Uptime          float64
	Tags            []string
	Demux           aggregator.Demultiplexer
	Time            float64
}

type GenerateCPUUtilizationEnhancedMetricArgs struct {
	IndividualCPUIdleTimes       map[string]float64
	IndividualCPUIdleOffsetTimes map[string]float64
	IdleTimeMs                   float64
	IdleTimeOffsetMs             float64
	UptimeMs                     float64
	UptimeOffsetMs               float64
	Tags                         []string
	Demux                        aggregator.Demultiplexer
	Time                         float64
}

// GenerateCPUEnhancedMetrics generates enhanced metrics for CPU time spent running the function in kernel mode,
// in user mode, and in total
func GenerateCPUEnhancedMetrics(args GenerateCPUEnhancedMetricsArgs) {
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       cpuSystemTimeMetric,
		Value:      args.SystemCPUTimeMs,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  args.Time,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       cpuUserTimeMetric,
		Value:      args.UserCPUTimeMs,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  args.Time,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       cpuTotalTimeMetric,
		Value:      args.SystemCPUTimeMs + args.UserCPUTimeMs,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  args.Time,
	})
}

// SendCPUEnhancedMetrics sends CPU enhanced metrics for the invocation
func SendCPUEnhancedMetrics(cpuOffsetData proc.CPUData, uptimeOffset float64, tags []string, demux aggregator.Demultiplexer) {
	if strings.ToLower(os.Getenv(enhancedMetricsEnvVar)) == "false" {
		return
	}
	cpuData, err := proc.GetCPUData("/proc/stat")
	if err != nil {
		log.Debug("Could not emit CPU enhanced metrics")
		return
	}

	now := float64(time.Now().UnixNano()) / float64(time.Second)
	GenerateCPUEnhancedMetrics(GenerateCPUEnhancedMetricsArgs{
		UserCPUTimeMs:   cpuData.TotalUserTimeMs - cpuOffsetData.TotalUserTimeMs,
		SystemCPUTimeMs: cpuData.TotalSystemTimeMs - cpuOffsetData.TotalSystemTimeMs,
		Tags:            tags,
		Demux:           demux,
		Time:            now,
	})

	perCoreData := cpuData.IndividualCPUIdleTimes
	if perCoreData != nil {
		uptimeMs, err := proc.GetUptime("/proc/uptime")
		if err != nil {
			log.Debug("Could not emit CPU enhanced metrics")
			return
		}
		GenerateCPUUtilizationEnhancedMetrics(GenerateCPUUtilizationEnhancedMetricArgs{
			cpuData.IndividualCPUIdleTimes,
			cpuOffsetData.IndividualCPUIdleTimes,
			cpuData.TotalIdleTimeMs,
			cpuOffsetData.TotalIdleTimeMs,
			uptimeMs,
			uptimeOffset,
			tags,
			demux,
			now,
		})
	}

}

func GenerateCPUUtilizationEnhancedMetrics(args GenerateCPUUtilizationEnhancedMetricArgs) {
	var maxUtilizedCPUName, minUtilizedCPUName string
	maxIdleTime := 0.0
	minIdleTime := math.MaxFloat64
	for cpuName, cpuIdleTime := range args.IndividualCPUIdleTimes {
		adjustedIdleTime := cpuIdleTime - args.IndividualCPUIdleOffsetTimes[cpuName]
		// Maximally utilized CPU is the one with the least time spent in the idle process
		if adjustedIdleTime < minIdleTime {
			maxUtilizedCPUName = cpuName
			minIdleTime = adjustedIdleTime
		}
		// Minimally utilized CPU is the one with the most time spent in the idle process
		if adjustedIdleTime >= maxIdleTime {
			minUtilizedCPUName = cpuName
			maxIdleTime = adjustedIdleTime
		}
	}

	adjustedUptime := args.UptimeMs - args.UptimeOffsetMs

	maxUtilizedPercent := 100 * (adjustedUptime - minIdleTime) / adjustedUptime
	minUtilizedPercent := 100 * (adjustedUptime - maxIdleTime) / adjustedUptime

	numberCPUs := float64(len(args.IndividualCPUIdleTimes))
	adjustedIdleTime := args.IdleTimeMs - args.IdleTimeOffsetMs
	totalUtilizedPercent := 100 * (adjustedUptime*numberCPUs - adjustedIdleTime) / (adjustedUptime * numberCPUs)

	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       cpuTotalUtilizationMetric,
		Value:      totalUtilizedPercent,
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  args.Time,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       numCoresMetric,
		Value:      float64(len(args.IndividualCPUIdleTimes)),
		Mtype:      metrics.DistributionType,
		Tags:       args.Tags,
		SampleRate: 1,
		Timestamp:  args.Time,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       cpuMaxUtilizationMetric,
		Value:      maxUtilizedPercent,
		Mtype:      metrics.DistributionType,
		Tags:       append(args.Tags, fmt.Sprintf("cpu_name:%s", maxUtilizedCPUName)),
		SampleRate: 1,
		Timestamp:  args.Time,
	})
	args.Demux.AggregateSample(metrics.MetricSample{
		Name:       cpuMinUtilizationMetric,
		Value:      minUtilizedPercent,
		Mtype:      metrics.DistributionType,
		Tags:       append(args.Tags, fmt.Sprintf("cpu_name:%s", minUtilizedCPUName)),
		SampleRate: 1,
		Timestamp:  args.Time,
	})
}

// incrementEnhancedMetric sends an enhanced metric with a value of 1 to the metrics channel
func incrementEnhancedMetric(name string, tags []string, timestamp float64, demux aggregator.Demultiplexer, force bool) {
	// TODO - pass config here, instead of directly looking up var
	if !force && strings.ToLower(os.Getenv(enhancedMetricsEnvVar)) == "false" {
		return
	}
	demux.AggregateSample(metrics.MetricSample{
		Name:       name,
		Value:      1.0,
		Mtype:      metrics.DistributionType,
		Tags:       tags,
		SampleRate: 1,
		Timestamp:  timestamp,
	})
}

// calculateEstimatedCost returns the estimated cost in USD of a Lambda invocation
func calculateEstimatedCost(billedDurationMs float64, memorySizeMb float64, architecture string) float64 {
	billedDurationSeconds := billedDurationMs / 1000.0
	memorySizeGb := memorySizeMb / 1024.0
	gbSeconds := billedDurationSeconds * memorySizeGb
	// round the final float result because float math could have float point imprecision
	// on some arch. (i.e. 1.00000000000002 values)
	return math.Round((baseLambdaInvocationPrice+(gbSeconds*getLambdaPricePerGbSecond(architecture)))*10e12) / 10e12
}

// get the lambda price per Gb second based on the runtime platform
func getLambdaPricePerGbSecond(architecture string) float64 {
	switch architecture {
	case serverlessTags.ArmLambdaPlatform:
		// for arm64
		return armLambdaPricePerGbSecond
	default:
		// for x86 and amd64
		return x86LambdaPricePerGbSecond
	}
}
