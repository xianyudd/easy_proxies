package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseLocalSourceNodesStrictLines(t *testing.T) {
	content := "\xef\xbb\xbf# 首行注释\r\n" +
		"socks5://alice:pw@one.test:1080#Alpha\r\n" +
		"\r\n" +
		"   \r\n" +
		vmessTestURI(`{"ps":"Beta","add":"node.test","id":"11111111-1111-4111-8111-111111111111","port":443}`) + "\r\n" +
		"# 尾部注释\n"
	result, err := ParseLocalSourceNodes(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2 (%+v)", len(result.Nodes), result.Nodes)
	}
	if result.Nodes[0].URI != "socks5://alice:pw@one.test:1080#Alpha" {
		t.Fatalf("uri = %q", result.Nodes[0].URI)
	}
	if ExtractNodeName(result.Nodes[0].URI) != "Alpha" {
		t.Fatalf("fragment name = %q, want Alpha", ExtractNodeName(result.Nodes[0].URI))
	}
	if ExtractNodeName(result.Nodes[1].URI) != "Beta" {
		t.Fatalf("vmess ps name = %q, want Beta", ExtractNodeName(result.Nodes[1].URI))
	}
	if len(result.Duplicates) != 0 {
		t.Fatalf("unexpected duplicates: %+v", result.Duplicates)
	}
}

func TestParseLocalSourceNodesRejectsUnknownScheme(t *testing.T) {
	_, err := ParseLocalSourceNodes("socks5://ok.test:1080\nfoo://unknown.test:1\n")
	if err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("want line-numbered unsupported-protocol rejection, got %v", err)
	}
}

func TestParseLocalSourceNodesRejectsEmptyList(t *testing.T) {
	_, err := ParseLocalSourceNodes("# nothing here\n\n   \n")
	if err == nil || !strings.Contains(err.Error(), "no proxy nodes") {
		t.Fatalf("want empty-list rejection, got %v", err)
	}
}

func TestParseLocalSourceNodesRejectsMissingHost(t *testing.T) {
	_, err := ParseLocalSourceNodes("vless://bob:s3cret-pw@\n")
	if err == nil || !strings.Contains(err.Error(), "line 1") || strings.Contains(err.Error(), "s3cret-pw") {
		t.Fatalf("want sanitized missing-host rejection, got %v", err)
	}
}

func TestParseLocalSourceNodesErrorsDoNotEchoCredentials(t *testing.T) {
	content := "socks5://alice:hunter2@ok.test:1080\n" +
		"vless://bob:s3cret-pw@host.test:443 bad line\n"
	_, err := ParseLocalSourceNodes(content)
	if err == nil {
		t.Fatal("expected rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line 2") {
		t.Fatalf("error must identify the offending line: %v", err)
	}
	for _, secret := range []string{"hunter2", "s3cret-pw", "vless://", "socks5://"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error echoes sensitive content %q: %v", secret, msg)
		}
	}
}

func TestParseLocalSourceNodesRejectsOversize(t *testing.T) {
	long := "socks5://" + strings.Repeat("a", MaxLocalSourceLineBytes) + ".test:1080"
	if _, err := ParseLocalSourceNodes(long); err == nil || !strings.Contains(err.Error(), "16 KiB") {
		t.Fatalf("want 16KiB line rejection, got %v", err)
	}
	big := strings.Repeat("socks5://big.test:1080\n", MaxLocalSourceBodyBytes/22+10)
	if _, err := ParseLocalSourceNodes(big); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("want 1MiB body rejection, got %v", err)
	}
}

func TestParseLocalSourceNodesRejectsTooManyNodes(t *testing.T) {
	var sb strings.Builder
	for i := 0; i <= MaxLocalSourceNodesPerSource; i++ {
		fmt.Fprintf(&sb, "socks5://host-%d.test:1080\n", i)
	}
	if _, err := ParseLocalSourceNodes(sb.String()); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want per-source cap rejection, got %v", err)
	}
}

