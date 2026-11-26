package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var ErrLocalSourceConflict = errors.New("local source revision conflict")
var ErrLocalSourceBusy = errors.New("local source application in progress")
var ErrLocalSourceNotFound = errors.New("local source not found")

type LocalSourceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type LocalSourcesState struct {
	Revision       int           `json:"revision"`
	ActiveRevision int           `json:"active_revision"`
	Pending        bool          `json:"pending"`
	Applying       bool          `json:"applying"`
	Sources        []LocalSource `json:"sources"`
}

func (c *Config) LocalState() LocalSourcesState {
	s := LocalSourcesState{Revision: c.LocalSourcesRevision, ActiveRevision: c.LocalSourcesRevision, Sources: CloneLocalSources(c.LocalSources)}
	if d := c.LocalSourcesDraft; d != nil {
		s.Revision = d.Revision
		s.Pending = true
		s.Sources = CloneLocalSources(d.Sources)
	}
	if s.Sources == nil {
		s.Sources = []LocalSource{}
	}
	return s
}
func connectionText(uri string) string { return strings.SplitN(strings.TrimSpace(uri), "#", 2)[0] }

// ComposeLocalSources neither mutates inputs nor checks reachability. Existing
// non-local ownership wins; local memberships are additive references.
func ComposeLocalSources(base []NodeConfig, sources []LocalSource) ([]NodeConfig, error) {
	if err := ValidateLocalSources(sources); err != nil {
		return nil, err
	}
	out := make([]NodeConfig, 0, len(base))
	index := map[string]int{}
	for _, n := range ExpandNodeOrigins(base) {
		if n.Source == NodeSourceLocal {
			continue
		}
		n.LocalSources = nil
		if i, ok := index[n.NodeKey()]; ok {
			if connectionText(out[i].URI) != connectionText(n.URI) {
				return nil, errors.New("conflicting base node key")
			}
			if out[i].Port != 0 && n.Port != 0 && out[i].Port != n.Port {
				return nil, errors.New("conflicting node listen ports")
			}
			if out[i].Username != n.Username || out[i].Password != n.Password {
				return nil, errors.New("conflicting node listen authentication")
			}
			if out[i].Port == 0 {
				out[i].Port = n.Port
			}
			out[i].Origins = append(out[i].Origins, n)
			if n.Source == NodeSourceInline && out[i].Source != NodeSourceInline {
				out[i].Source = NodeSourceInline
				out[i].Name = n.Name
			}
			if n.SubscriptionName != "" {
				out[i].SubscriptionName = n.SubscriptionName
				out[i].SubscriptionID = n.SubscriptionID
			}
			continue
		}
		index[n.NodeKey()] = len(out)
		origin := n
		n.Origins = []NodeConfig{origin}
		out = append(out, n)
	}
	for _, s := range sources {
		if !s.Enabled {
			continue
		}
		for _, member := range s.Nodes {
			n := NodeConfig{URI: member.URI, Name: ExtractNodeName(member.URI), Port: member.Port, Source: NodeSourceLocal}
			key := n.NodeKey()
			if i, ok := index[key]; ok {
				if connectionText(out[i].URI) != connectionText(n.URI) {
					return nil, errors.New("local source conflicts with an existing node key")
				}
				if out[i].Port != 0 && n.Port != 0 && out[i].Port != n.Port {
					return nil, errors.New("conflicting local member listen ports")
				}
				if out[i].Port == 0 {
					out[i].Port = n.Port
				}
				out[i].LocalSources = append(out[i].LocalSources, LocalSourceRef{ID: s.ID, Name: s.Name})
				continue
			}
			n.LocalSources = []LocalSourceRef{{ID: s.ID, Name: s.Name}}
			index[key] = len(out)
			out = append(out, n)
		}
	}
	return out, nil
}

// ExpandNodeOrigins recovers source inputs from deduplicated runtime nodes.
// Preserve the assigned port for each owner; do not propagate merged labels.
func ExpandNodeOrigins(nodes []NodeConfig) []NodeConfig {
	out := make([]NodeConfig, 0, len(nodes))
	for _, node := range nodes {
		if len(node.Origins) == 0 {
			node.LocalSources = nil
			out = append(out, node)
			continue
		}
		for _, origin := range node.Origins {
			origin.Port = node.Port
			origin.Username, origin.Password = node.Username, node.Password
			origin.Origins, origin.LocalSources = nil, nil
			out = append(out, origin)
		}
	}
	return out
}

func (c *Config) ComposeLocalNodes() error {
	nodes, err := ComposeLocalSources(c.Nodes, c.LocalSources)
	if err != nil {
		return err
	}
	c.Nodes = nodes
	return nil
}

// FetchFreeProxySourceNodes collects a source snapshot without runtime/local
// deduplication. Combining it with the latest owners happens under the commit gate.
func (c *Config) FetchFreeProxySourceNodes() ([]NodeConfig, error) {
	copy := *c
	copy.Nodes = nil
	if err := copy.appendFreeProxyNodesFiltered(); err != nil {
		return nil, err
	}
	return copy.Nodes, nil
}

