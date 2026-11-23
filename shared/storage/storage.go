// Package storage defines durable center state boundaries. Agent execution WAL
// stays local so losing the center/database never causes a repeated Docker write.
package storage

import "encoding/json"

type Log interface {
	Append(any, int64) error
	Scan(func(json.RawMessage) error) error
	Healthy() bool
	Usage() (int64, int64)
	Close()
}
type Backend interface {
	LoadDocument(string) ([]byte, bool, error)
	SaveDocument(string, []byte) error
	OpenLog(string, int64, func(json.RawMessage) error) (Log, error)
	ReadLog(string, string, int) ([]json.RawMessage, error)
	SaveSnapshot(string, []byte) error
	LoadSnapshots() (map[string]json.RawMessage, error)
	Healthy() bool
}
