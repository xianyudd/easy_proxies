package boxmgr

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"easy_proxies/internal/config"
)

// LockConfigEdits coordinates management settings writes with source commits.
// Callers acquire this before their own configuration mutex, never after it.
func (m *Manager) LockConfigEdits() func() { m.commitMu.Lock(); return m.commitMu.Unlock }

func (m *Manager) ConfigSnapshot() *config.Config {
	m.commitMu.Lock()
	defer m.commitMu.Unlock()
	return m.configSnapshotCommitted()
}
func (m *Manager) configSnapshotCommitted() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.copyConfigLocked()
}
func (m *Manager) LocalSources(ctx context.Context) (config.LocalSourcesState, error) {
	if err := ctx.Err(); err != nil {
		return config.LocalSourcesState{}, err
	}
	c := m.ConfigSnapshot()
	if c == nil {
		return config.LocalSourcesState{}, errConfigUnavailable
	}
	return c.ReadLocalSourcesState()
}

// PreviewLocalSource reads one consistent editing/active view without constructing
// outbounds, probing, DNS or returning any other owner's connection details.
func (m *Manager) PreviewLocalSource(ctx context.Context, id string, revision *int, content string) (config.LocalSourcePreview, error) {
	out := config.LocalSourcePreview{}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	parsed, err := config.ParseLocalSourceNodes(content)
	if err != nil {
		return out, err
	}
	m.commitMu.Lock()
	defer m.commitMu.Unlock()
	c := m.configSnapshotCommitted()
	if c == nil {
		return out, errConfigUnavailable
	}
	state, err := c.ReadLocalSourcesState()
	if err != nil {
		return out, err
	}
	if revision != nil && *revision != state.Revision {
		return out, config.ErrLocalSourceConflict
	}
	if id != "" {
		found := false
		for _, s := range state.Sources {
			if s.ID == id {
				found = true
			}
		}
		if !found {
			return out, config.ErrLocalSourceNotFound
		}
	}
	key := func(uri string) string { return strings.SplitN(strings.TrimSpace(uri), "#", 2)[0] }
	other := func(sources []config.LocalSource) map[string]bool {
		keys := map[string]bool{}
		for _, s := range sources {
			if s.ID == id || !s.Enabled {
				continue
			}
			for _, n := range s.Nodes {
				keys[key(n.URI)] = true
			}
		}
		return keys
	}
	active := other(c.LocalSources)
	editing := other(state.Sources)
	nonlocal := map[string]bool{}
	for _, n := range config.ExpandNodeOrigins(c.Nodes) {
		if n.Source != config.NodeSourceLocal {
			nonlocal[key(n.URI)] = true
		}
	}
	out.MemberCount = len(parsed.Nodes)
	out.Duplicates = parsed.Duplicates
	out.Protocols = map[string]int{}
	out.Revision = state.Revision
	for _, n := range parsed.Nodes {
		k := key(n.URI)
		scheme, _, _ := strings.Cut(n.URI, "://")
		out.Protocols[scheme]++
		if nonlocal[k] {
			out.SharedNonLocal++
		}
		if active[k] {
			out.SharedActive++
		}
		if state.Pending && editing[k] {
			out.SharedDraft++
		}
		if nonlocal[k] || editing[k] {
			out.SharedCount++
		}
	}
	return out, nil
}

