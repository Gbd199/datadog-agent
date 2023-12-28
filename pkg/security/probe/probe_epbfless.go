// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux

// Package probe holds probe related files
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"google.golang.org/grpc"

	"github.com/DataDog/datadog-go/v5/statsd"

	"github.com/DataDog/datadog-agent/pkg/security/config"
	"github.com/DataDog/datadog-agent/pkg/security/probe/kfilters"
	"github.com/DataDog/datadog-agent/pkg/security/proto/ebpfless"
	"github.com/DataDog/datadog-agent/pkg/security/resolvers"
	"github.com/DataDog/datadog-agent/pkg/security/resolvers/process"
	"github.com/DataDog/datadog-agent/pkg/security/secl/compiler/eval"
	"github.com/DataDog/datadog-agent/pkg/security/secl/model"
	"github.com/DataDog/datadog-agent/pkg/security/secl/rules"
	"github.com/DataDog/datadog-agent/pkg/security/seclog"
	"github.com/DataDog/datadog-agent/pkg/security/serializers"
	"github.com/DataDog/datadog-agent/pkg/util/native"
)

type client struct {
	conn             net.Conn
	probe            *EBPFLessProbe
	seqNum           uint64
	nsID             uint64
	containerContext *ebpfless.ContainerContext
	entrypointArgs   []string
}

type clientMsg struct {
	ebpfless.Message
	*client
}

// EBPFLessProbe defines an eBPF less probe
type EBPFLessProbe struct {
	sync.Mutex

	Resolvers *resolvers.EBPFLessResolvers

	// Constants and configuration
	opts         Opts
	config       *config.Config
	statsdClient statsd.ClientInterface

	// internals
	event         *model.Event
	server        *grpc.Server
	probe         *Probe
	ctx           context.Context
	cancelFnc     context.CancelFunc
	fieldHandlers *EBPFLessFieldHandlers
	buf           []byte
	clients       map[net.Conn]*client
}

func (p *EBPFLessProbe) handleClientMsg(cl *client, msg *ebpfless.Message) {
	if cl.seqNum != msg.SeqNum {
		seclog.Errorf("communication out of sync %d vs %d", cl.seqNum, msg.SeqNum)
	}
	cl.seqNum++

	switch msg.Type {
	case ebpfless.MessageTypeHello:
		if cl.nsID == 0 {
			cl.nsID = msg.Hello.NSID
			cl.containerContext = msg.Hello.ContainerContext
			cl.entrypointArgs = msg.Hello.EntrypointArgs
			if cl.containerContext != nil {
				seclog.Infof("tracing started for container ID [%s] (Name: [%s]) with entrypoint %q", cl.containerContext.ID, cl.containerContext.Name, cl.entrypointArgs)
			}
		}
	case ebpfless.MessageTypeSyscall:
		p.handleSyscallMsg(cl, msg.Syscall)
	}
}

