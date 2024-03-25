// /*
// Copyright © 2022 - 2023 SUSE LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */
//

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/rancher-sandbox/cluster-api-provider-elemental/internal/agent/elementalcli (interfaces: Runner)
//
// Generated by this command:
//
//	mockgen -copyright_file=hack/boilerplate.go.txt -destination=internal/agent/elementalcli/runner_mocks.go -package=elementalcli github.com/rancher-sandbox/cluster-api-provider-elemental/internal/agent/elementalcli Runner
//
// Package elementalcli is a generated GoMock package.
package elementalcli

import (
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
)

// MockRunner is a mock of Runner interface.
type MockRunner struct {
	ctrl     *gomock.Controller
	recorder *MockRunnerMockRecorder
}

// MockRunnerMockRecorder is the mock recorder for MockRunner.
type MockRunnerMockRecorder struct {
	mock *MockRunner
}

// NewMockRunner creates a new mock instance.
func NewMockRunner(ctrl *gomock.Controller) *MockRunner {
	mock := &MockRunner{ctrl: ctrl}
	mock.recorder = &MockRunnerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRunner) EXPECT() *MockRunnerMockRecorder {
	return m.recorder
}

// Install mocks base method.
func (m *MockRunner) Install(arg0 Install) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Install", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Install indicates an expected call of Install.
func (mr *MockRunnerMockRecorder) Install(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Install", reflect.TypeOf((*MockRunner)(nil).Install), arg0)
}

// Reset mocks base method.
func (m *MockRunner) Reset(arg0 Reset) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Reset", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Reset indicates an expected call of Reset.
func (mr *MockRunnerMockRecorder) Reset(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Reset", reflect.TypeOf((*MockRunner)(nil).Reset), arg0)
}

// Upgrade mocks base method.
func (m *MockRunner) Upgrade(arg0 Upgrade) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Upgrade", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Upgrade indicates an expected call of Upgrade.
func (mr *MockRunnerMockRecorder) Upgrade(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Upgrade", reflect.TypeOf((*MockRunner)(nil).Upgrade), arg0)
}