func TestParseLocalSourceNodesDedupsWithinSource(t *testing.T) {
	content := "socks5://alice:pw@Shared.test:1080#Alpha\n" +
		"# comment\n" +
		"socks5://alice:pw@Shared.test:1080#Beta\n"
	result, err := ParseLocalSourceNodes(content)
	if err != nil {
		t.Fatalf("duplicates must be reported, not rejected: %v", err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].URI != "socks5://alice:pw@Shared.test:1080#Alpha" {
		t.Fatalf("dedup must keep the first occurrence: %+v", result.Nodes)
	}
	if len(result.Duplicates) != 1 || result.Duplicates[0].Line != 3 {
		t.Fatalf("duplicates = %+v, want one report at line 3", result.Duplicates)
	}
	var reasons []string
	for _, dup := range result.Duplicates {
		reasons = append(reasons, dup.Why)
	}
	if strings.Contains(strings.Join(reasons, ";"), "alice:pw") {
		t.Fatal("duplicate reports must not echo credentials")
	}
}

func TestParseLocalSourceNodesDuplicateLineNumbersSkipCommentsAndBlanks(t *testing.T) {
	content := "\xef\xbb\xbf# 头注释\r\n" +
		"socks5://alice:pw@dup.test:1080#First\r\n" +
		"\r\n" +
		"   \r\n" +
		"# 中间注释\r\n" +
		"socks5://alice:pw@dup.test:1080#Second\r\n" +
		"\n" +
		"socks5://alice:pw@dup.test:1080#Third\n"
	result, err := ParseLocalSourceNodes(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].URI != "socks5://alice:pw@dup.test:1080#First" {
		t.Fatalf("dedup must keep the first occurrence: %+v", result.Nodes)
	}
	if len(result.Duplicates) != 2 {
		t.Fatalf("duplicates = %+v, want 2 reports", result.Duplicates)
	}
	if result.Duplicates[0].Line != 6 || result.Duplicates[1].Line != 8 {
		t.Fatalf("duplicate lines = %d,%d, want original input lines 6,8", result.Duplicates[0].Line, result.Duplicates[1].Line)
	}
	for _, dup := range result.Duplicates {
		if strings.Contains(dup.Why, "alice:pw") {
			t.Fatal("duplicate reports must not echo credentials")
		}
	}
}

// 问题3回归：同键凭据大小写碰撞必须整次拒绝，不能静默去重。
func TestParseLocalSourceNodesRejectsCredentialCaseCollision(t *testing.T) {
	content := "socks5://alice:Aa9@one.test:1080#Alpha\n" +
		"socks5://alice:aa9@one.test:1080#Beta\n"
	result, err := ParseLocalSourceNodes(content)
	if err == nil {
		t.Fatalf("credential case collision must reject the whole import, got nodes=%d", len(result.Nodes))
	}
	if len(result.Nodes) != 0 {
		t.Fatalf("rejected import must return empty result, got %+v", result.Nodes)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("rejection must cite the offending line, got %v", err)
	}
	for _, secret := range []string{"Aa9", "aa9", "socks5://"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error echoes sensitive content %q: %v", secret, err)
		}
	}
}

// 问题3回归：主机大小写不同但 NodeKey 相同（旧 key 规则）时保守拒绝冲突，
// 不静默合并、不回显凭据。
func TestParseLocalSourceNodesRejectsHostCaseKeyConflict(t *testing.T) {
	content := "socks5://alice:pw@Node.test:1080#One\n" +
		"socks5://alice:pw@NODE.TEST:1080#Two\n"
	result, err := ParseLocalSourceNodes(content)
	if err == nil {
		t.Fatalf("host-case key conflict must be rejected, got nodes=%d", len(result.Nodes))
	}
	if len(result.Nodes) != 0 {
		t.Fatalf("rejected import must return empty result, got %+v", result.Nodes)
	}
	for _, secret := range []string{"alice", "pw@", "socks5://"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error echoes sensitive content %q: %v", secret, err)
		}
	}
}

// 问题4回归：未知协议错误必须使用固定安全原因，不得回显 "://" 前的任意
// 输入（可能含凭据），并检查大小写变体。
func TestParseLocalSourceNodesUnknownSchemeDoesNotEchoPrefix(t *testing.T) {
	content := "socks5://ok.test:1080\n" +
		"alice:ReviewSecret@node.test:1080?next=https://example.test\n"
	_, err := ParseLocalSourceNodes(content)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want line-numbered rejection, got %v", err)
	}
	msg := err.Error()
	for _, secret := range []string{"alice", "ReviewSecret", "reviewsecret", "ALICE", "Alice"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("unknown-scheme error echoes input prefix %q: %v", secret, msg)
		}
	}
	if !strings.Contains(msg, "unsupported protocol") {
		t.Fatalf("error must use the fixed safe reason, got %v", err)
	}
}

func TestValidateLocalSourceName(t *testing.T) {
	if err := ValidateLocalSourceName("  欧洲节点 "); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	if err := ValidateLocalSourceName(""); err == nil {
		t.Fatal("empty name must be rejected")
	}
	if err := ValidateLocalSourceName("   "); err == nil {
		t.Fatal("blank name must be rejected")
	}
	if err := ValidateLocalSourceName(strings.Repeat("节", MaxLocalSourceNameRunes)); err != nil {
		t.Fatalf("80-rune name must pass: %v", err)
	}
	if err := ValidateLocalSourceName(strings.Repeat("节", MaxLocalSourceNameRunes+1)); err == nil {
		t.Fatal("81-rune name must be rejected")
	}
	if err := ValidateLocalSourceName("bad\x07name"); err == nil {
		t.Fatal("control characters must be rejected")
	}
	if !utf8.ValidString("欧洲节点") {
		t.Fatal("sanity: utf8")
	}
}

