package store

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"
)

type EventType string

const (
	EventPut    EventType = "put"
	EventDelete EventType = "delete"
)

type Event struct {
	Type     EventType
	Key      string
	Value    []byte
	Revision int64
}

type TreeNode struct {
	Name     string               `json:"name"`
	Value    json.RawMessage      `json:"value,omitempty"`
	Children map[string]*TreeNode `json:"children,omitempty"`
}

type StateStore struct {
	mu       sync.RWMutex
	root     *TreeNode
	revision int64
	ready    bool
	log      *zap.Logger
}

func New(log *zap.Logger) *StateStore {
	return &StateStore{
		root: &TreeNode{
			Name:     "",
			Children: make(map[string]*TreeNode),
		},
		log: log,
	}
}

func (s *StateStore) LoadSnapshot(items map[string][]byte, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Debug("loading snapshot", zap.Int("items", len(items)), zap.Int64("revision", revision))

	s.root = &TreeNode{
		Name:     "",
		Children: make(map[string]*TreeNode),
	}

	for key, value := range items {
		s.insertLocked(key, value)
	}

	s.revision = revision
	s.ready = true

	s.log.Debug("snapshot loaded", zap.Int64("revision", revision))
	return nil
}

func (s *StateStore) Apply(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch event.Type {
	case EventPut:
		s.insertLocked(event.Key, event.Value)
	case EventDelete:
		s.deleteLocked(event.Key)
	}

	s.revision = event.Revision

	s.log.Debug(
		"store event applied",
		zap.String("type", string(event.Type)),
		zap.String("key", event.Key),
		zap.Int64("revision", event.Revision),
	)

	return nil
}

func (s *StateStore) Snapshot() *TreeNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneNode(s.root)
}

func (s *StateStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node := s.findLocked(key)
	if node == nil || len(node.Value) == 0 {
		return nil, false
	}

	out := make([]byte, len(node.Value))
	copy(out, node.Value)
	return out, true
}

func (s *StateStore) ListChildren(prefix string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node := s.findLocked(prefix)
	if node == nil {
		return nil
	}

	children := make([]string, 0, len(node.Children))
	for name := range node.Children {
		children = append(children, name)
	}
	sort.Strings(children)

	return children
}

func (s *StateStore) Prefix(prefix string) map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node := s.findLocked(prefix)
	if node == nil {
		return nil
	}

	out := make(map[string][]byte)
	walk(prefix, node, out)
	return out
}

func (s *StateStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.ready
}

func (s *StateStore) Revision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.revision
}

func (s *StateStore) insertLocked(key string, value []byte) {
	node := s.root
	for _, part := range split(key) {
		child := node.Children[part]
		if child == nil {
			child = &TreeNode{
				Name:     part,
				Children: make(map[string]*TreeNode),
			}
			node.Children[part] = child
		}
		node = child
	}

	node.Value = normalizeJSON(value)
}

func (s *StateStore) deleteLocked(key string) {
	parts := split(key)
	if len(parts) == 0 {
		return
	}

	node := s.root
	for _, part := range parts[:len(parts)-1] {
		node = node.Children[part]
		if node == nil {
			return
		}
	}

	delete(node.Children, parts[len(parts)-1])
}

func (s *StateStore) findLocked(key string) *TreeNode {
	node := s.root
	for _, part := range split(key) {
		node = node.Children[part]
		if node == nil {
			return nil
		}
	}

	return node
}

func split(key string) []string {
	key = strings.Trim(key, "/")
	if key == "" {
		return nil
	}

	return strings.Split(key, "/")
}

func normalizeJSON(value []byte) json.RawMessage {
	if json.Valid(value) {
		out := make([]byte, len(value))
		copy(out, value)
		return out
	}

	encoded, _ := json.Marshal(string(value))
	return encoded
}

func cloneNode(node *TreeNode) *TreeNode {
	if node == nil {
		return nil
	}

	out := &TreeNode{
		Name:     node.Name,
		Children: make(map[string]*TreeNode, len(node.Children)),
	}

	if len(node.Value) > 0 {
		out.Value = append(json.RawMessage(nil), node.Value...)
	}

	for name, child := range node.Children {
		out.Children[name] = cloneNode(child)
	}

	if len(out.Children) == 0 {
		out.Children = nil
	}

	return out
}

func walk(prefix string, node *TreeNode, out map[string][]byte) {
	if len(node.Value) > 0 {
		out[prefix] = append([]byte(nil), node.Value...)
	}

	for name, child := range node.Children {
		childPrefix := strings.TrimRight(prefix, "/") + "/" + name
		walk(childPrefix, child, out)
	}
}