func (p *EBPFLessProbe) handleSyscallMsg(cl *client, syscallMsg *ebpfless.SyscallMsg) {
	event := p.zeroEvent()
	event.NSID = cl.nsID

	switch syscallMsg.Type {
	case ebpfless.SyscallTypeExec:
		event.Type = uint32(model.ExecEventType)
		entry := p.Resolvers.ProcessResolver.AddExecEntry(process.CacheResolverKey{Pid: syscallMsg.PID, NSID: cl.nsID}, syscallMsg.Exec.Filename, syscallMsg.Exec.Args, syscallMsg.Exec.Envs, cl.containerContext.ID)

		if syscallMsg.Exec.Credentials != nil {
			entry.Credentials.UID = syscallMsg.Exec.Credentials.UID
			entry.Credentials.EUID = syscallMsg.Exec.Credentials.EUID
			entry.Credentials.GID = syscallMsg.Exec.Credentials.GID
			entry.Credentials.EGID = syscallMsg.Exec.Credentials.EGID
		}

		event.Exec.Process = &entry.Process
	case ebpfless.SyscallTypeFork:
		event.Type = uint32(model.ForkEventType)
		p.Resolvers.ProcessResolver.AddForkEntry(process.CacheResolverKey{Pid: syscallMsg.PID, NSID: cl.nsID}, syscallMsg.Fork.PPID)
	case ebpfless.SyscallTypeOpen:
		event.Type = uint32(model.FileOpenEventType)
		event.Open.Retval = syscallMsg.Retval
		event.Open.File.PathnameStr = syscallMsg.Open.Filename
		event.Open.File.BasenameStr = filepath.Base(syscallMsg.Open.Filename)
		event.Open.Flags = syscallMsg.Open.Flags
		event.Open.Mode = syscallMsg.Open.Mode
	case ebpfless.SyscallTypeSetUID:
		p.Resolvers.ProcessResolver.UpdateUID(process.CacheResolverKey{Pid: syscallMsg.PID, NSID: cl.nsID}, syscallMsg.SetUID.UID, syscallMsg.SetUID.EUID)

	case ebpfless.SyscallTypeSetGID:
		p.Resolvers.ProcessResolver.UpdateGID(process.CacheResolverKey{Pid: syscallMsg.PID, NSID: cl.nsID}, syscallMsg.SetGID.GID, syscallMsg.SetGID.EGID)
	}

	// container context
	event.ContainerContext.ID = cl.containerContext.ID
	event.ContainerContext.CreatedAt = cl.containerContext.CreatedAt
	event.ContainerContext.Tags = []string{
		"image_name:" + cl.containerContext.Name,
	}

	// use ProcessCacheEntry process context as process context
	event.ProcessCacheEntry = p.Resolvers.ProcessResolver.Resolve(process.CacheResolverKey{Pid: syscallMsg.PID, NSID: cl.nsID})
	if event.ProcessCacheEntry == nil {
		event.ProcessCacheEntry = model.NewPlaceholderProcessCacheEntry(syscallMsg.PID, syscallMsg.PID, false)
	}
	event.ProcessContext = &event.ProcessCacheEntry.ProcessContext

	if syscallMsg.Type == ebpfless.SyscallTypeExit {
		event.Type = uint32(model.ExitEventType)
		exitTime := time.Now() // TODO: fix timestamp
		event.ProcessContext.ExitTime = exitTime
		event.Exit.Process = &event.ProcessCacheEntry.Process
		event.Exit.Cause = uint32(model.ExitExited) // TODO: fix cause
		event.Exit.Code = 0                         // TODO: fix code
		defer p.Resolvers.ProcessResolver.DeleteEntry(process.CacheResolverKey{Pid: syscallMsg.PID, NSID: cl.nsID}, exitTime)
	}

	p.DispatchEvent(event)
}

// DispatchEvent sends an event to the probe event handler
func (p *EBPFLessProbe) DispatchEvent(event *model.Event) {
	traceEvent("Dispatching event %s", func() ([]byte, model.EventType, error) {
		eventJSON, err := serializers.MarshalEvent(event)
		return eventJSON, event.GetEventType(), err
	})

	// send event to wildcard handlers, like the CWS rule engine, first
	p.probe.sendEventToWildcardHandlers(event)

	// send event to specific event handlers, like the event monitor consumers, subsequently
	p.probe.sendEventToSpecificEventTypeHandlers(event)
}

// Init the probe
func (p *EBPFLessProbe) Init() error {
	if err := p.Resolvers.Start(p.ctx); err != nil {
		return err
	}

	return nil
}

// Stop the probe
func (p *EBPFLessProbe) Stop() {
	p.server.GracefulStop()
	p.cancelFnc()
}

// Close the probe
func (p *EBPFLessProbe) Close() error {
	p.Lock()
	defer p.Unlock()

	for conn := range p.clients {
		conn.Close()
		delete(p.clients, conn)
	}

	return nil
}

func (p *EBPFLessProbe) readMsg(conn net.Conn, msg *ebpfless.Message) error {
	sizeBuf := make([]byte, 4)

	n, err := conn.Read(sizeBuf)
	if err != nil {
		return err
	}

	if n < 4 {
		// TODO return EOF
		return errors.New("not enough data")
	}

	size := native.Endian.Uint32(sizeBuf)
	if size > 64*1024 {
		return fmt.Errorf("data overflow the max size: %d", size)
	}

	if cap(p.buf) < int(size) {
		p.buf = make([]byte, size)
	}

	n, err = conn.Read(p.buf[:size])
	if err != nil {
		return err
	}

	return msgpack.Unmarshal(p.buf[0:n], msg)
}

// GetClientsCount returns the number of connected clients
func (p *EBPFLessProbe) GetClientsCount() int {
	p.Lock()
	defer p.Unlock()
	return len(p.clients)
}

func (p *EBPFLessProbe) handleNewClient(conn net.Conn, ch chan clientMsg) {
	client := &client{
		conn:  conn,
		probe: p,
	}

	p.Lock()
	p.clients[conn] = client
	p.Unlock()

	seclog.Debugf("new connection from: %v", conn.RemoteAddr())

	go func() {
		msg := clientMsg{
			client: client,
		}
		for {
			msg.Reset()
			if err := p.readMsg(conn, &msg.Message); err != nil {
				if errors.Is(err, io.EOF) {
					seclog.Debugf("connection closed by client: %v", conn.RemoteAddr())
				} else {
					seclog.Warnf("error while reading message: %v", err)
				}

				p.Lock()
				delete(p.clients, conn)
				p.Unlock()

				if client.containerContext != nil {
					seclog.Infof("tracing stopped for container ID [%s] (Name: [%s])", client.containerContext.ID, client.containerContext.Name)
				}

				return
			}

			ch <- msg

		}
	}()
}