// NormalizeRuntimeWithPortMap normalizes prepared source data without network I/O.
func (c *Config) NormalizeRuntimeWithPortMap(ports map[string]uint16) error {
	c.runtimeSourcesOnly = true
	defer func() { c.runtimeSourcesOnly = false }()
	return c.NormalizeWithPortMap(ports)
}
func (c *Config) RecordLocalPorts() {
	ports := c.BuildPortMap()
	for i := range c.LocalSources {
		for j := range c.LocalSources[i].Nodes {
			n := &c.LocalSources[i].Nodes[j]
			if c.LocalSources[i].Enabled {
				n.Port = ports[canonicalNodeURI(n.URI)]
			} else {
				n.Port = 0
			}
		}
	}
}
func (c *Config) SaveSubscriptionSettings() error {
	return c.updateConfigFile(func(disk *Config) error {
		disk.Subscriptions = append([]string(nil), c.Subscriptions...)
		disk.SubscriptionRefresh = c.SubscriptionRefresh
		return nil
	})
}
func (c *Config) ReadLocalSourcesState() (LocalSourcesState, error) {
	var state LocalSourcesState
	err := withConfigLock(c.filePath, func() error {
		cfg, err := readRawConfig(c.filePath)
		if err == nil {
			state = cfg.LocalState()
		}
		return err
	})
	return state, err
}
func (c *Config) SaveLocalSourcesDraft(expected int, sources []LocalSource) (LocalSourcesState, error) {
	if err := ValidateLocalSources(sources); err != nil {
		return LocalSourcesState{}, err
	}
	// Validate each stored member too, including disabled members.
	for _, s := range sources {
		if len(s.Nodes) == 0 {
			return LocalSourcesState{}, errors.New("local source contains no proxy nodes")
		}
		for i, n := range s.Nodes {
			if len(n.URI) > MaxLocalSourceLineBytes {
				return LocalSourcesState{}, errors.New("local source URI too long")
			}
			if err := validateLocalSourceURI(n.URI, i+1); err != nil {
				return LocalSourcesState{}, err
			}
		}
	}
	var state LocalSourcesState
	err := c.updateConfigFile(func(disk *Config) error {
		current := disk.LocalState()
		if expected != current.Revision {
			return ErrLocalSourceConflict
		}
		disk.LocalSourcesDraft = &LocalSourcesDraft{Revision: expected + 1, BaseRevision: disk.LocalSourcesRevision, Sources: CloneLocalSources(sources)}
		state = disk.LocalState()
		return nil
	})
	return state, err
}
func (c *Config) CommitLocalSources(revision int, sources []LocalSource) error {
	return c.updateConfigFile(func(disk *Config) error {
		d := disk.LocalSourcesDraft
		if d == nil || d.Revision != revision || d.BaseRevision != disk.LocalSourcesRevision {
			return ErrLocalSourceConflict
		}
		disk.LocalSources = CloneLocalSources(sources)
		disk.LocalSourcesRevision = revision
		disk.LocalSourcesDraft = nil
		return nil
	})
}
func readRawConfig(path string) (*Config, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var c Config
	if e = yaml.Unmarshal(b, &c); e != nil {
		return nil, e
	}
	return &c, nil
}

// The sidecar lock is stable across rename. The entire read/modify/write cycle
// is inside it, so settings and node writes cannot erase a newer local draft.
func withConfigLock(path string, fn func() error) error {
	if path == "" {
		return errors.New("config file path is unknown")
	}
	f, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = lockFile(f); e != nil {
		return e
	}
	defer unlockFile(f)
	return fn()
}
func (c *Config) updateConfigFile(fn func(*Config) error) error {
	return withConfigLock(c.filePath, func() error {
		disk, e := readRawConfig(c.filePath)
		if e != nil {
			return e
		}
		if e = fn(disk); e != nil {
			return e
		}
		b, e := yaml.Marshal(disk)
		if e != nil {
			return e
		}
		return atomicConfigWrite(c.filePath, b)
	})
}

var ErrLocalCommitUncertain = errors.New("configuration committed but directory durability could not be confirmed")

func atomicConfigWrite(path string, data []byte) error {
	return atomicConfigWriteWithSync(path, data, func(f *os.File) error { return f.Sync() })
}
func atomicConfigWriteWithSync(path string, data []byte, syncDir func(*os.File) error) error {
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	original, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	perm := os.FileMode(0600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm() & 0600
	} else if !os.IsNotExist(err) {
		return err
	}
	// Stage a durable recovery copy before the commit point. Keep it available
	// until the directory entry has been synchronized successfully.
	backup := ""
	keepRecovery := false
	if readErr == nil {
		b, err := os.CreateTemp(filepath.Dir(path), ".config-recovery-*")
		if err != nil {
			return err
		}
		backup = b.Name()
		if err = b.Chmod(perm); err != nil {
			b.Close()
			os.Remove(backup)
			return err
		}
		if _, err = b.Write(original); err != nil {
			b.Close()
			os.Remove(backup)
			return err
		}
		if err = b.Sync(); err != nil {
			b.Close()
			os.Remove(backup)
			return err
		}
		if err = b.Close(); err != nil {
			os.Remove(backup)
			return err
		}
		defer func() {
			if !keepRecovery {
				_ = os.Remove(backup)
			}
		}()
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".config-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(perm); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	if e = syncDir(dir); e == nil {
		return nil
	}
	// A failed post-rename sync is not an ordinary pre-commit failure. Restore
	// the old bytes before reporting it; if restoration itself fails, callers
	// must keep the new runtime and surface an explicit uncertain-commit state.
	var rollback error
	if backup != "" {
		rollback = os.Rename(backup, path)
	} else {
		rollback = os.Remove(path)
	}
	if rollback != nil {
		keepRecovery = true
		return ErrLocalCommitUncertain
	}
	if syncErr := syncDir(dir); syncErr != nil {
		return fmt.Errorf("configuration restored but directory durability uncertain: %w", syncErr)
	}
	return fmt.Errorf("configuration write rolled back after directory sync failure: %w", e)
}