func (m *Manager) SaveLocalSource(ctx context.Context, id string, revision int, name string, enabled bool, content string) (config.LocalSourcesState, error) {
	if err := ctx.Err(); err != nil {
		return config.LocalSourcesState{}, err
	}
	var members []config.LocalSourceNode
	if content != "" || id == "" {
		parsed, err := config.ParseLocalSourceNodes(content)
		if err != nil {
			return config.LocalSourcesState{}, err
		}
		members = parsed.Nodes
	}
	if !m.commitMu.TryLock() {
		return config.LocalSourcesState{}, config.ErrLocalSourceBusy
	}
	defer m.commitMu.Unlock()
	c := m.configSnapshotCommitted()
	if c == nil {
		return config.LocalSourcesState{}, errConfigUnavailable
	}
	state, err := c.ReadLocalSourcesState()
	if err != nil {
		return state, err
	}
	if revision != state.Revision {
		return state, config.ErrLocalSourceConflict
	}
	source := config.LocalSource{ID: id, Name: strings.TrimSpace(name), Enabled: enabled, Nodes: members}
	if id == "" {
		source.ID = config.NewLocalSourceID()
		state.Sources = append(state.Sources, source)
	} else {
		found := false
		for i := range state.Sources {
			if state.Sources[i].ID == id {
				if content == "" {
					source.Nodes = state.Sources[i].Nodes
				}
				state.Sources[i] = source
				found = true
				break
			}
		}
		if !found {
			return state, config.ErrLocalSourceNotFound
		}
	}
	// Cross-source/key collisions are invalid even before applying the draft.
	if _, err = config.ComposeLocalSources(c.Nodes, state.Sources); err != nil {
		return state, err
	}
	return c.SaveLocalSourcesDraft(revision, state.Sources)
}
func (m *Manager) DeleteLocalSource(ctx context.Context, id string, revision int) (config.LocalSourcesState, error) {
	if err := ctx.Err(); err != nil {
		return config.LocalSourcesState{}, err
	}
	if !m.commitMu.TryLock() {
		return config.LocalSourcesState{}, config.ErrLocalSourceBusy
	}
	defer m.commitMu.Unlock()
	c := m.configSnapshotCommitted()
	if c == nil {
		return config.LocalSourcesState{}, errConfigUnavailable
	}
	state, err := c.ReadLocalSourcesState()
	if err != nil {
		return state, err
	}
	if revision != state.Revision {
		return state, config.ErrLocalSourceConflict
	}
	found := false
	for i, s := range state.Sources {
		if s.ID == id {
			state.Sources = append(state.Sources[:i], state.Sources[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return state, config.ErrLocalSourceNotFound
	}
	return c.SaveLocalSourcesDraft(revision, state.Sources)
}
func (m *Manager) ApplyLocalSources(ctx context.Context, revision int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !m.commitMu.TryLock() {
		return config.ErrLocalSourceBusy
	}
	defer m.commitMu.Unlock()
	old := m.configSnapshotCommitted()
	if old == nil {
		return errConfigUnavailable
	}
	state, err := old.ReadLocalSourcesState()
	if err != nil {
		return err
	}
	if !state.Pending || state.Revision != revision || state.ActiveRevision != old.LocalSourcesRevision {
		return config.ErrLocalSourceConflict
	}
	candidate := m.configSnapshotCommitted()
	candidate.LocalSources = config.CloneLocalSources(state.Sources)
	candidate.LocalSourcesRevision = revision
	candidate.LocalSourcesDraft = nil
	if err = candidate.ComposeLocalNodes(); err != nil {
		return err
	}
	if len(candidate.Nodes) == 0 {
		return errors.New("cannot apply a configuration with zero nodes")
	}
	if err = m.reloadWithPortMapCommitted(candidate, old.BuildPortMap()); err != nil {
		if m.State() != StateRunning {
			return errors.New("local source application failed; runtime rollback also failed")
		}
		return errors.New("local source application failed; previous runtime restored and draft retained")
	}
	candidate.RecordLocalPorts()
	if err = candidate.CommitLocalSources(revision, candidate.LocalSources); err != nil {
		if errors.Is(err, config.ErrLocalCommitUncertain) {
			return errors.New("new configuration is active and committed; disk durability could not be confirmed")
		}
		rollback := m.reloadCommitted(old)
		if rollback != nil {
			return errors.New("local source persistence failed; runtime rollback also failed")
		}
		return errors.New("local source persistence failed; previous runtime restored and draft retained")
	}
	return nil
}

// CommitSubscriptionNodes accepts fetched source data, never a stale full
// config. Network work is performed by the caller outside the commit gate.
func (m *Manager) CommitSubscriptionNodes(expected []string, nodes []config.NodeConfig) error {
	m.commitMu.Lock()
	defer m.commitMu.Unlock()
	c := m.configSnapshotCommitted()
	if c == nil {
		return errConfigUnavailable
	}
	if !reflect.DeepEqual(c.Subscriptions, expected) {
		return config.ErrLocalSourceConflict
	}
	base := make([]config.NodeConfig, 0, len(c.Nodes)+len(nodes))
	for _, n := range config.ExpandNodeOrigins(c.Nodes) {
		if n.Source != config.NodeSourceSubscription && n.Source != config.NodeSourceLocal {
			base = append(base, n)
		}
	}
	subs := cloneNodes(nodes)
	for i := range subs {
		subs[i].Source = config.NodeSourceSubscription
		subs[i].Port = 0
	}
	c.Nodes = append(base, subs...)
	if err := m.reloadWithPortMapCommitted(c, m.CurrentPortMap()); err != nil {
		return err
	}
	path := c.NodesFile
	if path == "" {
		path = filepath.Join(filepath.Dir(c.FilePath()), "nodes.txt")
	}
	if err := config.WriteSubscriptionNodes(path, subs); err != nil {
		return fmt.Errorf("persist subscription cache: %w", err)
	}
	return nil
}

// ReloadFreeProxySources uses only the latest active settings and sources.
// Ordinary reload does not load local_sources_draft into runtime.
func (m *Manager) ReloadFreeProxySources(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fetched := m.ConfigSnapshot()
	if fetched == nil {
		return errConfigUnavailable
	}
	expected := fetched
	freeNodes, err := fetched.FetchFreeProxySourceNodes()
	if err != nil {
		return err
	}
	m.commitMu.Lock()
	defer m.commitMu.Unlock()
	current := m.configSnapshotCommitted()
	if !reflect.DeepEqual(expected.FreeProxySources, current.FreeProxySources) || !reflect.DeepEqual(expected.FreeProxyFilter, current.FreeProxyFilter) || !reflect.DeepEqual(expected.FreeProxyCache, current.FreeProxyCache) {
		return config.ErrLocalSourceConflict
	}
	nodes := make([]config.NodeConfig, 0, len(current.Nodes))
	for _, n := range config.ExpandNodeOrigins(current.Nodes) {
		if n.Source != config.NodeSourceFreeProxy {
			nodes = append(nodes, n)
		}
	}
	nodes = append(nodes, freeNodes...)
	current.Nodes = nodes
	return m.reloadWithPortMapCommitted(current, m.CurrentPortMap())
}
