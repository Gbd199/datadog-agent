// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf

// Package failure contains logic specific to TCP failed connection handling
package failure

import (
	"sync"
	"unsafe"

	ddebpf "github.com/DataDog/datadog-agent/pkg/ebpf"
	netebpf "github.com/DataDog/datadog-agent/pkg/network/ebpf"
	"github.com/DataDog/datadog-agent/pkg/telemetry"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

const failedConnConsumerModuleName = "network_tracer__ebpf"

var allowListErrs = map[uint32]struct{}{
	104: {}, // Connection reset by peer
	110: {}, // Connection timed out
	111: {}, // Connection refused
}

// Telemetry
var failedConnConsumerTelemetry = struct {
	eventsReceived telemetry.Counter
	eventsLost     telemetry.Counter
}{
	telemetry.NewCounter(failedConnConsumerModuleName, "failed_conn_polling_received", []string{}, "Counter measuring the number of closed connections received"),
	telemetry.NewCounter(failedConnConsumerModuleName, "failed_conn_polling_lost", []string{}, "Counter measuring the number of closed connection batches lost (were transmitted from ebpf but never received)"),
}

// TCPFailedConnConsumer consumes failed connection events from the kernel
type TCPFailedConnConsumer struct {
	eventHandler ddebpf.EventHandler
	once         sync.Once
	closed       chan struct{}
	FailedConns  *FailedConns
}

// NewFailedConnConsumer creates a new TCPFailedConnConsumer
func NewFailedConnConsumer(eventHandler ddebpf.EventHandler) *TCPFailedConnConsumer {
	return &TCPFailedConnConsumer{
		eventHandler: eventHandler,
		closed:       make(chan struct{}),
		FailedConns:  NewFailedConns(),
	}
}

// Stop stops the consumer
func (c *TCPFailedConnConsumer) Stop() {
	if c == nil {
		return
	}
	c.eventHandler.Stop()
	c.once.Do(func() {
		close(c.closed)
	})
}

func (c *TCPFailedConnConsumer) extractConn(data []byte) {
	c.FailedConns.Lock()
	defer c.FailedConns.Unlock()
	failedConn := (*netebpf.FailedConn)(unsafe.Pointer(&data[0]))
	failedConnConsumerTelemetry.eventsReceived.Inc()

	if _, exists := allowListErrs[failedConn.Reason]; !exists {
		//log.Debugf("Ignoring failed connection with reason: %d", failedConn.Reason)
		return
	}
	//log.Errorf("Adding failed connection to map: %+v", failedConn.Reason)
	stats, ok := c.FailedConns.FailedConnMap[failedConn.Tup]
	if !ok {
		stats = &FailedConnStats{
			CountByErrCode: make(map[uint32]uint32),
		}
		c.FailedConns.FailedConnMap[failedConn.Tup] = stats
	}
	stats.CountByErrCode[failedConn.Reason]++
	//log.Errorf("Failed connection map: %+v", c.FailedConns.FailedConnMap)
}

// Start starts the consumer
func (c *TCPFailedConnConsumer) Start() {
	if c == nil {
		return
	}

	var (
		lostSamplesCount uint64
	)

	go func() {
		dataChannel := c.eventHandler.DataChannel()
		lostChannel := c.eventHandler.LostChannel()
		for {
			select {

			case <-c.closed:
				return
			case dataEvent, ok := <-dataChannel:
				if !ok {
					return
				}

				l := len(dataEvent.Data)
				switch {
				case l >= netebpf.SizeofFailedConn:
					c.extractConn(dataEvent.Data)
				default:
					log.Errorf("unknown type received from buffer, skipping. data size=%d, expecting %d", len(dataEvent.Data), netebpf.SizeofFailedConn)
					continue
				}
				failedConnConsumerTelemetry.eventsLost.Inc()
				dataEvent.Done()
			// lost events only occur when using perf buffers
			case lc, ok := <-lostChannel:
				if !ok {
					return
				}
				failedConnConsumerTelemetry.eventsLost.Add(float64(lc))
				lostSamplesCount += lc
			}
		}
	}()
}