func TestValidateLocalSourcesLimits(t *testing.T) {
	if err := ValidateLocalSources(nil); err != nil {
		t.Fatalf("nil sources are valid: %v", err)
	}
	tooMany := make([]LocalSource, MaxLocalSourceCount+1)
	for i := range tooMany {
		tooMany[i] = LocalSource{ID: fmt.Sprintf("s%d", i), Name: "源", Nodes: []LocalSourceNode{{URI: "socks5://a.test:1080"}}}
	}
	if err := ValidateLocalSources(tooMany); err == nil || !strings.Contains(err.Error(), "50") {
		t.Fatalf("want 50-source cap rejection, got %v", err)
	}
	// 边界：10 个源各 500 条恰好达到 5000 总量上限（源 ID 与节点均唯一），应通过。
	boundary := make([]LocalSource, 0, 10)
	for i := 0; i < MaxLocalSourceNodesTotal/MaxLocalSourceNodesPerSource; i++ {
		nodes := make([]LocalSourceNode, MaxLocalSourceNodesPerSource)
		for j := range nodes {
			nodes[j] = LocalSourceNode{URI: fmt.Sprintf("socks5://%d-%d.test:1080", i, j)}
		}
		boundary = append(boundary, LocalSource{ID: fmt.Sprintf("s%d", i), Name: "源", Enabled: true, Nodes: nodes})
	}
	if err := ValidateLocalSources(boundary); err != nil {
		t.Fatalf("exactly %d nodes across 10 sources must pass: %v", MaxLocalSourceNodesTotal, err)
	}
	// 超限：再加 1 条（禁用源同样计入总量）→ 5001，整体拒绝。
	overflow := append(CloneLocalSources(boundary), LocalSource{
		ID: "s-extra", Name: "额外", Enabled: false,
		Nodes: []LocalSourceNode{{URI: "socks5://extra.test:1080"}},
	})
	if err := ValidateLocalSources(overflow); err == nil || !strings.Contains(err.Error(), "5000") {
		t.Fatalf("want 5000-total cap rejection including disabled sources, got %v", err)
	}
	dupID := []LocalSource{
		{ID: "same", Name: "甲", Nodes: []LocalSourceNode{{URI: "socks5://a.test:1080"}}},
		{ID: "same", Name: "乙", Nodes: []LocalSourceNode{{URI: "socks5://b.test:1080"}}},
	}
	if err := ValidateLocalSources(dupID); err == nil {
		t.Fatal("duplicate ids must be rejected")
	}
}

func TestCloneLocalSourcesDeepCopy(t *testing.T) {
	if got := CloneLocalSources(nil); got != nil {
		t.Fatal("nil sources must clone to nil")
	}
	empty := []LocalSource{}
	if got := CloneLocalSources(empty); got == nil || len(got) != 0 {
		t.Fatal("empty non-nil sources must stay non-nil")
	}
	original := []LocalSource{{
		ID: "s1", Name: "欧洲", Enabled: true,
		Nodes: []LocalSourceNode{{URI: "socks5://a.test:1080#A"}},
	}}
	clone := CloneLocalSources(original)
	clone[0].Name = "改名"
	clone[0].Nodes[0].URI = "socks5://b.test:1080#B"
	if original[0].Name != "欧洲" || original[0].Nodes[0].URI != "socks5://a.test:1080#A" {
		t.Fatalf("clone mutation leaked into original: %+v", original)
	}
}

func TestCloneLocalSourcesDraftPreservesNilVsEmpty(t *testing.T) {
	if got := CloneLocalSourcesDraft(nil); got != nil {
		t.Fatal("nil draft must clone to nil")
	}
	empty := &LocalSourcesDraft{Revision: 3, BaseRevision: 2, Sources: []LocalSource{}}
	clone := CloneLocalSourcesDraft(empty)
	if clone == nil || clone.Sources == nil || len(clone.Sources) != 0 {
		t.Fatal("draft with empty-but-present sources must stay non-nil")
	}
	clone.Sources = append(clone.Sources, LocalSource{ID: "x"})
	if len(empty.Sources) != 0 || clone.Revision != 3 || clone.BaseRevision != 2 {
		t.Fatal("clone mutation leaked into original draft")
	}
	unset := &LocalSourcesDraft{Revision: 1, BaseRevision: 1}
	if c := CloneLocalSourcesDraft(unset); c.Sources != nil {
		t.Fatal("nil draft sources must stay nil")
	}
}

func TestNewLocalSourceID(t *testing.T) {
	first, second := NewLocalSourceID(), NewLocalSourceID()
	if first == "" || second == "" || first == second {
		t.Fatalf("ids must be unique and non-empty: %q %q", first, second)
	}
}

func TestSaveNodesDoesNotPersistLocalSourceMaterializedNodes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("nodes:\n  - uri: socks5://inline.test:1080#inline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Nodes = append(cfg.Nodes, NodeConfig{
		Name: "LocalMember", URI: "socks5://local-member.test:1080#member", Source: NodeSourceLocal,
	})
	if err := cfg.SaveNodes(); err != nil {
		t.Fatal(err)
	}
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfgData), "local-member.test") {
		t.Fatal("config.yaml persisted a materialized local node")
	}
	if !strings.Contains(string(cfgData), "inline.test") {
		t.Fatal("inline node was lost by SaveNodes")
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "nodes.txt")); err == nil && strings.Contains(string(raw), "local-member.test") {
		t.Fatal("nodes.txt persisted a materialized local node")
	}
}
