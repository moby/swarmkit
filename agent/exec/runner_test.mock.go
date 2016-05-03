// Automatically generated by MockGen. DO NOT EDIT!
// Source: runner.go

package exec

import (
	api "github.com/docker/swarm-v2/api"
	gomock "github.com/golang/mock/gomock"
	context "golang.org/x/net/context"
)

// Mock of Controller interface
type MockController struct {
	ctrl     *gomock.Controller
	recorder *_MockControllerRecorder
}

// Recorder for MockController (not exported)
type _MockControllerRecorder struct {
	mock *MockController
}

func NewMockController(ctrl *gomock.Controller) *MockController {
	mock := &MockController{ctrl: ctrl}
	mock.recorder = &_MockControllerRecorder{mock}
	return mock
}

func (_m *MockController) EXPECT() *_MockControllerRecorder {
	return _m.recorder
}

func (_m *MockController) Update(ctx context.Context, t *api.Task) error {
	ret := _m.ctrl.Call(_m, "Update", ctx, t)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Update(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Update", arg0, arg1)
}

func (_m *MockController) Prepare(ctx context.Context) error {
	ret := _m.ctrl.Call(_m, "Prepare", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Prepare(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Prepare", arg0)
}

func (_m *MockController) Start(ctx context.Context) error {
	ret := _m.ctrl.Call(_m, "Start", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Start(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Start", arg0)
}

func (_m *MockController) Wait(ctx context.Context) error {
	ret := _m.ctrl.Call(_m, "Wait", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Wait(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Wait", arg0)
}

func (_m *MockController) Shutdown(ctx context.Context) error {
	ret := _m.ctrl.Call(_m, "Shutdown", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Shutdown(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Shutdown", arg0)
}

func (_m *MockController) Terminate(ctx context.Context) error {
	ret := _m.ctrl.Call(_m, "Terminate", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Terminate(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Terminate", arg0)
}

func (_m *MockController) Remove(ctx context.Context) error {
	ret := _m.ctrl.Call(_m, "Remove", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Remove(arg0 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Remove", arg0)
}

func (_m *MockController) Close() error {
	ret := _m.ctrl.Call(_m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockControllerRecorder) Close() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Close")
}

// Mock of Reporter interface
type MockReporter struct {
	ctrl     *gomock.Controller
	recorder *_MockReporterRecorder
}

// Recorder for MockReporter (not exported)
type _MockReporterRecorder struct {
	mock *MockReporter
}

func NewMockReporter(ctrl *gomock.Controller) *MockReporter {
	mock := &MockReporter{ctrl: ctrl}
	mock.recorder = &_MockReporterRecorder{mock}
	return mock
}

func (_m *MockReporter) EXPECT() *_MockReporterRecorder {
	return _m.recorder
}

func (_m *MockReporter) Report(ctx context.Context, state api.TaskState, msg string) error {
	ret := _m.ctrl.Call(_m, "Report", ctx, state, msg)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockReporterRecorder) Report(arg0, arg1, arg2 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Report", arg0, arg1, arg2)
}