// Start the probe
func (p *EBPFLessProbe) Start() error {
	family, address := config.GetFamilyAddress(p.config.RuntimeSecurity.EBPFLessSocket)
	_ = family

	tcpAddr, err := net.ResolveTCPAddr("tcp4", address)
	if err != nil {
		return err
	}

	// Start listening for TCP connections on the given address
	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return err
	}

	ch := make(chan clientMsg, 100)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				seclog.Errorf("unable to accept new connection")
				continue
			}

			p.handleNewClient(conn, ch)
		}
	}()

	go func() {
		for msg := range ch {
			p.handleClientMsg(msg.client, &msg.Message)
		}
	}()

	seclog.Infof("starting listening for ebpf less events on : %s", p.config.RuntimeSecurity.EBPFLessSocket)

	return nil
}

// Snapshot the already existing entities
func (p *EBPFLessProbe) Snapshot() error {
	return nil
}

// Setup the probe
func (p *EBPFLessProbe) Setup() error {
	return nil
}

// OnNewDiscarder handles discarders
func (p *EBPFLessProbe) OnNewDiscarder(_ *rules.RuleSet, _ *model.Event, _ eval.Field, _ eval.EventType) {
}

// NewModel returns a new Model
func (p *EBPFLessProbe) NewModel() *model.Model {
	return NewEBPFLessModel()
}

// SendStats send the stats
func (p *EBPFLessProbe) SendStats() error {
	return nil
}

// DumpDiscarders dump the discarders
func (p *EBPFLessProbe) DumpDiscarders() (string, error) {
	return "", errors.New("not supported")
}

// FlushDiscarders flush the discarders
func (p *EBPFLessProbe) FlushDiscarders() error {
	return nil
}

// ApplyRuleSet applies the new ruleset
func (p *EBPFLessProbe) ApplyRuleSet(_ *rules.RuleSet) (*kfilters.ApplyRuleSetReport, error) {
	return &kfilters.ApplyRuleSetReport{}, nil
}

// HandleActions handles the rule actions
func (p *EBPFLessProbe) HandleActions(_ *eval.Context, _ *rules.Rule) {}

// NewEvent returns a new event
func (p *EBPFLessProbe) NewEvent() *model.Event {
	return NewEBPFLessEvent(p.fieldHandlers)
}

// GetFieldHandlers returns the field handlers
func (p *EBPFLessProbe) GetFieldHandlers() model.FieldHandlers {
	return p.fieldHandlers
}

// DumpProcessCache dumps the process cache
func (p *EBPFLessProbe) DumpProcessCache(withArgs bool) (string, error) {
	return p.Resolvers.ProcessResolver.Dump(withArgs)
}

// AddDiscarderPushedCallback add a callback to the list of func that have to be called when a discarder is pushed to kernel
func (p *EBPFLessProbe) AddDiscarderPushedCallback(_ DiscarderPushedCallback) {}

// GetEventTags returns the event tags
func (p *EBPFLessProbe) GetEventTags(containerID string) []string {
	return p.Resolvers.TagsResolver.Resolve(containerID)
}

func (p *EBPFLessProbe) zeroEvent() *model.Event {
	p.event.Zero()
	p.event.FieldHandlers = p.fieldHandlers
	p.event.Origin = "ebpfless"
	return p.event
}

// NewEBPFLessProbe returns a new eBPF less probe
func NewEBPFLessProbe(probe *Probe, config *config.Config, opts Opts) (*EBPFLessProbe, error) {
	opts.normalize()

	ctx, cancelFnc := context.WithCancel(context.Background())

	var grpcOpts []grpc.ServerOption
	p := &EBPFLessProbe{
		probe:        probe,
		config:       config,
		opts:         opts,
		statsdClient: opts.StatsdClient,
		server:       grpc.NewServer(grpcOpts...),
		ctx:          ctx,
		cancelFnc:    cancelFnc,
		buf:          make([]byte, 4096),
		clients:      make(map[net.Conn]*client),
	}

	resolversOpts := resolvers.Opts{
		TagsResolver: opts.TagsResolver,
	}

	var err error
	p.Resolvers, err = resolvers.NewEBPFLessResolvers(config, p.statsdClient, probe.scrubber, resolversOpts)
	if err != nil {
		return nil, err
	}

	p.fieldHandlers = &EBPFLessFieldHandlers{resolvers: p.Resolvers}

	p.event = p.NewEvent()

	// be sure to zero the probe event before everything else
	p.zeroEvent()

	return p, nil
}
