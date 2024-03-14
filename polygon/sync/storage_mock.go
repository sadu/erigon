// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/ledgerwatch/erigon/polygon/sync (interfaces: Storage)
//
// Generated by this command:
//
//	mockgen -destination=./storage_mock.go -package=sync . Storage
//

// Package sync is a generated GoMock package.
package sync

import (
	context "context"
	reflect "reflect"

	types "github.com/ledgerwatch/erigon/core/types"
	gomock "go.uber.org/mock/gomock"
)

// MockStorage is a mock of Storage interface.
type MockStorage struct {
	ctrl     *gomock.Controller
	recorder *MockStorageMockRecorder
}

// MockStorageMockRecorder is the mock recorder for MockStorage.
type MockStorageMockRecorder struct {
	mock *MockStorage
}

// NewMockStorage creates a new mock instance.
func NewMockStorage(ctrl *gomock.Controller) *MockStorage {
	mock := &MockStorage{ctrl: ctrl}
	mock.recorder = &MockStorageMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockStorage) EXPECT() *MockStorageMockRecorder {
	return m.recorder
}

// Flush mocks base method.
func (m *MockStorage) Flush(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Flush", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Flush indicates an expected call of Flush.
func (mr *MockStorageMockRecorder) Flush(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Flush", reflect.TypeOf((*MockStorage)(nil).Flush), arg0)
}

// InsertBlocks mocks base method.
func (m *MockStorage) InsertBlocks(arg0 context.Context, arg1 []*types.Block) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "InsertBlocks", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// InsertBlocks indicates an expected call of InsertBlocks.
func (mr *MockStorageMockRecorder) InsertBlocks(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InsertBlocks", reflect.TypeOf((*MockStorage)(nil).InsertBlocks), arg0, arg1)
}

// Run mocks base method.
func (m *MockStorage) Run(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Run indicates an expected call of Run.
func (mr *MockStorageMockRecorder) Run(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockStorage)(nil).Run), arg0)
}
