// Code generated by mockery v2.32.2. DO NOT EDIT.

package mocks

import (
	mock "github.com/stretchr/testify/mock"

	process "github.com/DataDog/agent-payload/v5/process"
)

// SysProbeUtil is an autogenerated mock type for the SysProbeUtil type
type SysProbeUtil struct {
	mock.Mock
}

// GetConnections provides a mock function with given fields: clientID
func (_m *SysProbeUtil) GetConnections(clientID string) (*process.Connections, error) {
	ret := _m.Called(clientID)

	var r0 *process.Connections
	var r1 error
	if rf, ok := ret.Get(0).(func(string) (*process.Connections, error)); ok {
		return rf(clientID)
	}
	if rf, ok := ret.Get(0).(func(string) *process.Connections); ok {
		r0 = rf(clientID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*process.Connections)
		}
	}

	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(clientID)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetConnectionsGRPC provides a mock function with given fields: clientID, unixPath
func (_m *SysProbeUtil) GetConnectionsGRPC(clientID string, unixPath string) (*process.Connections, error) {
	ret := _m.Called(clientID, unixPath)

	var r0 *process.Connections
	var r1 error
	if rf, ok := ret.Get(0).(func(string, string) (*process.Connections, error)); ok {
		return rf(clientID, unixPath)
	}
	if rf, ok := ret.Get(0).(func(string, string) *process.Connections); ok {
		r0 = rf(clientID, unixPath)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*process.Connections)
		}
	}

	if rf, ok := ret.Get(1).(func(string, string) error); ok {
		r1 = rf(clientID, unixPath)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetProcStats provides a mock function with given fields: pids
func (_m *SysProbeUtil) GetProcStats(pids []int32) (*process.ProcStatsWithPermByPID, error) {
	ret := _m.Called(pids)

	var r0 *process.ProcStatsWithPermByPID
	var r1 error
	if rf, ok := ret.Get(0).(func([]int32) (*process.ProcStatsWithPermByPID, error)); ok {
		return rf(pids)
	}
	if rf, ok := ret.Get(0).(func([]int32) *process.ProcStatsWithPermByPID); ok {
		r0 = rf(pids)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*process.ProcStatsWithPermByPID)
		}
	}

	if rf, ok := ret.Get(1).(func([]int32) error); ok {
		r1 = rf(pids)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetStats provides a mock function with given fields:
func (_m *SysProbeUtil) GetStats() (map[string]interface{}, error) {
	ret := _m.Called()

	var r0 map[string]interface{}
	var r1 error
	if rf, ok := ret.Get(0).(func() (map[string]interface{}, error)); ok {
		return rf()
	}
	if rf, ok := ret.Get(0).(func() map[string]interface{}); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(map[string]interface{})
		}
	}

	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Register provides a mock function with given fields: clientID
func (_m *SysProbeUtil) Register(clientID string) error {
	ret := _m.Called(clientID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(clientID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// NewSysProbeUtil creates a new instance of SysProbeUtil. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewSysProbeUtil(t interface {
	mock.TestingT
	Cleanup(func())
}) *SysProbeUtil {
	mock := &SysProbeUtil{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
